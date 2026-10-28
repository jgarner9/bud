package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/paths"
)

func TestClaudeConfigDir(t *testing.T) {
	p := &paths.Paths{Data: "/data"}
	s := config.Defaults()
	if got := claudeConfigDir(p, s); got != "/data/claude" {
		t.Errorf("default %q", got)
	}
	s.AI.Claude.Config = "home"
	if got := claudeConfigDir(p, s); got != "" {
		t.Errorf("home %q", got)
	}
}

// the kit was synced into ~/.claude before bud had its own dir: a sync takes
// it out of there (on a yes) and puts it in bud's
func TestMoveKit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	budDir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", budDir)
	p, _ := paths.Resolve()

	skill := []byte("---\nname: s\n---\n")
	var ran []string
	origFS, origClaude, origPlugins, origConnectors := kitFS, kitClaude, installedPlugins, kitConnectors
	kitFS = func() fs.FS {
		return fstest.MapFS{
			"kit.json":          {Data: []byte(`{"mcp": ["docs"], "skills": ["s"]}`)},
			"mcp/docs.json":     {Data: []byte(`{"description": "the docs", "transport": "http", "url": "https://d"}`)},
			"skills/s/SKILL.md": {Data: skill},
		}
	}
	kitClaude = func(args ...string) error {
		ran = append(ran, os.Getenv("CLAUDE_CONFIG_DIR")+": "+strings.Join(args[:2], " "))
		return nil
	}
	installedPlugins = func() ([]string, error) { return nil, nil }
	kitConnectors = func() ([]kit.Connector, error) { return nil, nil }
	t.Cleanup(func() {
		kitFS, kitClaude, installedPlugins, kitConnectors = origFS, origClaude, origPlugins, origConnectors
	})

	// what an older bud left: the kit in ~/.claude, a ledger without a dir
	sync := func(pr *prompter) string {
		t.Helper()
		return captureStdout(t, func() {
			if err := syncKitWith(&Globals{}, pr, false); err != nil {
				t.Fatal(err)
			}
		})
	}
	old := filepath.Join(home, ".claude")
	os.MkdirAll(filepath.Join(old, "skills", "s"), 0o755)
	os.WriteFile(filepath.Join(old, "skills", "s", "SKILL.md"), skill, 0o644)
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers": {"docs": {}}}`), 0o644)
	sum := sha256.Sum256(skill)
	writeLedger := func() {
		os.MkdirAll(p.State, 0o755)
		os.WriteFile(p.KitLedger(), []byte(`{"mcp": {"docs": "x"}, "files": {"skills/s/SKILL.md": "`+hex.EncodeToString(sum[:8])+`"}}`), 0o644)
	}
	writeLedger()

	// not logged in to bud's dir yet: nothing changes
	fakeClaude(t, claudeLoggedOut)
	out := sync(yesPrompter("y\n"))
	if len(ran) != 0 || !strings.Contains(out, "bud claude setup") {
		t.Fatalf("ran %q, out %q", ran, out)
	}

	// nobody to ask: nothing changes
	fakeClaude(t, claudeReady)
	out = sync(nil)
	if len(ran) != 0 || !strings.Contains(out, "on a terminal") {
		t.Fatalf("ran %q, out %q", ran, out)
	}

	// a no: left in ~/.claude, the kit goes into bud's dir
	out = sync(yesPrompter("n\n"))
	if !strings.Contains(out, "Left in") || !isFile(filepath.Join(old, "skills", "s", "SKILL.md")) {
		t.Fatalf("out %q", out)
	}
	if len(ran) != 1 || ran[0] != budDir+": mcp add-json" || !isFile(filepath.Join(budDir, "skills", "s", "SKILL.md")) {
		t.Fatalf("ran %q", ran)
	}

	// a yes: removed from ~/.claude (claude run without CLAUDE_CONFIG_DIR there)
	ran = nil
	os.RemoveAll(budDir)
	writeLedger()
	out = sync(yesPrompter("y\n"))
	if isFile(filepath.Join(old, "skills", "s", "SKILL.md")) {
		t.Fatalf("skill left in ~/.claude: %q", out)
	}
	if len(ran) != 2 || ran[0] != ": mcp remove" || ran[1] != budDir+": mcp add-json" {
		t.Fatalf("ran %q", ran)
	}
	l, _, _ := kit.ReadLedger(p.KitLedger())
	if l.Dir != budDir || l.Files["skills/s/SKILL.md"] == "" {
		t.Fatalf("ledger %+v", l)
	}

	// synced where it is: nothing asked
	ran = nil
	out = sync(nil)
	if strings.Contains(out, "keeps its own") {
		t.Fatalf("asked again: %q", out)
	}
}
