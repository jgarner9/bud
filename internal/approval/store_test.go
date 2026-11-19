package approval

import (
	"errors"
	"os"
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
