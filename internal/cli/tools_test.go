package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
)

// toolsHome makes temp XDG dirs with one tool of bud's and one of yours
func toolsHome(t *testing.T) *paths.Paths {
	t.Helper()
	for _, v := range []string{"CONFIG", "DATA", "STATE"} {
		t.Setenv("XDG_"+v+"_HOME", t.TempDir())
	}
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for dir, desc := range map[string]string{
		filepath.Join(p.Tools(), "test-file"):     "Jump to the test",
		filepath.Join(p.UserTools(), "mine"):      "My tool",
		filepath.Join(p.UserTools(), "test-file"): "My own test-file",
	} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"description": "`+desc+`"}`), 0o644)
		os.WriteFile(filepath.Join(dir, "init.lua"), []byte("return {}\n"), 0o644)
	}
	// languages/ in user-tools is specs, not a tool
	os.MkdirAll(filepath.Join(p.UserLanguageSpecs()), 0o755)

	return p
}

func TestToolsLsEnableDisable(t *testing.T) {
	p := toolsHome(t)
	out := captureStdout(t, func() {
		if err := toolsLs(nil); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"mine       on   yours  My tool", "test-file  on   yours  My own test-file"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls %q lacks %q", out, want)
		}
	}
	if strings.Contains(out, "languages") {
		t.Errorf("ls lists languages/: %q", out)
	}

	captureStdout(t, func() {
		if err := toolsDisable([]string{"mine"}, "boom at line 3"); err != nil {
			t.Fatal(err)
		}
	})
	f, _ := config.Load(p.MachineConfig())
	if v, _ := f.Get("tools.disabled"); !slices.Equal(v.([]any), []any{"mine"}) {
		t.Fatalf("tools.disabled = %v", v)
	}
	out = captureStdout(t, func() { toolsLs(nil) })
	if !strings.Contains(out, "mine       off  yours  My tool  (broken: boom at line 3)") {
		t.Errorf("ls after disable: %q", out)
	}

	captureStdout(t, func() {
		if err := toolsEnable([]string{"mine"}); err != nil {
			t.Fatal(err)
		}
	})
	out = captureStdout(t, func() { toolsLs(nil) })
	if !strings.Contains(out, "mine       on ") || strings.Contains(out, "broken") {
		t.Errorf("ls after enable: %q", out)
	}

	var usage *UsageError
	if err := toolsEnable([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "have mine, test-file") {
		t.Errorf("unknown tool: %v", err)
	} else if !errors.As(err, &usage) {
		t.Errorf("not a usage error: %v", err)
	}
}

func TestToolsRm(t *testing.T) {
	p := toolsHome(t)
	os.RemoveAll(filepath.Join(p.UserTools(), "test-file"))
	if err := toolsRm([]string{"test-file"}, true); err == nil || !strings.Contains(err.Error(), "ships with bud") {
		t.Fatalf("removing bud's: %v", err)
	}
	captureStdout(t, func() {
		if err := toolsRm([]string{"mine"}, true); err != nil {
			t.Fatal(err)
		}
	})
	if exists(filepath.Join(p.UserTools(), "mine")) {
		t.Fatal("still there")
	}
}

func TestToolsPromote(t *testing.T) {
	p := toolsHome(t)
	repo := t.TempDir()
	os.WriteFile(filepath.Join(repo, "embed.go"), nil, 0o644)
	var ran []string
	orig := runGit
	runGit = func(dir string, args ...string) error { ran = append(ran, strings.Join(args, " ")); return nil }
	t.Cleanup(func() { runGit = orig })

	if err := toolsPromote([]string{"mine"}, "", true); err == nil || !strings.Contains(err.Error(), "bud checkout") {
		t.Fatalf("no checkout: %v", err)
	}
	captureStdout(t, func() {
		if err := toolsPromote([]string{"mine"}, repo, true); err != nil {
			t.Fatal(err)
		}
	})
	if !isFile(filepath.Join(repo, "tools", "mine", "init.lua")) || exists(filepath.Join(p.UserTools(), "mine")) {
		t.Fatal("not moved")
	}
	want := []string{"add tools/mine", `commit -m feat: promote the mine tool -- tools/mine`, "push"}
	if !slices.Equal(ran, want) {
		t.Fatalf("git %q, want %q", ran, want)
	}
}
