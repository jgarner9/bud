package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Store persists approvals in one private JSON file. Callers must authenticate
// requests and enforce action policy before invoking these methods.
type Store struct{ Path string }

var ErrNotFound = errors.New("approval request not found")
var ErrDuplicate = errors.New("approval request ID already exists")

// transact holds a separate lock file across read, modify and atomic replacement.
// The lock is shared by every process using the same path.
func (s Store) transact(fn func(map[string]Request) error) error {
	if s.Path == "" {
		return errors.New("approval store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	requests := map[string]Request{}
	original, err := os.ReadFile(s.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
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

func (s Store) Create(r Request) error {
	if r.ID == "" || r.Session == "" || r.Action == "" || r.Target == "" || r.State != Pending || r.ExpiresAt.IsZero() || len(r.ArgumentsHash) != 64 {
		return errors.New("invalid pending approval request")
	}
	return s.transact(func(requests map[string]Request) error {
		if _, ok := requests[r.ID]; ok {
			return ErrDuplicate
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
		err := r.Decide(state, now)
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
		err := r.Consume(session, action, target, arguments, now)
		requests[id] = r
		return err
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
