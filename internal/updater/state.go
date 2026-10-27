package updater

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
)

// CheckInterval is how often launching bud checks for a release (D9)
const CheckInterval = 24 * time.Hour

// State is update.json, what the background check found (D9, D42). The
// dashboard reads it for its banner
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	Channel   string    `json:"channel,omitempty"`
	// Available is a newer release that's waiting for `bud update` (nightly,
	// update.auto off, or a bud install.sh didn't install)
	Available string `json:"available,omitempty"`
	// Installed is a release the background check swapped in, whose config
	// sync runs at the next launch. From is the version it replaced, and
	// FailingBefore the doctor checks already failing then
	Installed     string   `json:"installed,omitempty"`
	From          string   `json:"from,omitempty"`
	FailingBefore []string `json:"failing_before,omitempty"`
}

// Due reports whether a check is due at now
func (s *State) Due(now time.Time) bool {
	return now.Sub(s.CheckedAt) >= CheckInterval || now.Before(s.CheckedAt)
}

// LoadState reads update.json; a missing or unreadable one is empty, so a
// broken file only means checking again
func LoadState(path string) *State {
	var s State
	data, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(data, &s) != nil {
			s = State{}
		}
	}

	return &s
}

// Save writes update.json atomically
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}

	return atomicfile.WriteFile(path, append(data, '\n'), 0o644)
}
