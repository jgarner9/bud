package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
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
	// ~/.claude is on Bedrock, which bud's own config dir doesn't see
	claudeElsewhere = claude.Status{Bin: "/c", Version: "2.1.0", Elsewhere: "Amazon Bedrock"}
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

// fakeShare answers shareClaudeDir, recording the provider it was asked about
func fakeShare(t *testing.T, share bool) *[]string {
	t.Helper()
	var asked []string
	orig := shareClaudeDir
	shareClaudeDir = func(_ *prompter, _ bool, provider string) (bool, error) {
		asked = append(asked, provider)
		return share, nil
	}
	t.Cleanup(func() { shareClaudeDir = orig })

	return &asked
}

func TestClaudeSetupElsewhere(t *testing.T) {
	t.Run("a no leaves it, never claude.ai's login", func(t *testing.T) {
		ran := fakeClaude(t, claudeElsewhere)
		asked := fakeShare(t, false)
		var err error
		captureStdout(t, func() { err = claudeSetup(yesPrompter("n\n"), false) })
		if err == nil || !strings.Contains(err.Error(), "ai.claude.config home") || len(*ran) != 0 || len(*asked) != 1 {
			t.Fatalf("err %v, ran %q, asked %q", err, *ran, *asked)
		}
	})

	t.Run("sharing ~/.claude is the setup", func(t *testing.T) {
		ready := claudeReady
		ready.Method = "Amazon Bedrock"
		ran := fakeClaude(t, claudeElsewhere, claudeElsewhere, ready)
		asked := fakeShare(t, true)
		out := captureStdout(t, func() {
			if err := installClaude(yesPrompter("y\n"), false); err != nil {
				t.Fatal(err)
			}
		})
		if len(*ran) != 0 || len(*asked) != 1 || (*asked)[0] != "Amazon Bedrock" || !strings.Contains(out, "logged in with Amazon Bedrock") {
			t.Fatalf("ran %q, asked %q, out %q", *ran, *asked, out)
		}
	})
}

// shareClaudeDir saves ai.claude.config "home" and points this process at
// ~/.claude, but only when the dir in use is bud's own
func TestShareClaudeDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p, _ := paths.Resolve()

	t.Setenv("CLAUDE_CONFIG_DIR", "/elsewhere")
	if ok, err := shareClaudeDir(yesPrompter("y\n"), false, "Amazon Bedrock"); ok || err != nil {
		t.Fatalf("your own CLAUDE_CONFIG_DIR: %v, %v", ok, err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", p.ClaudeConfig())
	captureStdout(t, func() {
		if ok, err := shareClaudeDir(yesPrompter("n\n"), false, "Amazon Bedrock"); ok || err != nil {
			t.Fatalf("no: %v, %v", ok, err)
		}
		if ok, err := shareClaudeDir(nil, false, "Amazon Bedrock"); ok || err != nil {
			t.Fatalf("nobody to ask: %v, %v", ok, err)
		}
		if ok, err := shareClaudeDir(yesPrompter("\n"), false, "Amazon Bedrock"); !ok || err != nil {
			t.Fatalf("enter is yes: %v, %v", ok, err)
		}
	})
	if d, set := os.LookupEnv("CLAUDE_CONFIG_DIR"); set {
		t.Errorf("CLAUDE_CONFIG_DIR still %q", d)
	}
	f, _ := config.Load(p.MachineConfig())
	if v, _ := f.Get("ai.claude.config"); v != "home" {
		t.Errorf("ai.claude.config = %v", v)
	}
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
		claudeElsewhere: "ai.claude.config home",
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
