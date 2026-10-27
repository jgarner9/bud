package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/version"
)

func TestBootstrap(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origSSH, origVersion := runSSH, version.Version
	t.Cleanup(func() { runSSH, version.Version = origSSH, origVersion })

	type call struct {
		in   string
		args []string
	}
	var calls []call
	runSSH = func(in []byte, args ...string) error {
		calls = append(calls, call{string(in), args})
		return nil
	}

	var usage *UsageError
	for _, args := range [][]string{nil, {"a", "b"}, {"-x"}} {
		if err := bootstrap(&Globals{}, args, false); !errors.As(err, &usage) {
			t.Errorf("%q: want a usage error, got %v", args, err)
		}
	}

	version.Version = "0.6.0"
	captureStdout(t, func() {
		if err := bootstrap(&Globals{}, []string{"me@box"}, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(calls) != 1 {
		t.Fatalf("calls %v", calls)
	}
	got := calls[0].args
	if !slices.Equal(got[:4], []string{"-t", "me@box", "sh", "-c"}) ||
		!strings.Contains(got[4], "--profile minimal --yes --version v0.6.0") || !strings.Contains(got[4], installURL) {
		t.Fatalf("ssh %q", got)
	}

	// --config with no config.json says so and runs nothing
	calls = nil
	if err := bootstrap(&Globals{}, []string{"me@box"}, true); err == nil || len(calls) != 0 {
		t.Fatalf("want an error and no ssh, got %v, %v", err, calls)
	}

	p, _ := paths.Resolve()
	os.MkdirAll(filepath.Dir(p.MachineConfig()), 0o755)
	os.WriteFile(p.MachineConfig(), []byte(`{"profile":"full"}`), 0o644)
	version.Version = "0.6.1-nightly.202610280300"
	captureStdout(t, func() {
		if err := bootstrap(&Globals{Profile: "full"}, []string{"me@box"}, true); err != nil {
			t.Fatal(err)
		}
	})
	if len(calls) != 2 || calls[0].in != `{"profile":"full"}` || !strings.Contains(calls[0].args[3], "config.json") {
		t.Fatalf("config copy: %+v", calls)
	}
	if !strings.Contains(calls[1].args[4], "--profile full --yes --version nightly") {
		t.Fatalf("install: %q", calls[1].args[4])
	}
}

// the quoted commands survive a real shell as one word each
func TestShellQuote(t *testing.T) {
	for _, s := range []string{`d="${X:-$HOME}" && cat >"$d/x"`, `it's`, `(curl a || wget b) | sh -s -- --yes`} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil || string(out) != s {
			t.Errorf("%q came back as %q (%v)", s, out, err)
		}
	}
}
