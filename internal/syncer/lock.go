package syncer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
			return nil, fmt.Errorf("another bud sync is running on this device; wait for it to finish and sync again (nothing changed)")
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	return func() { f.Close() }, nil
}
