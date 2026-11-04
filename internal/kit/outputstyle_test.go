package kit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// styleEnv is a Claude dir where an older bud set outputStyle to style
func styleEnv(t *testing.T, settings, style string) Env {
	t.Helper()
	dir := t.TempDir()
	env := Env{ClaudeDir: filepath.Join(dir, "claude"), Ledger: filepath.Join(dir, "ledger.json")}
	os.MkdirAll(env.ClaudeDir, 0o755)
	if settings != "" {
		os.WriteFile(filepath.Join(env.ClaudeDir, "settings.json"), []byte(settings), 0o644)
	}
	os.WriteFile(env.Ledger, []byte(`{"output_style": "`+style+`"}`), 0o644)

	return env
}

func readSettings(t *testing.T, env Env) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.ClaudeDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	return m
}

func styleAction(actions []Action) string {
	for _, a := range actions {
		if a.Kind == "output-style" {
			return strings.TrimSpace(a.Do + " " + a.Why)
		}
	}

	return ""
}

// the kit sets no output style (D78): the one an older bud set comes out,
// and the rest of settings.json stays as it was
func TestSyncRemovesOutputStyle(t *testing.T) {
	env := styleEnv(t, `{"outputStyle": "bud-ide:bud", "big": 12345678901234567890, "hooks": {"x": [{"command": "a && b > c"}]}}`, "bud-ide:bud")
	actions, err := Sync(&Kit{}, env, true)
	if err != nil || styleAction(actions) != "remove bud doesn't set one any more" {
		t.Fatalf("dry run %v, %v", actions, err)
	}
	if _, ok := readSettings(t, env)["outputStyle"]; !ok {
		t.Fatal("dry run removed it")
	}

	if _, err := Sync(&Kit{}, env, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(env.ClaudeDir, "settings.json"))
	if strings.Contains(string(data), "outputStyle") {
		t.Errorf("outputStyle left:\n%s", data)
	}
	for _, want := range []string{"12345678901234567890", `"a && b > c"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("settings.json lost %s:\n%s", want, data)
		}
	}
	if l, _, _ := ReadLedger(env.Ledger); l.OutputStyle != "" {
		t.Errorf("ledger still has %q", l.OutputStyle)
	}
	if actions, _ := Sync(&Kit{}, env, false); styleAction(actions) != "" {
		t.Errorf("again: %v", actions)
	}
}

// a style you picked after bud set its own is yours, and stays
func TestSyncKeepsYourOutputStyle(t *testing.T) {
	env := styleEnv(t, `{"outputStyle": "Explanatory"}`, "bud")
	actions, err := Sync(&Kit{}, env, false)
	if err != nil || styleAction(actions) != "" {
		t.Fatalf("%v, %v", actions, err)
	}
	if got := readSettings(t, env)["outputStyle"]; got != "Explanatory" {
		t.Errorf("outputStyle %v", got)
	}
	if l, _, _ := ReadLedger(env.Ledger); l.OutputStyle != "" {
		t.Errorf("ledger still has %q", l.OutputStyle)
	}
}

func TestSyncOutputStyleBadSettings(t *testing.T) {
	env := styleEnv(t, `{not json`, "bud")
	if _, err := Sync(&Kit{}, env, false); err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("err %v, want one naming settings.json", err)
	}
	if data, _ := os.ReadFile(filepath.Join(env.ClaudeDir, "settings.json")); string(data) != `{not json` {
		t.Errorf("settings.json rewritten: %q", data)
	}
}

func TestSyncOutputStyleSymlinkAndMode(t *testing.T) {
	env := styleEnv(t, "", "bud")
	real := filepath.Join(t.TempDir(), "dotfiles-settings.json")
	os.WriteFile(real, []byte(`{"theme": "dark", "outputStyle": "bud"}`), 0o600)
	link := filepath.Join(env.ClaudeDir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(&Kit{}, env, false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("settings.json is no longer a symlink: %v", err)
	}
	if data, _ := os.ReadFile(real); strings.Contains(string(data), "outputStyle") || !strings.Contains(string(data), "dark") {
		t.Errorf("the link's target:\n%s", data)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
}
