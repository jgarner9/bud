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

	out = captureStdout(t, func() {
		if err := kitSync(&Globals{}, nil, false); err != nil {
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
}

func TestKitSecrets(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NEEDED_KEY", "")
	t.Setenv("NICE_KEY", "from-env")
	origFS := kitFS
	kitFS = func() fs.FS {
		return fstest.MapFS{
			"kit.json":   {Data: []byte(`{"mcp": ["a", "b"]}`)},
			"mcp/a.json": {Data: []byte(`{"transport": "http", "url": "https://a", "headers": {"K": "$secret:NEEDED_KEY"}}`)},
			"mcp/b.json": {Data: []byte(`{"transport": "http", "url": "https://b", "optional_secrets": true, "headers": {"K": "$secret:NICE_KEY"}}`)},
		}
	}
	t.Cleanup(func() { kitFS = origFS })
	k, env, err := loadKit()
	if err != nil {
		t.Fatal(err)
	}
	all, required := secretNames(k, env.Profile)
	if strings.Join(all, ",") != "NEEDED_KEY,NICE_KEY" || strings.Join(required, ",") != "NEEDED_KEY" {
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
