package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	backend "jgarner.dev/bud/internal/agent"
)

func withAICheck(t *testing.T, st backend.Status) {
	t.Helper()
	orig := aiCheck
	t.Cleanup(func() { aiCheck = orig })
	aiCheck = func(quick bool) backend.Status { return st }
}

func TestAIStatusJSON(t *testing.T) {
	withAICheck(t, backend.Status{
		Argv:     []string{"opencode", "acp"},
		Problems: []backend.Problem{{What: "opencode isn't installed", Fix: backend.InstallCommand, Hard: true}},
	})
	out := captureStdout(t, func() {
		if err := aiStatus(true); err != nil {
			t.Fatal(err)
		}
	})
	var got aiStatusJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !slices.Equal(got.Command, []string{"opencode", "acp"}) || got.Ready || len(got.Problems) != 1 || got.Problems[0].Fix == "" {
		t.Errorf("status: %+v", got)
	}
}

func TestAIStatusText(t *testing.T) {
	withAICheck(t, backend.Status{
		Argv: []string{"opencode", "acp"}, Bin: "/usr/local/bin/opencode", Version: "2.3.2",
	})
	out := captureStdout(t, func() {
		if err := aiStatus(false); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"OpenCode: ready", "Runs: opencode acp", "2.3.2", "/usr/local/bin/opencode"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// a missing OpenCode is installed with npm, at the pinned version, once
// confirmed
func TestAISetupInstallsPinnedOpenCode(t *testing.T) {
	withAICheck(t, backend.Status{Argv: []string{"opencode", "acp"}})
	var installed string
	orig := npmInstall
	t.Cleanup(func() { npmInstall = orig })
	npmInstall = func(pkg string) error { installed = pkg; return nil }
	captureStdout(t, func() {
		if err := aiSetup(nil, true); err != nil {
			t.Fatal(err)
		}
	})
	if installed != "@opencode/cli@2.3.2" {
		t.Errorf("installed %q, want @opencode/cli@2.3.2", installed)
	}

	// without --yes it asks, and a no installs nothing
	installed = ""
	pr := newPrompter(strings.NewReader("n\n"), &strings.Builder{})
	err := aiSetup(pr, false)
	if err == nil || !strings.Contains(err.Error(), backend.InstallCommand) || installed != "" {
		t.Errorf("declined: err %v, installed %q", err, installed)
	}
	pr = newPrompter(strings.NewReader("y\n"), &strings.Builder{})
	captureStdout(t, func() {
		if err := aiSetup(pr, false); err != nil {
			t.Fatal(err)
		}
	})
	if installed != backend.OpenCodePackage {
		t.Errorf("confirmed: installed %q", installed)
	}
}

// an installed OpenCode is not installed again
func TestAISetupInstalled(t *testing.T) {
	withAICheck(t, backend.Status{Argv: []string{"opencode", "acp"}, Bin: "/bin/opencode"})
	orig := npmInstall
	t.Cleanup(func() { npmInstall = orig })
	npmInstall = func(pkg string) error { t.Fatalf("installed %s", pkg); return nil }
	captureStdout(t, func() {
		if err := aiSetup(nil, true); err != nil {
			t.Fatal(err)
		}
	})
}

// bud install offers bud ai setup
func TestInstallOpenCode(t *testing.T) {
	out := captureStdout(t, func() {
		if err := installOpenCode(nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "run 'bud ai setup'") {
		t.Errorf("no terminal: %q", out)
	}
	pr := newPrompter(strings.NewReader("n\n"), &strings.Builder{})
	out = captureStdout(t, func() {
		if err := installOpenCode(pr, false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Skipped") {
		t.Errorf("said no: %q", out)
	}
	withAICheck(t, backend.Status{Argv: []string{"opencode", "acp"}})
	var installed string
	orig := npmInstall
	t.Cleanup(func() { npmInstall = orig })
	npmInstall = func(pkg string) error { installed = pkg; return nil }
	captureStdout(t, func() {
		if err := installOpenCode(nil, true); err != nil {
			t.Fatal(err)
		}
	})
	if installed != backend.OpenCodePackage {
		t.Errorf("--yes installed %q", installed)
	}
}

func TestNpmInstallAllowsOnlyOpenCodeScript(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	original := runOnTerminal
	t.Cleanup(func() { runOnTerminal = original })
	var got []string
	runOnTerminal = func(argv []string) error { got = argv; return nil }
	for _, tc := range []struct {
		pkg  string
		want []string
	}{
		{backend.OpenCodePackage, []string{"npm", "install", "-g", "--allow-scripts=@opencode/cli", backend.OpenCodePackage}},
		{"left-pad", []string{"npm", "install", "-g", "left-pad"}},
	} {
		if err := npmInstall(tc.pkg); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("install %s: got %q, want %q", tc.pkg, got, tc.want)
		}
	}
}
