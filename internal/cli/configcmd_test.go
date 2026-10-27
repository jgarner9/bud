package cli

import (
	"os"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/paths"
)

func TestConfigCommands(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, _ := paths.Resolve()
	os.MkdirAll(p.State, 0o755)
	os.WriteFile(p.MachineConfig(), []byte(`{"mine": {"keep": 1}}`), 0o644)

	out := captureStdout(t, func() {
		if err := configGet([]string{"update.channel"}); err != nil {
			t.Fatal(err)
		}
	})
	if out != "stable\n" {
		t.Fatalf("default: %q", out)
	}

	captureStdout(t, func() {
		for _, kv := range [][]string{{"update.channel", "nightly"}, {"ai.claude.idle_minutes", "45"}, {"languages", `["go","lua"]`}, {"dashboard.git", "false"}} {
			if err := configSet(kv, false); err != nil {
				t.Fatalf("set %v: %v", kv, err)
			}
		}
	})
	for key, want := range map[string]string{"update.channel": "nightly\n", "ai.claude.idle_minutes": "45\n", "dashboard.git": "false\n"} {
		if got := captureStdout(t, func() { configGet([]string{key}) }); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	// typos, wrong types and values bud doesn't allow are refused, and nothing is saved
	before, _ := os.ReadFile(p.MachineConfig())
	for _, kv := range [][]string{{"update.chanel", "x"}, {"update.channel", "weekly"}, {"ai.claude.idle_minutes", "soon"}} {
		if err := configSet(kv, false); err == nil {
			t.Errorf("set %v accepted", kv)
		}
	}
	if after, _ := os.ReadFile(p.MachineConfig()); string(after) != string(before) {
		t.Fatal("a refused set changed the file")
	}

	// unset goes back to the default; keys bud doesn't know stay put
	captureStdout(t, func() { configUnset([]string{"update.channel"}) })
	if got := captureStdout(t, func() { configGet([]string{"update.channel"}) }); got != "stable\n" {
		t.Fatalf("after unset: %q", got)
	}
	data, _ := os.ReadFile(p.MachineConfig())
	if !strings.Contains(string(data), `"keep": 1`) || strings.Contains(string(data), "channel") {
		t.Fatalf("file %s", data)
	}

	// edit checks the result
	orig := editFile
	editFile = func(path string) error { return os.WriteFile(path, []byte(`{"terminal": "xterm"}`), 0o644) }
	t.Cleanup(func() { editFile = orig })
	if err := configEdit(nil); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("edit to a bad value: %v", err)
	}
}
