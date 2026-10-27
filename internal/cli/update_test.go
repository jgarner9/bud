package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/updater"
)

// captureStdout runs fn and returns what it printed
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = stdout
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestUpdateCheck(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var asked string
	orig := checkRelease
	t.Cleanup(func() { checkRelease = orig })

	for _, tt := range []struct {
		status *updater.Status
		err    error
		want   string
	}{
		{&updater.Status{Current: "0.5.1", Newer: true, Release: &updater.Release{Version: "0.6.0"}}, nil, "bud 0.6.0 is available on stable (you have 0.5.1)"},
		{&updater.Status{Current: "0.6.0", Release: &updater.Release{Version: "0.6.0"}}, nil, "bud 0.6.0 is up to date on stable"},
		{&updater.Status{Current: "dev", Dev: true, Release: &updater.Release{Version: "0.6.0"}}, nil, "development build (dev)"},
		{nil, updater.ErrNoRelease, "Nothing is published on the stable channel yet"},
	} {
		checkRelease = func(_ context.Context, channel, _ string) (*updater.Status, error) {
			asked = channel
			return tt.status, tt.err
		}
		var err error
		out := captureStdout(t, func() { err = update(&Globals{}, nil, true, "") })
		if err != nil || !strings.Contains(out, tt.want) {
			t.Errorf("got %q, %v; want %q", out, err, tt.want)
		}
		// no --channel: config.json's, which defaults to stable
		if asked != "stable" {
			t.Errorf("checked %q, want stable", asked)
		}
	}

	captureStdout(t, func() { update(&Globals{}, nil, true, "nightly") })
	if asked != "nightly" {
		t.Errorf("--channel nightly checked %q", asked)
	}
}
