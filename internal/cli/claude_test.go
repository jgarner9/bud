package cli

import (
	"errors"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/claude"
)

// fakeClaude answers checks from states in turn (the last one repeats) and
// records what would run on the terminal
func fakeClaude(t *testing.T, states ...claude.Status) *[][]string {
	t.Helper()
	var ran [][]string
	origCheck, origRun := checkClaude, runOnTerminal
	checkClaude = func() (claude.Status, error) {
		s := states[0]
		if len(states) > 1 {
			states = states[1:]
		}
		return s, nil
	}
	runOnTerminal = func(argv []string) error { ran = append(ran, argv); return nil }
	t.Cleanup(func() { checkClaude, runOnTerminal = origCheck, origRun })

	return &ran
}

var (
	claudeMissing   = claude.Status{}
	claudeLoggedOut = claude.Status{Bin: "/c", Version: "2.1.0"}
	claudeReady     = claude.Status{Bin: "/c", Version: "2.1.0", LoggedIn: true, Method: "claude.ai"}
)

func yesPrompter(answers string) *prompter {
	return newPrompter(strings.NewReader(answers), &strings.Builder{})
}

func TestClaudeSetup(t *testing.T) {
	t.Run("installs, then logs in", func(t *testing.T) {
		ran := fakeClaude(t, claudeMissing, claudeLoggedOut, claudeReady)
		captureStdout(t, func() {
			if err := claudeSetup(yesPrompter("y\n"), false); err != nil {
				t.Fatal(err)
			}
		})
		if len(*ran) != 2 || (*ran)[0][2] != claude.InstallScript || strings.Join((*ran)[1], " ") != "/c auth login" {
			t.Fatalf("ran %q", *ran)
		}
	})

	t.Run("no to installing changes nothing", func(t *testing.T) {
		ran := fakeClaude(t, claudeMissing)
		var err error
		captureStdout(t, func() { err = claudeSetup(yesPrompter("n\n"), false) })
		if !errors.Is(err, errNotConfirmed) || len(*ran) != 0 {
			t.Fatalf("err %v, ran %q", err, *ran)
		}
	})

	t.Run("--yes without a terminal installs and leaves the login", func(t *testing.T) {
		ran := fakeClaude(t, claudeMissing, claudeLoggedOut)
		var err error
		out := captureStdout(t, func() { err = claudeSetup(nil, true) })
		if err != nil || len(*ran) != 1 || !strings.Contains(out, "/c auth login") {
			t.Fatalf("err %v, ran %q, out %q", err, *ran, out)
		}
	})

	t.Run("no terminal and no --yes refuses", func(t *testing.T) {
		fakeClaude(t, claudeMissing)
		var err error
		captureStdout(t, func() { err = claudeSetup(nil, false) })
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("already ready runs nothing", func(t *testing.T) {
		ran := fakeClaude(t, claudeReady)
		captureStdout(t, func() {
			if err := claudeSetup(nil, false); err != nil {
				t.Fatal(err)
			}
		})
		if len(*ran) != 0 {
			t.Fatalf("ran %q", *ran)
		}
	})

	t.Run("a login that doesn't take is an error", func(t *testing.T) {
		fakeClaude(t, claudeLoggedOut)
		var err error
		captureStdout(t, func() { err = claudeSetup(yesPrompter(""), false) })
		if err == nil || !strings.Contains(err.Error(), "still isn't logged in") {
			t.Fatalf("err %v", err)
		}
	})
}

func TestInstallClaude(t *testing.T) {
	ran := fakeClaude(t, claudeMissing)
	out := captureStdout(t, func() {
		if err := installClaude(yesPrompter("n\n"), false); err != nil {
			t.Fatal(err)
		}
		if err := installClaude(nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(*ran) != 0 || !strings.Contains(out, "Skipped") || !strings.Contains(out, "run 'bud claude setup'") {
		t.Fatalf("ran %q, out %q", *ran, out)
	}
}

func TestClaudeStatus(t *testing.T) {
	for s, want := range map[claude.Status]string{
		claudeMissing:   "isn't installed",
		claudeLoggedOut: "not logged in",
		claudeReady:     "logged in with claude.ai",
	} {
		fakeClaude(t, s)
		out := captureStdout(t, func() {
			if err := claudeStatus(nil); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, want) {
			t.Errorf("%+v: %q lacks %q", s, out, want)
		}
	}
}

func TestInstallKit(t *testing.T) {
	synced := 0
	orig := syncKit
	syncKit = func(*Globals, *prompter, bool) error { synced++; return nil }
	t.Cleanup(func() { syncKit = orig })

	// not installed, or logged out: only the hint, since connectors need a login
	for _, s := range []claude.Status{claudeMissing, claudeLoggedOut} {
		fakeClaude(t, s)
		out := captureStdout(t, func() {
			if err := installKit(&Globals{}, nil); err != nil {
				t.Fatal(err)
			}
		})
		if synced != 0 || !strings.Contains(out, "bud kit sync") {
			t.Fatalf("synced %d, out %q", synced, out)
		}
	}

	fakeClaude(t, claudeReady)
	if err := installKit(&Globals{}, nil); err != nil || synced != 1 {
		t.Fatalf("err %v, synced %d", err, synced)
	}
}
