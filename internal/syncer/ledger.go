package syncer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// LedgerFormat is the sync.json format this bud writes
const LedgerFormat = 1

// Ledger is sync.json: what the last sync left on this device. bud doctor
// checks the files against it, and the next sync uses it to tell a file bud
// wrote from one edited by hand or never bud's. It holds no timestamps, so a
// sync that changes nothing writes the same bytes
type Ledger struct {
	Format  int    `json:"format"`
	Device  string `json:"device"`
	Channel string `json:"channel"`
	// Content is the content version the current link points at
	Content string `json:"content"`
	// RolledBack lists versions bud sync --rollback left, which a sync
	// doesn't go back to; a newer version on the channel clears it
	RolledBack []string    `json:"rolled_back,omitempty"`
	Overlay    *OverlayRef `json:"overlay,omitempty"`
	// Active names the dir in active.d the active link points at
	Active string `json:"active,omitempty"`
	Files  []File `json:"files"`
}

// OverlayRef is the overlay a sync layered
type OverlayRef struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit,omitempty"`
}

// File is one file bud sync wrote
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	// Kind is config, secrets, agents or commands
	Kind string `json:"kind"`
	// Layer is where it came from: public or overlay, or rendered for a
	// file built from both
	Layer string `json:"layer"`
}

// LoadLedger reads path; a missing file is an empty ledger, as on a device
// that hasn't synced yet
func LoadLedger(path string) (*Ledger, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Ledger{Format: LedgerFormat}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w; move it aside and sync again (bud then treats every file it would write as not its own)", path, err)
	}
	if l.Format != LedgerFormat {
		return nil, fmt.Errorf("%s is format %d, and this bud reads format %d; update bud with bud update", path, l.Format, LedgerFormat)
	}

	return &l, nil
}

func (l *Ledger) marshal() ([]byte, error) {
	slices.SortFunc(l.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(l.RolledBack)
	l.RolledBack = slices.Compact(l.RolledBack)
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}

// save writes the ledger, unless it already holds the same bytes
func (l *Ledger) save(path string) error {
	b, err := l.marshal()
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}

	return atomicfile.WriteFile(path, b, 0o644)
}
