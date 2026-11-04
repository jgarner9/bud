package kit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// styleKit is a kit with a plugin's output style, turned on
func styleKit(t *testing.T) *Kit {
	t.Helper()
	k, err := Load(fstest.MapFS{
		"kit.json": {Data: []byte(`{"plugins": ["bud-ide@bud-plugins"], "output_style": "bud-ide:bud"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// styleEnv is a Claude dir with a claude plugin that installs what it's
// asked to
func styleEnv(t *testing.T) Env {
	t.Helper()
	dir := t.TempDir()
	var installed []string

	return Env{
		ClaudeDir: filepath.Join(dir, "claude"), Ledger: filepath.Join(dir, "ledger.json"),
		Claude: func(a ...string) error {
			switch a[1] {
			case "install":
				installed = append(installed, a[len(a)-1])
			case "uninstall":
				installed = slices.DeleteFunc(installed, func(s string) bool { return s == a[len(a)-1] })
			}
			return nil
		},
		InstalledPlugins: func() ([]string, error) { return slices.Clone(installed), nil },
	}
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
	if got := readSettings(t, env)["outputStyle"]; got != "bud-ide:bud" {
		t.Errorf("outputStyle %v, want bud-ide:bud", got)
	}
	if l, _, _ := ReadLedger(env.Ledger); l.OutputStyle != "bud-ide:bud" {
		t.Errorf("ledger OutputStyle %q, want bud-ide:bud", l.OutputStyle)
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
	writeSettings(t, env, `{"permissions": {"allow": ["Bash(ls)"]}, "outputStyle": "bud-ide:bud"}`)
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
	if m["outputStyle"] != "bud-ide:bud" {
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
}

func TestSyncOutputStyleDisabled(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	env.Disabled = []string{"bud-ide:bud"}
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(env.ClaudeDir, "settings.json")) {
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

func TestSyncSettingsRoundTrip(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	writeSettings(t, env, `{"big": 12345678901234567890, "hooks": {"x": [{"command": "a && b > c"}]}}`)
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(env.ClaudeDir, "settings.json"))
	for _, want := range []string{"12345678901234567890", `"a && b > c"`, `"outputStyle": "bud-ide:bud"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("settings.json lost %s:\n%s", want, data)
		}
	}
}

func TestSyncNullSettings(t *testing.T) {
	for _, body := range []string{"null", "", " \n"} {
		k, env := styleKit(t), styleEnv(t)
		writeSettings(t, env, body)
		if _, err := Sync(k, env, false); err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if got := readSettings(t, env)["outputStyle"]; got != "bud-ide:bud" {
			t.Errorf("%q: outputStyle %v", body, got)
		}
	}
}

func TestSyncSettingsSymlinkAndMode(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	os.MkdirAll(env.ClaudeDir, 0o755)
	real := filepath.Join(t.TempDir(), "dotfiles-settings.json")
	os.WriteFile(real, []byte(`{"theme": "dark"}`), 0o600)
	link := filepath.Join(env.ClaudeDir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("settings.json is no longer a symlink: %v", err)
	}
	data, _ := os.ReadFile(real)
	if !strings.Contains(string(data), `"outputStyle": "bud-ide:bud"`) {
		t.Errorf("the link's target wasn't updated:\n%s", data)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
}

// without the plugin that has it, the style isn't turned on
func TestSyncStyleWaitsForPlugin(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	env.Claude = func(...string) error { return errors.New("offline") }
	actions, err := Sync(k, env, false)
	if err == nil {
		t.Fatal("no error for the failed install")
	}
	if got := styleAction(actions); got != "skip needs the bud-ide plugin" {
		t.Errorf("%q", got)
	}
	if exists(filepath.Join(env.ClaudeDir, "settings.json")) {
		t.Error("turned on a style Claude Code doesn't have")
	}
}

// an older bud synced its own bud style file and turned it on: the next sync
// takes the file out and switches to the plugin's
func TestSyncOutputStyleFromOlderBud(t *testing.T) {
	k, env := styleKit(t), styleEnv(t)
	file := []byte("---\nname: bud\n---\nlogs\n")
	os.MkdirAll(filepath.Join(env.ClaudeDir, "output-styles"), 0o755)
	os.WriteFile(filepath.Join(env.ClaudeDir, "output-styles", "bud.md"), file, 0o644)
	writeSettings(t, env, `{"outputStyle": "bud"}`)
	os.WriteFile(env.Ledger, []byte(`{"files": {"output-styles/bud.md": "`+hash(file)+`"}, "output_style": "bud"}`), 0o644)

	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := styleAction(actions); got != "update" {
		t.Errorf("%q, want update", got)
	}
	if got := readSettings(t, env)["outputStyle"]; got != "bud-ide:bud" {
		t.Errorf("outputStyle %v", got)
	}
	if exists(filepath.Join(env.ClaudeDir, "output-styles")) {
		t.Error("the old style file is still there")
	}
}
