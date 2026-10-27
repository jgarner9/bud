package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "update.json")
	st := LoadState(path)
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if !st.Due(now) {
		t.Fatal("never checked should be due")
	}

	st.CheckedAt, st.Available = now, "0.6.0"
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	st = LoadState(path)
	if st.Available != "0.6.0" || !st.CheckedAt.Equal(now) {
		t.Fatalf("round trip: %+v", st)
	}
	for _, tt := range []struct {
		at  time.Time
		due bool
	}{
		{now.Add(time.Hour), false},
		{now.Add(CheckInterval), true},
		{now.Add(-time.Hour), true}, // the clock went back
	} {
		if got := st.Due(tt.at); got != tt.due {
			t.Errorf("Due(%v) = %v, want %v", tt.at, got, tt.due)
		}
	}

	os.WriteFile(path, []byte("{broken"), 0o644)
	if st := LoadState(path); !st.Due(now) || st.Available != "" {
		t.Fatalf("a broken file should read as empty: %+v", st)
	}
}
