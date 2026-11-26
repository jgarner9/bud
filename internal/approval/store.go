package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/audit"
)

// Store persists approvals in one private JSON file. Expired records are
// pruned on Create; unexpired records are retained until their deadlines.
// Create fails closed at the request or byte limit. Callers must authenticate
// requests, enforce policy and isolate the store from agent tools; file mode
// alone does not isolate processes running as the same user.
type Store struct {
	Path string
	// Audit, with Source, records each Dispatch in the shared audit log (Suite
	// D140); nil records nothing. A failed write never changes a decision
	Audit  *audit.Log
	Source string
}

var ErrNotFound = errors.New("approval request not found")
var ErrDuplicate = errors.New("approval request ID already exists")
var ErrStoreFull = errors.New("approval store has reached its request limit")

// Keep unexpired requests (including terminal ones) until their deadlines, so
// retries cannot reuse an ID while a reply might still be valid. Refuse new
// requests at the limit instead of evicting a live record.
const maxStoreRequests = 4096
const maxStoreBytes = 16 << 20

// transact holds a separate lock file across read, modify and atomic replacement.
// The lock is shared by every process using the same path.
func (s Store) transact(fn func(map[string]Request) error) error {
	if s.Path == "" {
		return errors.New("approval store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := makePrivateRegular(lock); err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	requests := map[string]Request{}
	file, err := os.OpenFile(s.Path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var original []byte
	if err == nil {
		if err := makePrivateRegular(file); err != nil {
			file.Close()
			return err
		}
		original, err = io.ReadAll(io.LimitReader(file, maxStoreBytes+1))
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if len(original) > maxStoreBytes {
			return ErrStoreFull
		}
		if err := json.Unmarshal(original, &requests); err != nil || requests == nil {
			return fmt.Errorf("invalid approval store")
		}
	}
	transitionErr := fn(requests)
	// Expiry changes state even when the attempted transition is rejected.
	if transitionErr != nil && !errors.Is(transitionErr, ErrTransition) {
		return transitionErr
	}
	data, err := json.Marshal(requests)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxStoreBytes {
		return ErrStoreFull
	}
	if bytes.Equal(original, data) {
		return transitionErr
	}
	temp, err := os.CreateTemp(filepath.Dir(s.Path), ".approvals-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), s.Path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	return transitionErr
}

func makePrivateRegular(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("approval store file is not regular")
	}
	if info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return file.Chmod(0600)
	}
	return nil
}

func (s Store) Create(r Request) error {
	if r.ID == "" || r.Session == "" || r.Action == "" || r.Target == "" || len(r.ID) > 16384 || len(r.Session) > 16384 || len(r.Action) > 16384 || len(r.Target) > 16384 || r.State != Pending || r.ExpiresAt.IsZero() || len(r.ArgumentsHash) != 64 {
		return errors.New("invalid pending approval request")
	}
	return s.transact(func(requests map[string]Request) error {
		now := time.Now()
		for id, existing := range requests {
			if !now.Before(existing.ExpiresAt) {
				delete(requests, id)
			}
		}
		if _, ok := requests[r.ID]; ok {
			return ErrDuplicate
		}
		if len(requests) >= maxStoreRequests {
			return ErrStoreFull
		}
		if !now.Before(r.ExpiresAt) {
			return errors.New("approval request already expired")
		}
		requests[r.ID] = r
		return nil
	})
}

func (s Store) Decide(id string, state State, now time.Time) error {
	return s.transact(func(requests map[string]Request) error {
		r, ok := requests[id]
		if !ok {
			return ErrNotFound
		}
		err := r.Decide(state, transitionTime(now))
		requests[id] = r
		return err
	})
}

func (s Store) Consume(id, session, action, target string, arguments []byte, now time.Time) error {
	return s.transact(func(requests map[string]Request) error {
		r, ok := requests[id]
		if !ok {
			return ErrNotFound
		}
		err := r.Consume(session, action, target, arguments, transitionTime(now))
		requests[id] = r
		return err
	})
}

// transitionTime preserves a future time supplied by the caller (for example,
// when reconciling an expiry) but never permits a stale time captured before
// waiting for the store lock to authorize an expired request. Preserve zero so
// the lifecycle's explicit invalid-time check still applies.
func transitionTime(now time.Time) time.Time {
	if !now.IsZero() {
		if current := time.Now(); current.After(now) {
			return current
		}
	}
	return now
}

// approveAndConsume records a one-shot review and spends it in the same
// transaction. A process crash must not leave an approved request that another
// process can later consume. Read the clock after acquiring the store lock so
// a request that expired while waiting for the lock cannot be consumed.
func (s Store) approveAndConsume(id, session, action, target string, arguments []byte) error {
	return s.transact(func(requests map[string]Request) error {
		r, ok := requests[id]
		if !ok {
			return ErrNotFound
		}
		now := time.Now()
		if err := r.Decide(Approved, now); err != nil {
			requests[id] = r // persist expiry, if applicable
			return err
		}
		if err := r.Consume(session, action, target, arguments, now); err != nil {
			// Do not persist the intermediate Approved state on a mismatch.
			return err
		}
		requests[id] = r
		return nil
	})
}

func (s Store) Get(id string) (Request, error) {
	var result Request
	err := s.transact(func(requests map[string]Request) error {
		var ok bool
		result, ok = requests[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return result, err
}

// PendingCounts counts the requests still waiting for an answer at now, by
// action. It reads the file without taking the lock or creating anything:
// writes replace the file atomically, so a read sees a whole store. Ids,
// targets and digests are left out. A store that doesn't exist yet is empty
func (s Store) PendingCounts(now time.Time) (map[string]int, error) {
	if s.Path == "" {
		return nil, errors.New("approval store path is empty")
	}
	file, err := os.OpenFile(s.Path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := makePrivateRegular(file); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStoreBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStoreBytes {
		return nil, ErrStoreFull
	}
	var requests map[string]Request
	if err := json.Unmarshal(data, &requests); err != nil || requests == nil {
		return nil, errors.New("invalid approval store")
	}
	counts := map[string]int{}
	for _, r := range requests {
		if r.State == Pending && now.Before(r.ExpiresAt) {
			counts[r.Action]++
		}
	}

	return counts, nil
}
