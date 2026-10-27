package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud"
)

func writeFile(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestLoadMissingIsDefaults(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if want := Defaults(); s.Terminal != want.Terminal || s.Update != want.Update ||
		!slices.Equal(s.Languages, want.Languages) || s.Dashboard != want.Dashboard || !s.FormatOnSave {
		t.Fatalf("got %+v, want defaults", s)
	}
}

func TestSettingsMergeOverDefaults(t *testing.T) {
	f, err := Load(writeFile(t, `{
		"update": {"channel": "nightly"},
		"languages": ["bash"],
		"format_on_save": false,
		"ai": {"claude": {"args": ["--model", "opus"]}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.Settings()
	if err != nil {
		t.Fatal(err)
	}

	if s.Update.Channel != "nightly" || !s.Update.Auto {
		t.Errorf("update = %+v, want nightly with auto kept on", s.Update)
	}
	if !slices.Equal(s.Languages, []string{"bash"}) {
		t.Errorf("languages = %v, want the file's list to replace the default", s.Languages)
	}
	if s.FormatOnSave {
		t.Error("format_on_save = true, want the file's false")
	}
	if c := s.AI.Claude; !c.Autostart || c.Resume != "continue" || !slices.Equal(c.Args, []string{"--model", "opus"}) {
		t.Errorf("ai.claude = %+v", c)
	}
	if s.Dashboard.Style != "silly" || s.Terminal != "kitty" {
		t.Errorf("untouched keys lost their defaults: %+v", s)
	}
}

func TestSettingsValidate(t *testing.T) {
	for _, doc := range []string{
		`{"profile": "huge"}`,
		`{"terminal": "xterm"}`,
		`{"update": {"channel": "beta"}}`,
		`{"dashboard": {"style": "loud"}}`,
		`{"ai": {"claude": {"resume": "maybe"}}}`,
	} {
		f, err := Load(writeFile(t, doc))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Settings(); err == nil {
			t.Errorf("%s: expected a validation error", doc)
		}
	}
}

func TestSettingsWrongType(t *testing.T) {
	f, err := Load(writeFile(t, `{"update": {"auto": "yes"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Settings(); err == nil {
		t.Fatal("expected an error for a string where a bool goes")
	}
}

func TestLoadCorrupt(t *testing.T) {
	if _, err := Load(writeFile(t, "{not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestRoundTripKeepsUnknownKeys(t *testing.T) {
	path := writeFile(t, `{
		"terminal": "ghostty",
		"from_the_future": {"nested": [1, 2.5, 12345678901234567890], "flag": true},
		"update": {"channel": "stable", "new_knob": "x"}
	}`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("update.channel", "nightly"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	future, ok := got["from_the_future"].(map[string]any)
	if !ok || future["flag"] != true {
		t.Fatalf("from_the_future = %v", got["from_the_future"])
	}
	// a big integer has to come back digit for digit, not as a rounded float
	if !strings.Contains(string(data), "12345678901234567890") {
		t.Errorf("big number was rewritten:\n%s", data)
	}
	update := got["update"].(map[string]any)
	if update["new_knob"] != "x" || update["channel"] != "nightly" {
		t.Errorf("update = %v", update)
	}
	if got["terminal"] != "ghostty" {
		t.Errorf("terminal = %v", got["terminal"])
	}
}

func TestSaveCreatesDirAndLeavesNoTemp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "bud")
	f, err := Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("dashboard.style", "minimal"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("dir holds %v, want only config.json", entries)
	}
}

func TestGetSet(t *testing.T) {
	f, err := Load(writeFile(t, `{"terminal": "kitty"}`))
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Set("ai.claude.autostart", false); err != nil {
		t.Fatal(err)
	}
	if v, ok := f.Get("ai.claude.autostart"); !ok || v != false {
		t.Errorf("get after set = %v, %v", v, ok)
	}
	if _, ok := f.Get("update.channel"); ok {
		t.Error("a key the file doesn't set should not be found")
	}
	if err := f.Set("terminal.name", "x"); err == nil {
		t.Error("expected an error setting a key under a string")
	}
	if err := f.Set("update..channel", "x"); err == nil {
		t.Error("expected an error for an empty key part")
	}
}

func TestPresetLanguages(t *testing.T) {
	if got := PresetLanguages("full"); !slices.Equal(got, Languages) {
		t.Errorf("full = %v", got)
	}
	if got := PresetLanguages("minimal"); !slices.Equal(got, []string{"python", "bash"}) {
		t.Errorf("minimal = %v", got)
	}
	// the preset is a copy, so callers can't change Languages through it
	PresetLanguages("full")[0] = "x"
	if Languages[0] != "go" {
		t.Error("PresetLanguages returned Languages itself")
	}
}

// Languages and the minimal preset copy the shipped specs until Go reads them
// itself (JGA-54), so they must name the same languages
func TestLanguagesMatchSpecs(t *testing.T) {
	cfg := bud.Config()
	files, err := fs.Glob(cfg, "languages/*.lua")
	if err != nil {
		t.Fatal(err)
	}

	var specs, minimal []string
	for _, file := range files {
		name := strings.TrimSuffix(path.Base(file), ".lua")
		if strings.HasPrefix(name, "_") {
			continue
		}
		specs = append(specs, name)
		data, err := fs.ReadFile(cfg, file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "\n  minimal = true,") {
			minimal = append(minimal, name)
		}
	}

	if got := slices.Sorted(slices.Values(Languages)); !slices.Equal(got, specs) {
		t.Errorf("Languages = %v, specs in languages/ = %v", got, specs)
	}
	if got := slices.Sorted(slices.Values(PresetLanguages("minimal"))); !slices.Equal(got, minimal) {
		t.Errorf("minimal preset = %v, specs with minimal = true: %v", got, minimal)
	}
}
