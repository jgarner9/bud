package kit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// styleKit is a kit with one output style, turned on
func styleKit(t *testing.T) *Kit {
	t.Helper()
	k, err := Load(fstest.MapFS{
		"kit.json":             {Data: []byte(`{"output-styles": ["bud"], "output_style": "bud"}`)},
		"output-styles/bud.md": {Data: []byte("---\nname: bud\n---\nlogs\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func styleEnv(t *testing.T) Env {
	t.Helper()
	dir := t.TempDir()

	return Env{ClaudeDir: filepath.Join(dir, "claude"), Ledger: filepath.Join(dir, "ledger.json"), Profile: "full"}
}

func writeSettings(t *testing.T, env Env, body string) {
	t.Helper()
	os.MkdirAll(env.ClaudeDir, 0o755)
	if err := os.WriteFile(filepath.Join(env.ClaudeDir, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
		if a.Kind == "output-style" && a.Name == "settings.json" {
			return strings.TrimSpace(a.Do + " " + a.Why)
		}
	}

	return ""
}

func TestSyncOutputStyle(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := styleAction(actions); got != "add" {
		t.Errorf("first sync: %q, want add", got)
	}
	if !exists(filepath.Join(env.ClaudeDir, "output-styles", "bud.md")) {
		t.Error("style file not synced")
	}
	if got := readSettings(t, env)["outputStyle"]; got != "bud" {
		t.Errorf("outputStyle %v, want bud", got)
	}
	if l, _, _ := ReadLedger(env.Ledger); l.OutputStyle != "bud" {
		t.Errorf("ledger OutputStyle %q, want bud", l.OutputStyle)
	}
	actions, err = Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := styleAction(actions); got != "ok" {
		t.Errorf("second sync: %q, want ok", got)
	}
}

func TestSyncKeepsOtherSettings(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	writeSettings(t, env, `{"permissions": {"allow": ["Bash(ls)"]}, "outputStyle": "bud"}`)
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if l, _, _ := ReadLedger(env.Ledger); l.OutputStyle != "" {
		t.Errorf("ledger OutputStyle %q: bud didn't write it", l.OutputStyle)
	}
	if _, err := Unsync(env, false); err != nil {
		t.Fatal(err)
	}
	m := readSettings(t, env)
	if m["outputStyle"] != "bud" {
		t.Errorf("unsync removed the user's own outputStyle: %v", m)
	}
	if _, ok := m["permissions"]; !ok {
		t.Errorf("permissions lost: %v", m)
	}
}

func TestSyncUserOutputStyleWins(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	writeSettings(t, env, `{"outputStyle": "Explanatory"}`)
	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := styleAction(actions); got != "skip you picked Explanatory" {
		t.Errorf("%q, want skip you picked Explanatory", got)
	}
	if got := readSettings(t, env)["outputStyle"]; got != "Explanatory" {
		t.Errorf("outputStyle %v, want Explanatory", got)
	}
}

func TestUnsyncOutputStyle(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	writeSettings(t, env, `{"theme": "dark"}`)
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	actions, err := Unsync(env, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := styleAction(actions); got != "remove no longer in the kit" {
		t.Errorf("unsync: %q, want remove", got)
	}
	m := readSettings(t, env)
	if _, ok := m["outputStyle"]; ok || m["theme"] != "dark" {
		t.Errorf("settings after unsync: %v", m)
	}
	if exists(filepath.Join(env.ClaudeDir, "output-styles", "bud.md")) {
		t.Error("style file left behind")
	}

	// changed to another style after bud set it: unsync leaves it
	env = styleEnv(t)
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, env, `{"outputStyle": "Explanatory"}`)
	if _, err := Unsync(env, false); err != nil {
		t.Fatal(err)
	}
	if got := readSettings(t, env)["outputStyle"]; got != "Explanatory" {
		t.Errorf("outputStyle %v, want Explanatory", got)
	}
}

func TestSyncBadSettings(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	writeSettings(t, env, `{not json`)
	_, err := Sync(k, env, false)
	if err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("err %v, want one naming settings.json", err)
	}
	if data, _ := os.ReadFile(filepath.Join(env.ClaudeDir, "settings.json")); string(data) != `{not json` {
		t.Errorf("settings.json rewritten: %q", data)
	}
	if !exists(filepath.Join(env.ClaudeDir, "output-styles", "bud.md")) {
		t.Error("style file not synced")
	}
}

func TestSyncOutputStyleDisabled(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	env.Disabled = []string{"bud"}
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(env.ClaudeDir, "output-styles", "bud.md")) || exists(filepath.Join(env.ClaudeDir, "settings.json")) {
		t.Error("disabled style was synced")
	}
}

func TestSyncOutputStyleDryRun(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	actions, err := Sync(k, env, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := styleAction(actions); got != "add" {
		t.Errorf("%q, want add", got)
	}
	if exists(filepath.Join(env.ClaudeDir, "settings.json")) {
		t.Error("dry run wrote settings.json")
	}
}
