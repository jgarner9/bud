package approval

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
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
