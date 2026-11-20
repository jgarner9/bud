package approval

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestStorePersistsAndConsumesOnceConcurrently(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	now := time.Now().UTC()
	r, err := New("one", "s", "send", "t", []byte("body"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(r); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.Decide(r.ID, Approved, now); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if (Store{Path: s.Path}).Consume(r.ID, "s", "send", "t", []byte("body"), now) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful consumptions: %d", successes.Load())
	}
	got, err := (Store{Path: s.Path}).Get(r.ID)
	if err != nil || got.State != Executed {
		t.Fatalf("after restart: %+v, %v", got, err)
	}
	info, err := os.Stat(s.Path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v, %v", info, err)
	}
}

func TestApproveAndConsumeIsAtomic(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	r, err := New("one", "session", "edit", "target", []byte("reviewed"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(r); err != nil {
		t.Fatal(err)
	}
	if err := s.approveAndConsume(r.ID, r.Session, r.Action, r.Target, []byte("changed")); !errors.Is(err, ErrTransition) {
		t.Fatalf("changed arguments: %v", err)
	}
	got, err := s.Get(r.ID)
	if err != nil || got.State != Pending {
		t.Fatalf("mismatched approval persisted intermediate state: %+v, %v", got, err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.approveAndConsume(r.ID, r.Session, r.Action, r.Target, []byte("reviewed")) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful approvals = %d, want 1", successes.Load())
	}
	got, err = s.Get(r.ID)
	if err != nil || got.State != Executed {
		t.Fatalf("persisted request = %+v, %v", got, err)
	}
}

func TestApproveAndConsumeChecksExpiryAfterLock(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	r, err := New("one", "session", "edit", "target", []byte("reviewed"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	if err := s.Create(r); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.approveAndConsume(r.ID, r.Session, r.Action, r.Target, []byte("reviewed")) }()
	<-time.After(time.Until(r.ExpiresAt) + 20*time.Millisecond)
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrTransition) {
		t.Fatalf("expired approval = %v", err)
	}
	got, err := s.Get(r.ID)
	if err != nil || got.State != Expired {
		t.Fatalf("persisted request = %+v, %v", got, err)
	}
}

func TestStoreRejectsStaleTransitionTime(t *testing.T) {
	for _, state := range []State{Pending, Approved} {
		t.Run(string(state), func(t *testing.T) {
			s := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
			start := time.Now().Add(-2 * time.Hour)
			r, err := New("one", "session", "edit", "target", []byte("reviewed"), start)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Create(r); err != nil {
				t.Fatal(err)
			}
			if state == Approved {
				if err := s.transact(func(requests map[string]Request) error {
					r.State = Approved
					requests[r.ID] = r
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				err = s.Consume(r.ID, r.Session, r.Action, r.Target, []byte("reviewed"), start)
			} else {
				err = s.Decide(r.ID, Approved, start)
			}
			if !errors.Is(err, ErrTransition) {
				t.Fatalf("stale transition = %v", err)
			}
			got, err := s.Get(r.ID)
			if err != nil || got.State != Expired {
				t.Fatalf("persisted request = %+v, %v", got, err)
			}
		})
	}
}

func TestStoreRejectsChangedArgumentsAndPersistsExpiry(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	now := time.Now().UTC()
	r, _ := New("one", "s", "send", "t", []byte("body"), now)
	if err := s.Create(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Decide(r.ID, Approved, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Consume(r.ID, "s", "send", "t", []byte("other"), now); !errors.Is(err, ErrTransition) {
		t.Fatalf("changed arguments: %v", err)
	}
	if err := s.Consume(r.ID, "s", "send", "t", []byte("body"), now.Add(time.Hour)); !errors.Is(err, ErrTransition) {
		t.Fatalf("expiry: %v", err)
	}
	got, err := s.Get(r.ID)
	if err != nil || got.State != Expired {
		t.Fatalf("expired: %+v, %v", got, err)
	}
}

func TestStoreConsumesOnceAcrossProcesses(t *testing.T) {
	if file := os.Getenv("BUD_APPROVAL_TEST_STORE"); file != "" {
		err := (Store{Path: file}).Consume("one", "s", "send", "t", []byte("body"), time.Now())
		if err == nil {
			os.Exit(0)
		}
		if errors.Is(err, ErrTransition) {
			os.Exit(10)
		}
		os.Exit(11)
	}

	store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	now := time.Now()
	request, err := New("one", "s", "send", "t", []byte("body"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(request); err != nil {
		t.Fatal(err)
	}
	if err := store.Decide(request.ID, Approved, now); err != nil {
		t.Fatal(err)
	}
	const count = 12
	children := make([]*exec.Cmd, 0, count)
	for i := 0; i < count; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreConsumesOnceAcrossProcesses$")
		cmd.Env = append(os.Environ(), "BUD_APPROVAL_TEST_STORE="+store.Path)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
	}
	successes := 0
	for _, cmd := range children {
		err := cmd.Wait()
		if err == nil {
			successes++
		} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 10 {
			t.Errorf("consumer failed unexpectedly: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful process consumptions = %d, want 1", successes)
	}
	got, err := store.Get("one")
	if err != nil || got.State != Executed {
		t.Fatalf("persisted request = %+v, %v", got, err)
	}
}
