package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/projects"
)

// targetJSON runs issue target --json and decodes what it printed
func targetJSON(t *testing.T, args []string, bud bool) target {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = issueTarget(args, bud, true) })
	if err != nil {
		t.Fatal(err)
	}
	var got target
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%q: %v", out, err)
	}

	return got
}

func TestIssueTarget(t *testing.T) {
	p := layoutHome(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// nothing set: no target, but the dir is still the root
	if got := targetJSON(t, []string{dir}, false); got != (target{Root: dir}) {
		t.Fatalf("unset: %+v", got)
	}
	if err := issueTarget([]string{dir}, false, false); err == nil || !strings.Contains(err.Error(), "bud issue set --team") {
		t.Fatalf("unset, text: %v", err)
	}

	// config.json's for every project
	captureStdout(t, func() {
		if err := issueSet(nil, "ENG", "Web", true); err != nil {
			t.Fatal(err)
		}
	})
	if got := targetJSON(t, []string{dir}, false); got != (target{Team: "ENG", Project: "Web", Source: "config", Root: dir}) {
		t.Fatalf("config: %+v", got)
	}

	// a project's own wins, and registers the dir
	captureStdout(t, func() {
		if err := issueSet([]string{dir}, "APP", "", false); err != nil {
			t.Fatal(err)
		}
	})
	r, _ := projects.Load(p.Projects())
	if proj, ok := r.Get(dir); !ok || proj.Issues == nil || proj.Issues.Team != "APP" {
		t.Fatalf("projects.json: %+v", r.Projects)
	}
	if got := targetJSON(t, []string{dir}, false); got != (target{Team: "APP", Source: "project", Root: dir}) {
		t.Fatalf("project: %+v", got)
	}

	// unset falls back to config.json's
	captureStdout(t, func() {
		if err := issueUnset([]string{dir}); err != nil {
			t.Fatal(err)
		}
	})
	if got := targetJSON(t, []string{dir}, false); got.Source != "config" {
		t.Fatalf("after unset: %+v", got)
	}
}

func TestIssueTargetBud(t *testing.T) {
	p := layoutHome(t)
	checkout := t.TempDir()
	// Link wants something that looks like a bud checkout
	os.MkdirAll(filepath.Join(checkout, "nvim"), 0o755)
	os.WriteFile(filepath.Join(checkout, "nvim", "init.lua"), nil, 0o644)
	os.MkdirAll(filepath.Join(checkout, "languages"), 0o755)

	// bud's own tracker by default, with no checkout until one is dev-linked
	if got := targetJSON(t, nil, true); got != (target{Team: "JGA", Project: "Bud IDE", Source: "bud"}) {
		t.Fatalf("default: %+v", got)
	}
	if err := nvim.Link(checkout, p.Config); err != nil {
		t.Fatal(err)
	}
	if got := targetJSON(t, nil, true); got.Root != checkout {
		t.Fatalf("linked root is %q, want %q", got.Root, checkout)
	}

	// cleared, there's no target
	f, _ := config.Load(p.MachineConfig())
	f.Set("ai.issues.bud.team", "")
	f.Save()
	if got := targetJSON(t, nil, true); got.Source != "" {
		t.Fatalf("cleared: %+v", got)
	}
}

func TestIssueSetUsage(t *testing.T) {
	layoutHome(t)
	var usage *UsageError
	if err := issueSet(nil, "", "", false); !errors.As(err, &usage) {
		t.Fatalf("no --team: %v", err)
	}
	if err := issueSet([]string{"x"}, "T", "", true); !errors.As(err, &usage) {
		t.Fatalf("--global with a project: %v", err)
	}
	if err := issueSet([]string{"/no/such/dir"}, "T", "", false); err == nil {
		t.Fatal("a missing dir was registered")
	}
}

// budRepo makes a dir that looks like a bud git checkout
func budRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "nvim"), 0o755)
	os.WriteFile(filepath.Join(dir, "nvim", "init.lua"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, "languages"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	return dir
}

// <leader>aB remembers the bud checkout, and checks it each time (JGA-226)
func TestIssueCheckoutIsRememberedAndChecked(t *testing.T) {
	layoutHome(t)
	checkout := budRepo(t)

	if err := issueCheckout([]string{filepath.Join(t.TempDir(), "nope")}, false); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("a missing dir should be refused: %v", err)
	}
	notBud := t.TempDir()
	if err := issueCheckout([]string{notBud}, false); err == nil || !strings.Contains(err.Error(), "isn't a bud checkout") {
		t.Fatalf("a dir that isn't bud should be refused: %v", err)
	}
	if got := targetJSON(t, nil, true); got.Root != "" || got.Stale != "" {
		t.Fatalf("nothing saved yet: %+v", got)
	}

	captureStdout(t, func() {
		if err := issueCheckout([]string{checkout}, false); err != nil {
			t.Fatal(err)
		}
	})
	if got := targetJSON(t, nil, true); got.Root != checkout || got.Stale != "" {
		t.Fatalf("saved: %+v", got)
	}

	// moved: not used, and reported with why
	moved := checkout + "-moved"
	if err := os.Rename(checkout, moved); err != nil {
		t.Fatal(err)
	}
	got := targetJSON(t, nil, true)
	if got.Root != "" || got.Stale != checkout || !strings.Contains(got.StaleWhy, "moved or removed") {
		t.Fatalf("moved: %+v", got)
	}
	if err := issueCheckout(nil, false); err == nil || !strings.Contains(err.Error(), "unusable") {
		t.Fatalf("status of a gone checkout: %v", err)
	}

	// back in place: used again
	os.Rename(moved, checkout)
	if got := targetJSON(t, nil, true); got.Root != checkout {
		t.Fatalf("restored: %+v", got)
	}

	// forgotten
	captureStdout(t, func() {
		if err := issueCheckout(nil, true); err != nil {
			t.Fatal(err)
		}
	})
	if got := targetJSON(t, nil, true); got.Root != "" || got.Stale != "" {
		t.Fatalf("forgotten: %+v", got)
	}
}
