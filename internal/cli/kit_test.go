package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/paths"
)

func TestKitCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var ran []string
	origFS, origClaude := kitFS, kitClaude
	kitFS = func() fs.FS {
		return fstest.MapFS{
			"kit.json":      {Data: []byte(`{"mcp": ["docs"]}`)},
			"mcp/docs.json": {Data: []byte(`{"description": "the docs", "transport": "http", "url": "https://d"}`)},
		}
	}
	kitClaude = func(args ...string) error {
		ran = append(ran, strings.Join(args, " "))
		return os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers": {"docs": {}}}`), 0o644)
	}
	origPlugins, origConnectors := installedPlugins, kitConnectors
	installedPlugins = func() ([]string, error) { return nil, nil }
	kitConnectors = func() ([]kit.Connector, error) { return nil, nil }
	t.Cleanup(func() {
		kitFS, kitClaude = origFS, origClaude
		installedPlugins, kitConnectors = origPlugins, origConnectors
	})

	out := captureStdout(t, func() {
		if err := kitLs(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "mcp          docs             the docs") {
		t.Errorf("ls %q", out)
	}
	out = captureStdout(t, func() { kitStatus(nil) })
	if !strings.Contains(out, "docs") || !strings.Contains(out, "not synced") {
		t.Errorf("status before %q", out)
	}
	// an update doesn't sync a kit you never synced
	p, _ := paths.Resolve()
	captureStdout(t, func() { resyncKit(&Globals{}, p) })
	if len(ran) != 0 {
		t.Fatalf("update synced unasked: %q", ran)
	}

	// the first sync into ~/.claude shows what it adds and needs a yes
	out = captureStdout(t, func() {
		if err := kitSync(&Globals{}, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(ran) != 0 || !strings.Contains(out, "It would add:\n  mcp docs") || !strings.Contains(out, "on a terminal") {
		t.Fatalf("synced unasked: ran %q, out %q", ran, out)
	}
	out = captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("n\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	if len(ran) != 0 || !strings.Contains(out, "Nothing changed") {
		t.Fatalf("synced on a no: ran %q, out %q", ran, out)
	}
	out = captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("y\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	if len(ran) != 1 || !strings.HasPrefix(ran[0], "mcp add-json --scope user docs {") || !strings.Contains(out, "Synced the kit") {
		t.Fatalf("ran %q, out %q", ran, out)
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
	if !strings.Contains(out, "already has the kit") {
		t.Errorf("second sync %q", out)
	}

	// unsync takes it out and forgets the ledger, so an update doesn't put it back
	out = captureStdout(t, func() {
		if err := kitUnsync(nil, true, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(ran) != 1 || !strings.Contains(out, "  mcp docs") {
		t.Fatalf("dry run: ran %q, out %q", ran, out)
	}
	captureStdout(t, func() {
		if err := kitUnsync(nil, false, true); err != nil {
			t.Fatal(err)
		}
	})
	if len(ran) != 2 || ran[1] != "mcp remove --scope user docs" {
		t.Fatalf("unsync ran %q", ran)
	}
	if _, err := os.Stat(p.KitLedger()); !os.IsNotExist(err) {
		t.Fatalf("ledger left: %v", err)
	}
	captureStdout(t, func() { resyncKit(&Globals{}, p) })
	if len(ran) != 2 {
		t.Fatalf("update synced after unsync: %q", ran)
	}
}

func TestKitSecrets(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NEEDED_KEY", "")
	t.Setenv("NICE_KEY", "from-env")
	origFS := kitFS
	kitFS = func() fs.FS {
		return fstest.MapFS{
			"kit.json":   {Data: []byte(`{"mcp": ["a", "b", "c"]}`)},
			"mcp/a.json": {Data: []byte(`{"transport": "http", "url": "https://a", "headers": {"K": "$secret:NEEDED_KEY"}}`)},
			"mcp/b.json": {Data: []byte(`{"transport": "http", "url": "https://b", "optional_secrets": true, "headers": {"K": "$secret:NICE_KEY"}}`)},
			"mcp/c.json": {Data: []byte(`{"transport": "http", "url": "$secret:HOST_URL", "optional_secrets": true}`)},
		}
	}
	t.Cleanup(func() { kitFS = origFS })
	k, env, err := loadKit()
	if err != nil {
		t.Fatal(err)
	}
	all, required := secretNames(k, env.Profile)
	// a URL secret is needed even in an entry whose others are optional
	if strings.Join(all, ",") != "HOST_URL,NEEDED_KEY,NICE_KEY" || strings.Join(required, ",") != "HOST_URL,NEEDED_KEY" {
		t.Fatalf("all %v, required %v", all, required)
	}
	// an env var is already in the session's environment, so it isn't handed over again
	vars, err := keychainEnv()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := vars["NICE_KEY"]; ok {
		t.Fatalf("handed over an env var: %v", vars)
	}
	// nobody to ask: nothing asked, nothing fails
	if err := askKitSecrets(nil); err != nil {
		t.Fatal(err)
	}
}
