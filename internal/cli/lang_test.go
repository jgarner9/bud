package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/lang"
	"jgarner.dev/bud/internal/paths"
)

// langTest gives a test its own XDG dirs and spec set, and records restores
type langTest struct {
	p        *paths.Paths
	restored int
}

func newLangTest(t *testing.T, configJSON string, set *lang.Set) *langTest {
	t.Helper()
	root := t.TempDir()
	for env, dir := range map[string]string{
		"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache",
	} {
		t.Setenv(env, filepath.Join(root, dir))
	}
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if configJSON != "" {
		if err := os.MkdirAll(p.State, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.MachineConfig(), []byte(configJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	lt := &langTest{p: p}
	origLoad, origRestore := loadLanguages, restorePlugins
	loadLanguages = func(bool, ...string) (*lang.Set, error) { return set, nil }
	restorePlugins = func() error { lt.restored++; return nil }
	t.Cleanup(func() { loadLanguages, restorePlugins = origLoad, origRestore })

	return lt
}

func (lt *langTest) languages(t *testing.T) []string {
	t.Helper()
	f, err := config.Load(lt.p.MachineConfig())
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.Settings()
	if err != nil {
		t.Fatal(err)
	}
	return s.Languages
}

// markSynced makes the config dir look like bud install has run
func (lt *langTest) markSynced(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(lt.p.Config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lt.p.Config, ".bud-manifest.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLangEnable(t *testing.T) {
	g := &Globals{}

	t.Run("adds to the preset, saves, restores", func(t *testing.T) {
		// python and bash are the minimal preset; c needs only cc, a base dep
		lt := newLangTest(t, `{"profile": "minimal"}`, testSpecs())
		lt.markSynced(t)
		if err := langEnable(g, []string{"c", "bash"}, true); err != nil {
			t.Fatal(err)
		}
		if got := lt.languages(t); !slices.Equal(got, []string{"bash", "python", "c"}) {
			t.Errorf("languages = %v", got)
		}
		if lt.restored != 1 {
			t.Errorf("restored %d times, want 1", lt.restored)
		}
	})

	t.Run("already enabled does nothing", func(t *testing.T) {
		lt := newLangTest(t, `{"languages": ["go"]}`, testSpecs())
		lt.markSynced(t)
		if err := langEnable(g, []string{"go"}, true); err != nil {
			t.Fatal(err)
		}
		if lt.restored != 0 {
			t.Error("nothing new to install, so no restore")
		}
	})

	t.Run("before bud install there's nothing to restore", func(t *testing.T) {
		lt := newLangTest(t, `{"languages": []}`, testSpecs())
		if err := langEnable(g, []string{"go"}, true); err != nil {
			t.Fatal(err)
		}
		if got := lt.languages(t); !slices.Equal(got, []string{"go"}) || lt.restored != 0 {
			t.Errorf("languages = %v, restored %d", got, lt.restored)
		}
	})

	t.Run("unknown and broken names change nothing", func(t *testing.T) {
		set := testSpecs()
		set.Errors["zig"] = `unknown field "oops"`
		lt := newLangTest(t, `{"languages": ["go"]}`, set)
		if err := langEnable(g, []string{"bash", "nim"}, true); err == nil || !strings.Contains(err.Error(), `no language "nim"`) {
			t.Errorf("unknown: %v", err)
		}
		if err := langEnable(g, []string{"zig"}, true); err == nil || !strings.Contains(err.Error(), "oops") {
			t.Errorf("broken: %v", err)
		}
		if got := lt.languages(t); !slices.Equal(got, []string{"go"}) {
			t.Errorf("languages = %v", got)
		}
	})
}

func TestLangDisable(t *testing.T) {
	t.Run("from the preset", func(t *testing.T) {
		lt := newLangTest(t, "", testSpecs())
		if err := langDisable([]string{"c", "html"}); err != nil {
			t.Fatal(err)
		}
		if got := lt.languages(t); !slices.Equal(got, []string{"bash", "css", "go", "python", "typescript"}) {
			t.Errorf("languages = %v", got)
		}
	})

	t.Run("down to none leaves an empty list, not the preset", func(t *testing.T) {
		lt := newLangTest(t, `{"languages": ["go"]}`, testSpecs())
		if err := langDisable([]string{"go"}); err != nil {
			t.Fatal(err)
		}
		if got := lt.languages(t); got == nil || len(got) != 0 {
			t.Errorf("languages = %#v", got)
		}
	})

	t.Run("not enabled", func(t *testing.T) {
		newLangTest(t, `{"languages": ["go"]}`, testSpecs())
		if err := langDisable([]string{"go", "c"}); err == nil || err.Error() != "not enabled: c" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestLangAdd(t *testing.T) {
	lt := newLangTest(t, "", testSpecs())
	if err := langAdd([]string{"zig"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lt.p.UserLanguageSpecs(), "zig.lua")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "filetypes =") {
		t.Fatalf("template not copied: %v", err)
	}

	if err := langAdd([]string{"zig"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("twice: %v", err)
	}

	// enabling it unedited would hand the placeholders to the package manager
	set := testSpecs()
	set.Specs = append(set.Specs, lang.Spec{Name: "zig"})
	loadLanguages = func(bool, ...string) (*lang.Set, error) { return set, nil }
	if err := langEnable(&Globals{}, []string{"zig"}, true); err == nil || !strings.Contains(err.Error(), "still the template") {
		t.Errorf("enable the template: %v", err)
	}
	if err := os.WriteFile(path, []byte(`return { filetypes = { "zig" } }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := langEnable(&Globals{}, []string{"zig"}, true); err != nil {
		t.Errorf("enable once edited: %v", err)
	}
	if err := langAdd([]string{"go"}); err == nil || !strings.Contains(err.Error(), "already ships go") {
		t.Errorf("shipped: %v", err)
	}
	for _, bad := range []string{"Zig", "../x", "_template", "1c", ""} {
		if err := langAdd([]string{bad}); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestLangRm(t *testing.T) {
	lt := newLangTest(t, `{"languages": ["go", "zig"]}`, testSpecs())
	if err := langAdd([]string{"zig"}); err != nil {
		t.Fatal(err)
	}
	if err := langRm([]string{"go"}, true); err == nil || !strings.Contains(err.Error(), "can't be removed") {
		t.Errorf("shipped: %v", err)
	}
	if err := langRm([]string{"nim"}, true); err == nil || !strings.Contains(err.Error(), "no language spec of yours") {
		t.Errorf("missing: %v", err)
	}

	if err := langRm([]string{"zig"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(lt.p.UserLanguageSpecs(), "zig.lua")); !os.IsNotExist(err) {
		t.Errorf("spec still there: %v", err)
	}
	if got := lt.languages(t); !slices.Equal(got, []string{"go"}) {
		t.Errorf("languages = %v, want zig dropped", got)
	}
}
