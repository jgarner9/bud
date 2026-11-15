package syncer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"jgarner.dev/bud/internal/paths"
)

// lock takes the device's sync lock, so two syncs or rollbacks (a timer and
// a hand run, say) never interleave: one could otherwise prune the link set
// the other just switched to. The lock is an flock on a file beside the
// ledger, so the kernel drops it when the process exits, however it exits.
// It fails at once rather than waiting, naming what to do
func lock(ledger string) (unlock func(), err error) {
	path := filepath.Join(filepath.Dir(ledger), "sync.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another bud sync, or a bud doctor checking the last one, is running on this device; wait for it to finish and sync again (nothing changed)")
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	return func() { f.Close() }, nil
}

// ErrBusy is TryLock's answer while a sync holds the lock
var ErrBusy = errors.New("a bud sync is running on this device")

// TryLock takes the sync lock for a reader that needs the ledger, the links
// and the files to agree, as bud doctor does: a sync switches the links
// before it saves the ledger. It returns ErrBusy at once while a sync runs
func TryLock(p *paths.Paths) (unlock func(), err error) {
	unlock, err = lock(p.SyncLedger())
	if err != nil && strings.Contains(err.Error(), "is running on this device") {
		return nil, ErrBusy
	}

	return unlock, err
}
