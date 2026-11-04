package kit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud"
)

func TestShippedKit(t *testing.T) {
	k, err := Load(bud.Kit())
	if err != nil {
		t.Fatal(err)
	}
	// everything about how Claude works comes from bud-plugins (D77)
	if k.Marketplaces["bud-plugins"] != "https://github.com/jgarner9/bud-plugins.git" {
		t.Errorf("marketplaces %v", k.Marketplaces)
	}
	for _, id := range []string{"bud@bud-plugins", "bud-ide@bud-plugins", "superpowers@anthropic-plugin-directory"} {
		if !slices.Contains(k.Plugins, id) {
			t.Errorf("no %s in %v", id, k.Plugins)
		}
	}
	if k.OutputStyle != "bud-ide:bud" {
		t.Errorf("output style %q", k.OutputStyle)
	}
}

func TestWithoutBlock(t *testing.T) {
	block := blockStart + "\nbe a bud\n" + blockEnd + "\n"
	for doc, want := range map[string]string{
		"# mine\n\n" + block:                      "# mine\n",
		"# mine\n\n" + block + "\nmore of mine\n": "# mine\n\nmore of mine\n",
		block:          "",
		"# no block\n": "# no block\n",
	} {
		if got := withoutBlock(doc); got != want {
			t.Errorf("withoutBlock(%q) = %q, want %q", doc, got, want)
		}
	}
}

type fakeClaude struct {
	ran     []string
	servers map[string]string
	json    string
}

func (f *fakeClaude) run(args ...string) error {
	// args are mcp <add-json|remove> --scope user <name> [json]
	f.ran = append(f.ran, args[0]+" "+args[1]+" "+args[4])
	switch args[1] {
	case "add-json":
		f.servers[args[4]] = args[5]
	case "remove":
		delete(f.servers, args[4])
	}
	data, _ := json.Marshal(map[string]any{"mcpServers": f.servers})
	return os.WriteFile(f.json, data, 0o644)
}

// what a bud from before D77 synced (MCP servers, files, the CLAUDE.md block)
// comes out, and only bud's
func TestSyncRemovesLegacy(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	fc := &fakeClaude{servers: map[string]string{"docs": "{}", "mine": "{}"}, json: filepath.Join(dir, ".claude.json")}
	fc.run("mcp", "add-json", "--scope", "user", "mine", "{}")
	fc.ran = nil
	env := Env{ClaudeDir: claudeDir, ClaudeJSON: fc.json, Ledger: filepath.Join(dir, "ledger.json"), Claude: fc.run}
	write := func(rel, body string) {
		p := filepath.Join(claudeDir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("CLAUDE.md", "# mine\n\n"+blockStart+"\nbe a bud\n"+blockEnd+"\n")
	write("skills/lookup/SKILL.md", "the kit's")
	write("agents/code-reviewer.md", "edited by you")
	write("skills/notes/SKILL.md", "yours")
	// "gone" was removed by hand already
	os.WriteFile(env.Ledger, []byte(`{"mcp": {"docs": "x", "gone": "y"}, "instructions": "z", "files": {
		"skills/lookup/SKILL.md": "`+hash([]byte("the kit's"))+`",
		"agents/code-reviewer.md": "`+hash([]byte("what bud wrote"))+`"}}`), 0o644)

	actions, err := Sync(&Kit{}, env, true)
	if err != nil || len(fc.ran) != 0 {
		t.Fatalf("dry run ran %q (%v)", fc.ran, err)
	}
	if len(actions) != 5 {
		t.Fatalf("dry run actions %v", actions)
	}

	if _, err := Sync(&Kit{}, env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fc.ran, []string{"mcp remove docs"}) {
		t.Fatalf("claude ran %q", fc.ran)
	}
	if _, ok := fc.servers["mine"]; !ok {
		t.Fatal("removed your own server")
	}
	if md, _ := os.ReadFile(filepath.Join(claudeDir, "CLAUDE.md")); string(md) != "# mine\n" {
		t.Fatalf("CLAUDE.md %q", md)
	}
	if exists(filepath.Join(claudeDir, "skills", "lookup")) {
		t.Error("the kit's skill is still there")
	}
	for _, rel := range []string{"agents/code-reviewer.md", "skills/notes/SKILL.md"} {
		if !exists(filepath.Join(claudeDir, filepath.FromSlash(rel))) {
			t.Errorf("removed %s, which is yours", rel)
		}
	}
	l, _, _ := ReadLedger(env.Ledger)
	if len(l.MCP) != 0 || len(l.Files) != 0 || l.Instructions != "" {
		t.Fatalf("ledger kept %+v", l)
	}
	actions, _ = Sync(&Kit{}, env, false)
	if len(actions) != 0 {
		t.Fatalf("again: %v", actions)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestSyncPlugins(t *testing.T) {
	dir := t.TempDir()
	installed := []string{"mine@somewhere"}
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		Claude: func(a ...string) error {
			ran = append(ran, strings.Join(a, " "))
			if a[1] == "install" {
				installed = append(installed, a[4])
			} else {
				installed = slices.DeleteFunc(installed, func(s string) bool { return s == a[4] })
			}
			return nil
		},
		InstalledPlugins: func() ([]string, error) { return slices.Clone(installed), nil },
	}
	k := &Kit{Plugins: []string{"superpowers@claude-plugins-official", "mine@somewhere"}}
	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, []string{"plugin install --scope user superpowers@claude-plugins-official"}) {
		t.Fatalf("ran %q (%v)", ran, actions)
	}
	// dropped from the kit: uninstalled, but never the one you installed yourself
	k.Plugins = nil
	ran = nil
	Sync(k, env, false)
	if !slices.Equal(ran, []string{"plugin uninstall --scope user superpowers@claude-plugins-official"}) || !slices.Contains(installed, "mine@somewhere") {
		t.Fatalf("ran %q, installed %v", ran, installed)
	}
}

// fakeMarketplaces is claude plugin with marketplaces: it records what ran,
// and what's registered and installed
type fakeMarketplaces struct {
	ran        []string
	registered []string
	installed  []string
	fail       string // a command line that fails
}

func (f *fakeMarketplaces) env(dir string) Env {
	return Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		Claude: func(a ...string) error {
			line := strings.Join(a, " ")
			f.ran = append(f.ran, line)
			if line == f.fail {
				return errors.New("offline")
			}
			switch {
			case a[1] == "marketplace" && a[2] == "add":
				// the name comes from the repo's marketplace.json
				f.registered = append(f.registered, "bud-plugins")
			case a[1] == "marketplace" && a[2] == "remove":
				f.registered = slices.DeleteFunc(f.registered, func(s string) bool { return s == a[3] })
			case a[1] == "install":
				f.installed = append(f.installed, a[4])
			case a[1] == "uninstall":
				f.installed = slices.DeleteFunc(f.installed, func(s string) bool { return s == a[4] })
			}
			return nil
		},
		InstalledPlugins: func() ([]string, error) { return slices.Clone(f.installed), nil },
		Marketplaces:     func() ([]string, error) { return slices.Clone(f.registered), nil },
	}
}

func TestSyncMarketplaces(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMarketplaces{}
	env := f.env(dir)
	src := "https://github.com/jgarner9/bud-plugins.git"
	k := &Kit{Marketplaces: map[string]string{"bud-plugins": src}, Plugins: []string{"bud-ide@bud-plugins"}}

	// a dry run says what it would do and does nothing
	actions, err := Sync(k, env, true)
	if err != nil || len(f.ran) != 0 {
		t.Fatalf("dry run ran %q (%v)", f.ran, err)
	}
	if !slices.Contains(actions, Action{"marketplace", "bud-plugins", "add", ""}) {
		t.Fatalf("dry run actions %v", actions)
	}

	// the first sync adds the marketplace before installing from it
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	want := []string{"plugin marketplace add " + src, "plugin install --scope user bud-ide@bud-plugins"}
	if !slices.Equal(f.ran, want) {
		t.Fatalf("ran %q, want %q", f.ran, want)
	}
	l, _, _ := ReadLedger(env.Ledger)
	if l.Marketplaces["bud-plugins"] != src {
		t.Fatalf("ledger %+v", l.Marketplaces)
	}

	// later syncs refresh it and the plugins bud installed from it
	f.ran = nil
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	want = []string{"plugin marketplace update bud-plugins", "plugin update --scope user bud-ide@bud-plugins"}
	if !slices.Equal(f.ran, want) {
		t.Fatalf("refresh ran %q, want %q", f.ran, want)
	}

	// offline: a refresh that fails is a skip, not a failed sync
	f.ran, f.fail = nil, "plugin marketplace update bud-plugins"
	actions, err = Sync(k, env, false)
	if err != nil {
		t.Fatalf("a failed refresh failed the sync: %v", err)
	}
	if i := slices.IndexFunc(actions, func(a Action) bool { return a.Kind == "marketplace" }); i < 0 || actions[i].Do != "skip" {
		t.Fatalf("actions %v", actions)
	}
	if slices.Contains(f.ran, "plugin update --scope user bud-ide@bud-plugins") {
		t.Fatalf("updated a plugin from a marketplace that didn't refresh: %q", f.ran)
	}
	f.fail = ""

	// dropped from the kit: bud's plugins from it go first, then the marketplace
	f.ran = nil
	if _, err := Sync(&Kit{}, env, false); err != nil {
		t.Fatal(err)
	}
	want = []string{"plugin uninstall --scope user bud-ide@bud-plugins", "plugin marketplace remove bud-plugins"}
	if !slices.Equal(f.ran, want) {
		t.Fatalf("removal ran %q, want %q", f.ran, want)
	}
	if l, _, _ := ReadLedger(env.Ledger); len(l.Marketplaces) != 0 || len(l.Plugins) != 0 {
		t.Fatalf("ledger kept %+v %+v", l.Marketplaces, l.Plugins)
	}
}

// a marketplace of the same name you added (clod's local checkout, or the one
// claude.ai syncs) is yours: never refreshed or removed, and the kit's plugins
// install from it
func TestSyncMarketplaceYouAdded(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMarketplaces{registered: []string{"bud-plugins"}, installed: []string{"bud@bud-plugins"}}
	env := f.env(dir)
	k := &Kit{
		Marketplaces: map[string]string{"bud-plugins": "https://github.com/jgarner9/bud-plugins.git"},
		Plugins:      []string{"bud@bud-plugins", "bud-ide@bud-plugins"},
	}
	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ran, []string{"plugin install --scope user bud-ide@bud-plugins"}) {
		t.Fatalf("ran %q", f.ran)
	}
	for _, want := range []Action{
		{"marketplace", "bud-plugins", "skip", "you added it yourself"},
		{"plugin", "bud@bud-plugins", "skip", "you installed it yourself"},
	} {
		if !slices.Contains(actions, want) {
			t.Errorf("no %v in %v", want, actions)
		}
	}

	f.ran = nil
	if _, err := Unsync(env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ran, []string{"plugin uninstall --scope user bud-ide@bud-plugins"}) {
		t.Fatalf("unsync ran %q", f.ran)
	}
	if !slices.Equal(f.registered, []string{"bud-plugins"}) || !slices.Equal(f.installed, []string{"bud@bud-plugins"}) {
		t.Fatalf("unsync touched yours: %v %v", f.registered, f.installed)
	}
}

func TestParseMarketplaces(t *testing.T) {
	out := `[{"name": "bud-plugins", "source": "git", "url": "https://github.com/jgarner9/bud-plugins.git", "installLocation": "/x"}, {"name": "local", "source": "directory"}]`
	names, err := ParseMarketplaces([]byte(out))
	if err != nil || !slices.Equal(names, []string{"bud-plugins", "local"}) {
		t.Fatalf("%v %v", names, err)
	}
	if _, err := ParseMarketplaces([]byte("not json")); err == nil {
		t.Fatal("no error for bad output")
	}
}
