package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

// fakeInstall lays out a machine bud was installed on with install.sh, with
// every XDG dir in a temp dir, and returns its paths and the binary's path
func fakeInstall(t *testing.T) (*paths.Paths, string) {
	t.Helper()
	// never the real /Applications/Bud.app
	orig := findBundles
	findBundles = func(*paths.Paths) []bundle.Installed { return nil }
	t.Cleanup(func() { findBundles = orig })
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, v := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+v+"_HOME", filepath.Join(root, strings.ToLower(v)))
	}
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := nvim.SyncConfig(fstest.MapFS{"init.lua": {Data: []byte("1")}}, p.Config); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin", "bud")
	for _, f := range []string{
		filepath.Join(p.Data, "lazy", "plugin.lua"),
		filepath.Join(p.UserTools(), "tool", "init.lua"),
		p.MachineConfig(),
		p.Projects(),
		filepath.Join(p.Cache, "log"),
		bin,
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	receipt := `{"method": "install.sh", "path": "` + bin + `"}`
	if err := os.WriteFile(p.InstallReceipt(), []byte(receipt), 0o644); err != nil {
		t.Fatal(err)
	}

	return p, bin
}

func runRemovals(t *testing.T, removals []removal) {
	t.Helper()
	for _, r := range removals {
		if err := r.do(); err != nil {
			t.Fatalf("removing %s: %v", r.path, err)
		}
	}
}

func targets(removals []removal) []string {
	var out []string
	for _, r := range removals {
		out = append(out, r.path)
	}
	return out
}

func TestPlanUninstall(t *testing.T) {
	p, bin := fakeInstall(t)

	removals, kept, err := planUninstall(p, false, bin)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{p.Config, p.Data, p.State, p.Cache, bin}
	if got := targets(removals); !slices.Equal(got, want) {
		t.Fatalf("removes %q, want %q", got, want)
	}
	if len(kept) != 0 {
		t.Fatalf("kept %q, want nothing", kept)
	}

	runRemovals(t, removals)
	for _, path := range want {
		if exists(path) {
			t.Errorf("%s still there", path)
		}
	}

	// a second run finds nothing left
	if removals, _, err := planUninstall(p, false, ""); err != nil || len(removals) != 0 {
		t.Fatalf("after uninstall: %q, %v", targets(removals), err)
	}
}

func TestPlanUninstallKeepConfig(t *testing.T) {
	p, bin := fakeInstall(t)

	removals, kept, err := planUninstall(p, true, bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 {
		t.Fatalf("kept = %q", kept)
	}
	runRemovals(t, removals)

	for _, path := range []string{p.MachineConfig(), filepath.Join(p.UserTools(), "tool", "init.lua")} {
		if !exists(path) {
			t.Errorf("%s was removed", path)
		}
	}
	for _, path := range []string{p.Projects(), filepath.Join(p.Data, "lazy"), p.InstallReceipt(), p.Cache, p.Config, bin} {
		if exists(path) {
			t.Errorf("%s still there", path)
		}
	}
}

func TestPlanUninstallBinaryNotFromInstallScript(t *testing.T) {
	p, bin := fakeInstall(t)
	if err := os.Remove(p.InstallReceipt()); err != nil {
		t.Fatal(err)
	}

	removals, kept, err := planUninstall(p, false, bin)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(targets(removals), bin) {
		t.Fatal("a binary install.sh didn't install must not be removed")
	}
	if len(kept) != 1 || !strings.HasPrefix(kept[0], bin) {
		t.Fatalf("kept = %q, want the binary reported", kept)
	}
}

func TestPlanUninstallOtherBinaryRunning(t *testing.T) {
	// bud from go install running uninstall on a machine install.sh also set up
	p, bin := fakeInstall(t)
	other := filepath.Join(t.TempDir(), "go", "bin", "bud")

	removals, kept, err := planUninstall(p, false, other)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(targets(removals), bin) {
		t.Error("install.sh's binary should still be removed")
	}
	if len(kept) != 1 || !strings.HasPrefix(kept[0], other) {
		t.Fatalf("kept = %q, want the running binary reported", kept)
	}
}

func TestPlanUninstallBadReceipt(t *testing.T) {
	p, _ := fakeInstall(t)
	if err := os.WriteFile(p.InstallReceipt(), []byte(`{"path": "/etc/passwd"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := planUninstall(p, false, ""); err == nil {
		t.Fatal("expected an error for a receipt that doesn't name a bud binary")
	}
}

func TestPlanUninstallBundle(t *testing.T) {
	p, bin := fakeInstall(t)
	app := filepath.Join(t.TempDir(), "Bud.app")
	os.MkdirAll(app, 0o755)
	findBundles = func(*paths.Paths) []bundle.Installed {
		return []bundle.Installed{{Path: app, What: "the Bud app", Files: []string{app}}}
	}

	removals, _, err := planUninstall(p, false, bin)
	if err != nil {
		t.Fatal(err)
	}
	if got := targets(removals); !slices.Contains(got, app) {
		t.Fatalf("removes %q, not the app", got)
	}
	runRemovals(t, removals)
	if exists(app) {
		t.Fatal("app still there")
	}
}

// with a shared ~/.claude (D76) uninstall takes out what the kit added there,
// and nothing of yours; in bud's own dir it goes with the data dir
func TestPlanUninstallKit(t *testing.T) {
	p, bin := fakeInstall(t)
	home := os.Getenv("HOME")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	var ran []string
	origFS, origClaude, origPlugins, origConnectors := kitFS, kitClaude, installedPlugins, kitConnectors
	kitFS = func() fs.FS {
		return fstest.MapFS{
			"kit.json":          {Data: []byte(`{"mcp": ["docs"], "skills": ["s"]}`)},
			"mcp/docs.json":     {Data: []byte(`{"description": "the docs", "transport": "http", "url": "https://d"}`)},
			"skills/s/SKILL.md": {Data: []byte("---\nname: s\n---\n")},
		}
	}
	kitClaude = func(args ...string) error {
		ran = append(ran, strings.Join(args, " "))
		servers := `{"mcpServers": {"docs": {}}}`
		if args[1] == "remove" {
			servers = `{"mcpServers": {}}`
		}
		return os.WriteFile(filepath.Join(home, ".claude.json"), []byte(servers), 0o644)
	}
	installedPlugins = func() ([]string, error) { return nil, nil }
	kitConnectors = func() ([]kit.Connector, error) { return nil, nil }
	t.Cleanup(func() {
		kitFS, kitClaude, installedPlugins, kitConnectors = origFS, origClaude, origPlugins, origConnectors
	})
	mine := filepath.Join(home, ".claude", "skills", "mine", "SKILL.md")
	os.MkdirAll(filepath.Dir(mine), 0o755)
	os.WriteFile(mine, []byte("mine"), 0o644)
	captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("y\n"), false); err != nil {
			t.Fatal(err)
		}
	})

	removals, _, err := planUninstall(p, false, bin)
	if err != nil {
		t.Fatal(err)
	}
	claudeDir := filepath.Join(home, ".claude")
	if removals[0].path != claudeDir || !strings.Contains(removals[0].what, "the 2 things bud's kit added") {
		t.Fatalf("first removal %+v", removals[0])
	}
	runRemovals(t, removals)
	if len(ran) != 2 || ran[1] != "mcp remove --scope user docs" {
		t.Errorf("ran %q", ran)
	}
	if exists(filepath.Join(claudeDir, "skills", "s")) {
		t.Error("the kit's skill is still there")
	}
	if !exists(mine) {
		t.Error("removed your own skill")
	}

	// bud's own Claude Code dir is in the data dir, which goes whole
	t.Setenv("CLAUDE_CONFIG_DIR", p.ClaudeConfig())
	captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("y\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	removals, _, err = planUninstall(p, false, bin)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(targets(removals), p.ClaudeConfig()) {
		t.Errorf("bud's own dir listed on its own: %q", targets(removals))
	}
}
