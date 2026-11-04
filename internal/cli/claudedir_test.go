package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
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
	// bud shares ~/.claude unless told to keep its own (D77)
	if got := claudeConfigDir(p, s); got != "" {
		t.Errorf("default %q", got)
	}
	s.AI.Claude.Config = "bud"
	if got := claudeConfigDir(p, s); got != "/data/claude" {
		t.Errorf("bud %q", got)
	}
}

// the kit was synced into ~/.claude by an older bud, with an MCP server and a
// skill then: a sync into another dir takes them out of there (on a yes)
// and syncs the kit in the new one
func TestMoveKit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	budDir := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", budDir)
	p, _ := paths.Resolve()
	f := fakeKit(t, fstest.MapFS{"kit.json": {Data: []byte(`{"plugins": ["docs@market"]}`)}})
	ran := func() []string {
		var out []string
		for i, r := range f.ran {
			out = append(out, f.dirs[i]+": "+r)
		}
		return out
	}

	sync := func(pr *prompter) string {
		t.Helper()
		return captureStdout(t, func() {
			if err := syncKitWith(&Globals{}, pr, false); err != nil {
				t.Fatal(err)
			}
		})
	}
	old := filepath.Join(home, ".claude")
	skill := []byte("---\nname: s\n---\n")
	sum := sha256.Sum256(skill)
	leave := func() {
		os.MkdirAll(filepath.Join(old, "skills", "s"), 0o755)
		os.WriteFile(filepath.Join(old, "skills", "s", "SKILL.md"), skill, 0o644)
		os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers": {"docs": {}}}`), 0o644)
		os.MkdirAll(p.State, 0o755)
		os.WriteFile(p.KitLedger(), []byte(`{"mcp": {"docs": "x"}, "files": {"skills/s/SKILL.md": "`+hex.EncodeToString(sum[:8])+`"}}`), 0o644)
	}
	leave()

	// not logged in to the new dir yet: nothing changes
	fakeClaude(t, claudeLoggedOut)
	out := sync(yesPrompter("y\n"))
	if len(f.ran) != 0 || !strings.Contains(out, "bud claude setup") {
		t.Fatalf("ran %q, out %q", f.ran, out)
	}

	// nobody to ask: nothing changes
	fakeClaude(t, claudeReady)
	out = sync(nil)
	if len(f.ran) != 0 || !strings.Contains(out, "on a terminal") {
		t.Fatalf("ran %q, out %q", f.ran, out)
	}

	// a no: left in ~/.claude, the kit goes into the new dir
	out = sync(yesPrompter("n\n"))
	if !strings.Contains(out, "Left in") || !isFile(filepath.Join(old, "skills", "s", "SKILL.md")) {
		t.Fatalf("out %q", out)
	}
	if got := ran(); !slices.Equal(got, []string{budDir + ": plugin install --scope user docs@market"}) {
		t.Fatalf("ran %q", got)
	}

	// a yes: removed from ~/.claude (claude run without CLAUDE_CONFIG_DIR there)
	f.ran, f.dirs, f.installed = nil, nil, map[string][]string{}
	leave()
	out = sync(yesPrompter("y\n"))
	if isFile(filepath.Join(old, "skills", "s", "SKILL.md")) {
		t.Fatalf("skill left in ~/.claude: %q", out)
	}
	want := []string{old + ": mcp remove --scope user docs", budDir + ": plugin install --scope user docs@market"}
	if got := ran(); !slices.Equal(got, want) {
		t.Fatalf("ran %q, want %q", got, want)
	}
	l, _, _ := kit.ReadLedger(p.KitLedger())
	if l.Dir != budDir || l.Plugins["docs@market"] == "" || len(l.MCP) != 0 || len(l.Files) != 0 {
		t.Fatalf("ledger %+v", l)
	}

	// synced where it is: nothing asked
	out = sync(nil)
	if strings.Contains(out, "uses the Claude Code config") {
		t.Fatalf("asked again: %q", out)
	}
}

// a machine that never set ai.claude.config had the kit in bud's own dir;
// with sharing the default (D77), the next sync moves it into ~/.claude,
// asking at both ends
func TestMoveKitToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	p, _ := paths.Resolve()
	budDir := p.ClaudeConfig()
	f := fakeKit(t, fstest.MapFS{"kit.json": {Data: []byte(`{"plugins": ["kitted@market"]}`)}})
	f.installed[budDir] = []string{"kitted@market"}
	os.MkdirAll(p.State, 0o755)
	os.WriteFile(p.KitLedger(), []byte(`{"plugins": {"kitted@market": "installed"}, "dir": "`+budDir+`"}`), 0o644)
	fakeClaude(t, claudeReady)

	captureStdout(t, func() {
		if err := syncKitWith(&Globals{}, yesPrompter("y\ny\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	home = filepath.Join(home, ".claude")
	if !slices.Equal(f.ran, []string{"plugin uninstall --scope user kitted@market", "plugin install --scope user kitted@market"}) || !slices.Equal(f.dirs, []string{budDir, home}) {
		t.Fatalf("ran %q in %q", f.ran, f.dirs)
	}
	if l, _, _ := kit.ReadLedger(p.KitLedger()); l.Dir != home {
		t.Fatalf("ledger dir %q", l.Dir)
	}
}
