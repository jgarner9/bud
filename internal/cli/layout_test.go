package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
)

// layoutHome makes temp XDG dirs with synced layouts, and returns the paths
func layoutHome(t *testing.T) *paths.Paths {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(p.Layouts(), 0o755)
	for name, desc := range map[string]string{"default": "Tree and editor", "focus": "Editor only"} {
		os.WriteFile(filepath.Join(p.Layouts(), name+".lua"), []byte("return {\n  description = \""+desc+"\",\n  panes = {},\n}\n"), 0o644)
	}

	return p
}

func TestLayoutLs(t *testing.T) {
	layoutHome(t)
	var err error
	out := captureStdout(t, func() { err = layoutLs(nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"default  Tree and editor  (default)", "focus    Editor only\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls output %q lacks %q", out, want)
		}
	}
}

func TestLayoutSetDefault(t *testing.T) {
	p := layoutHome(t)
	var usage *UsageError
	if err := layoutSetDefault([]string{"nope"}); !errors.As(err, &usage) || !strings.Contains(err.Error(), "have default, focus") {
		t.Fatalf("unknown layout: %v", err)
	}
	captureStdout(t, func() {
		if err := layoutSetDefault([]string{"focus"}); err != nil {
			t.Fatal(err)
		}
	})
	f, _ := config.Load(p.MachineConfig())
	if s, _ := f.Settings(); s.Layout != "focus" {
		t.Fatalf("config.json layout is %q", s.Layout)
	}
}

func TestLayoutSetProject(t *testing.T) {
	p := layoutHome(t)
	dir := t.TempDir()

	// a dir that isn't registered gets registered
	captureStdout(t, func() {
		if err := layoutSet([]string{"focus", dir}); err != nil {
			t.Fatal(err)
		}
	})
	r, _ := projects.Load(p.Projects())
	got, ok := r.Get(dir)
	if !ok || got.Layout != "focus" {
		t.Fatalf("project %+v, %v", got, ok)
	}

	// ls marks it from inside the project
	wd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(wd) })
	os.Chdir(dir)
	out := captureStdout(t, func() { layoutLs(nil) })
	if !strings.Contains(out, "Editor only  ("+got.Name+")") {
		t.Errorf("ls from the project: %q", out)
	}

	// unset, by name, with no path given defaulting to the cwd
	captureStdout(t, func() {
		if err := layoutUnset(nil); err != nil {
			t.Fatal(err)
		}
	})
	r, _ = projects.Load(p.Projects())
	if got, _ = r.Get(dir); got.Layout != "" {
		t.Fatalf("still %q", got.Layout)
	}
	data, _ := os.ReadFile(p.Projects())
	if strings.Contains(string(data), `"layout"`) {
		t.Errorf("an unset layout should leave no key: %s", data)
	}

	var usage *UsageError
	if err := layoutSet([]string{"nope"}); !errors.As(err, &usage) {
		t.Errorf("unknown layout: %v", err)
	}
	if err := layoutUnset([]string{"/no/such/dir"}); err == nil {
		t.Error("unset on an unknown project should fail")
	}
}

func TestOpenChecksLayout(t *testing.T) {
	p := layoutHome(t)
	var usage *UsageError
	if err := checkLayout(p, "nope"); !errors.As(err, &usage) {
		t.Errorf("unknown --layout: %v", err)
	}
	if err := checkLayout(p, "focus"); err != nil {
		t.Errorf("known --layout: %v", err)
	}
	os.RemoveAll(p.Layouts())
	if err := checkLayout(p, "anything"); err != nil {
		t.Errorf("no layouts synced yet should pass: %v", err)
	}
}
