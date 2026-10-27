package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/paths"
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

func TestSelfUpdate(t *testing.T) {
	state, data := t.TempDir(), t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_DATA_HOME", data)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(t.TempDir(), "bud")
	os.WriteFile(installed, []byte("old"), 0o755)

	origCheck, origFetch, origBin, origExec, origExe := checkRelease, fetchRelease, checkBinary, execBinary, executable
	t.Cleanup(func() {
		checkRelease, fetchRelease, checkBinary, execBinary, executable = origCheck, origFetch, origBin, origExec, origExe
	})
	newer := &updater.Status{Current: "0.5.1", Newer: true, Release: &updater.Release{Version: "0.6.0"}}
	checkRelease = func(context.Context, string, string) (*updater.Status, error) { return newer, nil }
	fetched := false
	fetchRelease = func(_ context.Context, _ *updater.Release, _, _, dir string, _ func(string, ...any)) (string, error) {
		fetched = true
		bin := filepath.Join(dir, "bud")
		return bin, os.WriteFile(bin, []byte("new"), 0o755)
	}
	checkBinary = func(context.Context, string) error { return nil }
	var execArgs, execEnv []string
	execBinary = func(_ string, args, env []string) error {
		execArgs, execEnv = args, env
		return nil
	}
	executable = func() (string, error) { return installed, nil }

	// no receipt: someone else owns this binary
	out := captureStdout(t, func() { err = selfUpdate(&Globals{}, p, "stable") })
	if err != nil || fetched || !strings.Contains(out, "install.sh didn't install this bud") {
		t.Fatalf("no receipt: %q, %v, fetched %v", out, err, fetched)
	}

	os.MkdirAll(filepath.Dir(p.InstallReceipt()), 0o755)
	os.WriteFile(p.InstallReceipt(), []byte(`{"method":"install.sh","path":"/somewhere/else/bud"}`), 0o644)
	out = captureStdout(t, func() { err = selfUpdate(&Globals{}, p, "stable") })
	if err != nil || fetched || !strings.Contains(out, "isn't the one install.sh installed") {
		t.Fatalf("other binary: %q, %v, fetched %v", out, err, fetched)
	}

	os.WriteFile(p.InstallReceipt(), []byte(`{"method":"install.sh","path":"`+installed+`"}`), 0o644)
	captureStdout(t, func() { err = selfUpdate(&Globals{Verbose: true}, p, "stable") })
	if err != nil || !fetched {
		t.Fatalf("update: %v, fetched %v", err, fetched)
	}
	for path, want := range map[string]string{installed: "new", p.PrevBinary(): "old"} {
		if b, _ := os.ReadFile(path); string(b) != want {
			t.Errorf("%s = %q, want %q", path, b, want)
		}
	}
	if want := []string{installed, "--verbose", "update"}; !slices.Equal(execArgs, want) {
		t.Errorf("exec %v, want %v", execArgs, want)
	}
	if !slices.Contains(execEnv, updatedFromEnv+"=0.5.1") {
		t.Errorf("env has no %s", updatedFromEnv)
	}

	// nothing newer, or GitHub unreachable: no download, and the sync goes on
	fetched = false
	for _, check := range []func(context.Context, string, string) (*updater.Status, error){
		func(context.Context, string, string) (*updater.Status, error) {
			return &updater.Status{Current: "0.6.0", Release: &updater.Release{Version: "0.6.0"}}, nil
		},
		func(context.Context, string, string) (*updater.Status, error) { return nil, errors.New("offline") },
		func(context.Context, string, string) (*updater.Status, error) { return nil, updater.ErrNoRelease },
	} {
		checkRelease = check
		captureStdout(t, func() { err = selfUpdate(&Globals{}, p, "stable") })
		if err != nil || fetched {
			t.Errorf("got %v, fetched %v", err, fetched)
		}
	}
}
