package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backend "jgarner.dev/bud/internal/agent"
	"jgarner.dev/bud/internal/config"
)

// openclawBackend sets ai.backend = "openclaw" in the test's config.json
func openclawBackend(t *testing.T) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "bud")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"ai": {"backend": "openclaw"}}`), 0o644)
}

func withAICheck(t *testing.T, st backend.Status) *config.Settings {
	t.Helper()
	seen := &config.Settings{}
	orig := aiCheck
	t.Cleanup(func() { aiCheck = orig })
	aiCheck = func(s config.Settings, quick bool) backend.Status {
		*seen = s
		return st
	}
	return seen
}

func TestAIStatusJSON(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	os.WriteFile(filepath.Join(state, "bud", "config.json"), []byte(`{"ai":{"backend":"codex"}}`), 0o644)
	seen := withAICheck(t, backend.Status{
		Backend: "codex", Argv: []string{"codex-acp"},
		Problems: []backend.Problem{{What: "codex-acp isn't installed", Fix: "npm install -g @zed-industries/codex-acp", Hard: true}},
	})
	out := captureStdout(t, func() {
		if err := aiStatus(true); err != nil {
			t.Fatal(err)
		}
	})
	if seen.AI.Backend != "codex" {
		t.Errorf("checked backend %q, want the config's codex", seen.AI.Backend)
	}
	var got aiStatusJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Backend != "codex" || got.Ready || len(got.Problems) != 1 || got.Problems[0].Fix == "" {
		t.Errorf("status: %+v", got)
	}
}

func TestAIStatusText(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	openclawBackend(t)
	withAICheck(t, backend.Status{
		Backend: "openclaw", Argv: []string{"openclaw", "acp", "--url", "wss://home:18789"},
		Bin: "/usr/local/bin/openclaw", Version: "OpenClaw 2026.11.3", Gateway: "home:18789", Reachable: true,
	})
	out := captureStdout(t, func() {
		if err := aiStatus(false); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"Backend: openclaw, ready", "Runs: openclaw acp --url wss://home:18789", "OpenClaw 2026.11.3", "Gateway: home:18789, answering"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// setup asks for the Gateway and saves what changed
func TestAISetupOpenClaw(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	openclawBackend(t)
	withAICheck(t, backend.Status{Backend: "openclaw", Argv: []string{"openclaw", "acp"}, Bin: "/bin/openclaw"})
	pr := newPrompter(strings.NewReader("wss://home.tail.ts.net:18789\n\n"), &strings.Builder{})
	captureStdout(t, func() {
		if err := aiSetup(pr, false); err != nil {
			t.Fatal(err)
		}
	})
	f, err := config.Load(filepath.Join(state, "bud", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.Settings()
	if s.AI.OpenClaw.URL != "wss://home.tail.ts.net:18789" || s.AI.OpenClaw.TokenFile != "~/.config/openclaw/gateway-token" {
		t.Errorf("saved %+v", s.AI.OpenClaw)
	}

	// a URL that isn't a websocket is refused
	pr = newPrompter(strings.NewReader("https://home\n"), &strings.Builder{})
	if err := aiSetup(pr, false); err == nil || !strings.Contains(err.Error(), "isn't a ws:// or wss:// URL") {
		t.Errorf("bad URL: %v", err)
	}
}

// a missing backend is installed with npm once confirmed
func TestAISetupInstalls(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	openclawBackend(t)
	withAICheck(t, backend.Status{Backend: "openclaw", Argv: []string{"openclaw", "acp"}})
	var installed string
	orig := npmInstall
	t.Cleanup(func() { npmInstall = orig })
	npmInstall = func(pkg string) error { installed = pkg; return nil }
	captureStdout(t, func() {
		if err := aiSetup(nil, true); err != nil {
			t.Fatal(err)
		}
	})
	if installed != "openclaw" {
		t.Errorf("installed %q", installed)
	}
}

// bud install on a backend other than claude offers bud ai setup
func TestInstallBackend(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	openclawBackend(t)
	out := captureStdout(t, func() {
		if err := installBackend(nil, false, "openclaw"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "run 'bud ai setup'") {
		t.Errorf("no terminal: %q", out)
	}
	pr := newPrompter(strings.NewReader("n\n"), &strings.Builder{})
	out = captureStdout(t, func() {
		if err := installBackend(pr, false, "openclaw"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Skipped") {
		t.Errorf("said no: %q", out)
	}
	withAICheck(t, backend.Status{Backend: "openclaw", Argv: []string{"openclaw", "acp"}})
	var installed string
	orig := npmInstall
	t.Cleanup(func() { npmInstall = orig })
	npmInstall = func(pkg string) error { installed = pkg; return nil }
	captureStdout(t, func() {
		if err := installBackend(nil, true, "openclaw"); err != nil {
			t.Fatal(err)
		}
	})
	if installed != "openclaw" {
		t.Errorf("--yes installed %q", installed)
	}
}
