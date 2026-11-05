package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/paths"
)

// kitFake is claude plugin for the kit tests: what ran, in which config dir,
// and what's installed in each
type kitFake struct {
	ran, dirs []string
	installed map[string][]string
}

// fakeKit swaps in a kit made of files and a claude that keeps track of
// plugins per config dir and takes MCP servers out of its .claude.json
func fakeKit(t *testing.T, files fstest.MapFS) *kitFake {
	t.Helper()
	f := &kitFake{installed: map[string][]string{}}
	dir := func() string {
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			return d
		}
		home, _ := homeClaudeDir()
		return home
	}
	origFS, origClaude, origPlugins, origMarkets := kitFS, kitClaude, installedPlugins, kitMarketplaces
	kitFS = func() fs.FS { return files }
	kitClaude = func(args ...string) error {
		d := dir()
		f.ran = append(f.ran, strings.Join(args, " "))
		f.dirs = append(f.dirs, d)
		id := args[len(args)-1]
		switch args[0] + " " + args[1] {
		case "plugin install":
			f.installed[d] = append(f.installed[d], id)
		case "plugin uninstall":
			f.installed[d] = slices.DeleteFunc(f.installed[d], func(s string) bool { return s == id })
		case "mcp remove":
			var cfg map[string]map[string]any
			data, _ := os.ReadFile(claudeJSONFor(d))
			json.Unmarshal(data, &cfg)
			delete(cfg["mcpServers"], id)
			data, _ = json.Marshal(cfg)
			os.WriteFile(claudeJSONFor(d), data, 0o644)
		}
		return nil
	}
	installedPlugins = func() ([]string, error) { return slices.Clone(f.installed[dir()]), nil }
	kitMarketplaces = func() ([]string, error) { return nil, nil }
	t.Cleanup(func() {
		kitFS, kitClaude, installedPlugins, kitMarketplaces = origFS, origClaude, origPlugins, origMarkets
	})

	return f
}

func TestKitCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := fakeKit(t, fstest.MapFS{"kit.json": {Data: []byte(`{"plugins": ["docs@market"]}`)}})

	out := captureStdout(t, func() {
		if err := kitLs(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "plugin       docs@market") {
		t.Errorf("ls %q", out)
	}
	out = captureStdout(t, func() { kitStatus(nil) })
	if !strings.Contains(out, "docs@market") || !strings.Contains(out, "not synced") {
		t.Errorf("status before %q", out)
	}
	// an update doesn't sync a kit you never synced: with nobody to ask it
	// leaves it, logged out it says when, and on a no nothing changes
	p, _ := paths.Resolve()
	captureStdout(t, func() { resyncKit(&Globals{}, p, nil) })
	if len(f.ran) != 0 {
		t.Fatalf("update synced unasked: %q", f.ran)
	}
	fakeClaude(t, claudeLoggedOut)
	out = captureStdout(t, func() { resyncKit(&Globals{}, p, yesPrompter("y\n")) })
	if len(f.ran) != 0 || !strings.Contains(out, "once Claude Code is logged in") {
		t.Fatalf("logged out: ran %q, out %q", f.ran, out)
	}
	fakeClaude(t, claudeReady)
	out = captureStdout(t, func() { resyncKit(&Globals{}, p, yesPrompter("n\n")) })
	if len(f.ran) != 0 || !strings.Contains(out, "It would add:\n  plugin docs@market") {
		t.Fatalf("update offer on a no: ran %q, out %q", f.ran, out)
	}

	// the first sync into ~/.claude shows what it adds and needs a yes
	out = captureStdout(t, func() {
		if err := kitSync(&Globals{}, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(f.ran) != 0 || !strings.Contains(out, "It would add:\n  plugin docs@market") || !strings.Contains(out, "on a terminal") {
		t.Fatalf("synced unasked: ran %q, out %q", f.ran, out)
	}
	out = captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("n\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	if len(f.ran) != 0 || !strings.Contains(out, "Nothing changed") {
		t.Fatalf("synced on a no: ran %q, out %q", f.ran, out)
	}
	out = captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("y\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	if !slices.Equal(f.ran, []string{"plugin install --scope user docs@market"}) || !strings.Contains(out, "Synced the kit") {
		t.Fatalf("ran %q, out %q", f.ran, out)
	}
	out = captureStdout(t, func() { kitStatus(nil) })
	if !strings.Contains(out, "synced") || strings.Contains(out, "not synced") {
		t.Errorf("status after %q", out)
	}
	// later syncs don't ask again
	out = captureStdout(t, func() {
		if err := kitSync(&Globals{}, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "already has the kit") || f.ran[len(f.ran)-1] != "plugin update --scope user docs@market" {
		t.Errorf("second sync %q, ran %q", out, f.ran)
	}
	f.ran = nil

	// unsync takes it out and forgets the ledger, so an update doesn't put it back
	out = captureStdout(t, func() {
		if err := kitUnsync(nil, true, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(f.ran) != 0 || !strings.Contains(out, "  plugin docs@market") {
		t.Fatalf("dry run: ran %q, out %q", f.ran, out)
	}
	captureStdout(t, func() {
		if err := kitUnsync(nil, false, true); err != nil {
			t.Fatal(err)
		}
	})
	if !slices.Equal(f.ran, []string{"plugin uninstall --scope user docs@market"}) {
		t.Fatalf("unsync ran %q", f.ran)
	}
	if _, err := os.Stat(p.KitLedger()); !os.IsNotExist(err) {
		t.Fatalf("ledger left: %v", err)
	}
	captureStdout(t, func() { resyncKit(&Globals{}, p, nil) })
	if len(f.ran) != 1 {
		t.Fatalf("update synced after unsync: %q", f.ran)
	}

	// on a terminal, an update offers it again, and a yes syncs it
	f.ran = nil
	out = captureStdout(t, func() { resyncKit(&Globals{}, p, yesPrompter("y\n")) })
	if !slices.Equal(f.ran, []string{"plugin install --scope user docs@market"}) || !strings.Contains(out, "Synced the kit") {
		t.Fatalf("update offer: ran %q, out %q", f.ran, out)
	}
	// and once synced, an update on a terminal says so
	out = captureStdout(t, func() { resyncKit(&Globals{}, p, yesPrompter("")) })
	if !strings.Contains(out, "already has the kit") {
		t.Fatalf("update resync %q", out)
	}
}

// bud kit core prints core.md from the bud-plugins checkout, a sync reminds
// you to paste it while it's changed, and --pasted quiets that (JGA-179)
func TestKitCore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	fakeKit(t, fstest.MapFS{"kit.json": {Data: []byte(`{"core": "bud-plugins/core.md"}`)}})

	if err := kitCore(nil, false); err == nil || !strings.Contains(err.Error(), "bud kit sync") {
		t.Fatalf("no checkout: %v", err)
	}
	checkout := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugins", "known_marketplaces.json"), []byte(`{"bud-plugins": {"installLocation": "`+checkout+`"}}`), 0o644)
	os.WriteFile(filepath.Join(checkout, "core.md"), []byte("You are Bud.\n"), 0o644)

	out := captureStdout(t, func() {
		if err := kitCore(nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if out != "You are Bud.\n" {
		t.Fatalf("core %q", out)
	}
	sync := func() string {
		return captureStdout(t, func() {
			if err := syncKitWith(&Globals{}, nil, false); err != nil {
				t.Fatal(err)
			}
		})
	}
	if out := sync(); !strings.Contains(out, "core.md changed") {
		t.Fatalf("no reminder: %q", out)
	}
	captureStdout(t, func() {
		if err := kitCore(nil, true); err != nil {
			t.Fatal(err)
		}
	})
	if out := sync(); strings.Contains(out, "core.md changed") {
		t.Fatalf("reminded after pasting: %q", out)
	}
}
