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
	for _, id := range []string{"bud@bud-plugins", "bud-ide@bud-plugins", "superpowers@bud-plugins"} {
		if !slices.Contains(k.Plugins, id) {
			t.Errorf("no %s in %v", id, k.Plugins)
		}
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
		InstalledPlugins: func() ([]Plugin, error) { return asPlugins(installed), nil },
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

func TestPluginMovesToTheKitsMarketplaceUnlessYouInstalledItElsewhere(t *testing.T) {
	// bud's own copy from the old marketplace: the new one goes in, the old
	// one comes out, all in one sync
	f := &fakeMarketplaces{}
	env := f.env(t.TempDir())
	if _, err := Sync(&Kit{Plugins: []string{"superpowers@anthropic-plugin-directory"}}, env, false); err != nil {
		t.Fatal(err)
	}
	f.ran = nil
	if _, err := Sync(&Kit{Plugins: []string{"superpowers@bud-plugins"}}, env, false); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"plugin install --scope user superpowers@bud-plugins",
		"plugin uninstall --scope user superpowers@anthropic-plugin-directory",
	}
	if !slices.Equal(f.ran, want) || !slices.Equal(f.installed, []string{"superpowers@bud-plugins"}) {
		t.Fatalf("ran %q, installed %v; want %q", f.ran, f.installed, want)
	}

	// the new one fails to install: bud's old copy stays until it's in
	f = &fakeMarketplaces{}
	env = f.env(t.TempDir())
	if _, err := Sync(&Kit{Plugins: []string{"superpowers@anthropic-plugin-directory"}}, env, false); err != nil {
		t.Fatal(err)
	}
	f.ran, f.fail = nil, "plugin install --scope user superpowers@bud-plugins"
	actions, err := Sync(&Kit{Plugins: []string{"superpowers@bud-plugins"}}, env, false)
	if err == nil || !slices.Equal(f.installed, []string{"superpowers@anthropic-plugin-directory"}) {
		t.Fatalf("err %v, installed %v; want the failure and the old copy kept", err, f.installed)
	}
	if last := actions[len(actions)-1]; last.Do != "skip" || last.Name != "superpowers@anthropic-plugin-directory" {
		t.Fatalf("actions %v; want the old copy skipped", actions)
	}
	f.ran, f.fail = nil, ""
	if _, err := Sync(&Kit{Plugins: []string{"superpowers@bud-plugins"}}, env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ran, want) || !slices.Equal(f.installed, []string{"superpowers@bud-plugins"}) {
		t.Fatalf("next sync: ran %q, installed %v; want %q", f.ran, f.installed, want)
	}

	// the kit drops the old copy's marketplace too: it stays while the copy does
	f = &fakeMarketplaces{}
	env = f.env(t.TempDir())
	src := "https://github.com/jgarner9/bud-plugins.git"
	if _, err := Sync(&Kit{Marketplaces: map[string]string{"bud-plugins": src}, Plugins: []string{"docs@bud-plugins"}}, env, false); err != nil {
		t.Fatal(err)
	}
	f.fail = "plugin install --scope user docs@other"
	Sync(&Kit{Plugins: []string{"docs@other"}}, env, false)
	if !slices.Equal(f.registered, []string{"bud-plugins"}) || !slices.Equal(f.installed, []string{"docs@bud-plugins"}) {
		t.Fatalf("registered %v, installed %v; want the old copy and its marketplace kept", f.registered, f.installed)
	}
	f.fail = ""
	if _, err := Sync(&Kit{Plugins: []string{"docs@other"}}, env, false); err != nil {
		t.Fatal(err)
	}
	if len(f.registered) != 0 || !slices.Equal(f.installed, []string{"docs@other"}) {
		t.Fatalf("next sync: registered %v, installed %v; want both old ones gone", f.registered, f.installed)
	}

	// one you installed yourself from another marketplace stays, and bud
	// doesn't add a second copy beside it
	f = &fakeMarketplaces{installed: []string{"superpowers@anthropic-plugin-directory"}}
	actions, err = Sync(&Kit{Plugins: []string{"superpowers@bud-plugins"}}, f.env(t.TempDir()), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ran) != 0 || len(actions) != 1 || actions[0].Do != "skip" {
		t.Fatalf("ran %q, actions %v; want one skip and nothing run", f.ran, actions)
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
		InstalledPlugins: func() ([]Plugin, error) { return asPlugins(f.installed), nil },
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
	// bud's plugin is still brought up to date from the copy that's there
	if !slices.Contains(f.ran, "plugin update --scope user bud-ide@bud-plugins") {
		t.Fatalf("didn't update bud's plugin: %q", f.ran)
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

	// later syncs update bud's plugin from your marketplace (JGA-174), and
	// never refresh the marketplace or touch your plugin
	f.ran = nil
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.ran, []string{"plugin update --scope user bud-ide@bud-plugins"}) {
		t.Fatalf("second sync ran %q", f.ran)
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

// a plugin claude.ai syncs loads as name@synced (JGA-177): the kit leaves it
// be, and takes out its own copy if it had installed one before
func TestSyncPluginFromClaudeAI(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMarketplaces{registered: []string{"bud-plugins"}, installed: []string{"bud@synced", "bud@bud-plugins"}}
	env := f.env(dir)
	os.WriteFile(env.Ledger, []byte(`{"plugins": {"bud@bud-plugins": "installed"}, "marketplaces": {"bud-plugins": "x"}}`), 0o644)
	k := &Kit{
		Marketplaces: map[string]string{"bud-plugins": "x"},
		Plugins:      []string{"bud@bud-plugins", "bud-ide@bud-plugins"},
	}

	actions, err := Sync(k, env, true)
	if err != nil || slices.Contains(f.ran, "plugin uninstall --scope user bud@bud-plugins") {
		t.Fatalf("dry run ran %q (%v)", f.ran, err)
	}
	if !slices.Contains(actions, Action{"plugin", "bud@bud-plugins", "remove", "claude.ai syncs it"}) {
		t.Fatalf("dry run actions %v", actions)
	}

	f.ran = nil
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.ran, "plugin uninstall --scope user bud@bud-plugins") || !slices.Contains(f.installed, "bud@synced") {
		t.Fatalf("ran %q, installed %v", f.ran, f.installed)
	}
	if l, _, _ := ReadLedger(env.Ledger); l.Plugins["bud@bud-plugins"] != "" {
		t.Fatalf("ledger kept bud's copy: %v", l.Plugins)
	}

	// from then on it's skipped, and only bud-ide is bud's
	f.ran = nil
	actions, _ = Sync(k, env, false)
	if !slices.Contains(actions, Action{"plugin", "bud@bud-plugins", "skip", "claude.ai syncs it"}) || slices.ContainsFunc(f.ran, func(r string) bool { return strings.Contains(r, "bud@") }) {
		t.Fatalf("actions %v, ran %q", actions, f.ran)
	}
}

// JGA-183: a project or local copy loads in one project only, so it neither
// keeps bud from installing the kit's plugin nor is bud's to uninstall
func TestSyncCountsOnlyCopiesThatLoadInEveryProject(t *testing.T) {
	dir := t.TempDir()
	installed := []Plugin{
		{ID: "bud@bud-plugins", Scope: "local", Project: "/w"},
		{ID: "corp@x", Scope: "managed"},
		{ID: "old@bud-plugins", Scope: "project", Project: "/p"},
		{ID: "gone@bud-plugins", Scope: "managed"},
	}
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		Claude: func(a ...string) error {
			ran = append(ran, strings.Join(a, " "))
			return nil
		},
		InstalledPlugins: func() ([]Plugin, error) { return installed, nil },
	}
	// old is bud's but dropped from the kit, gone is bud's with only a policy copy left
	os.WriteFile(env.Ledger, []byte(`{"plugins": {"old@bud-plugins": "installed", "gone@bud-plugins": "installed"}}`), 0o644)
	k := &Kit{Plugins: []string{"bud@bud-plugins", "corp@x", "gone@bud-plugins"}}

	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, []string{"plugin install --scope user bud@bud-plugins"}) {
		t.Fatalf("ran %q (%v)", ran, actions)
	}
	for _, want := range []Action{
		{"plugin", "bud@bud-plugins", "add", "only in some projects so far: bud@bud-plugins (local, /w)"},
		{"plugin", "corp@x", "skip", "your organisation's policy installs it"},
		{"plugin", "gone@bud-plugins", "skip", "your organisation's policy installs it"},
		{"plugin", "old@bud-plugins", "skip", "no longer in the kit, and the copy left isn't bud's: old@bud-plugins (project, /p) (yours)"},
	} {
		if !slices.Contains(actions, want) {
			t.Errorf("no %v in %v", want, actions)
		}
	}
	l, _, _ := ReadLedger(env.Ledger)
	if _, ok := l.Plugins["old@bud-plugins"]; ok {
		t.Errorf("ledger kept old, whose only copy is a project's: %v", l.Plugins)
	}
	if _, ok := l.Plugins["gone@bud-plugins"]; ok {
		t.Errorf("ledger kept gone, whose only copy is the policy's: %v", l.Plugins)
	}
	if l.Plugins["bud@bud-plugins"] != "installed" {
		t.Errorf("ledger lacks bud: %v", l.Plugins)
	}
}

func TestSyncKeepsTheLedgerWhenPluginsCantBeListed(t *testing.T) {
	dir := t.TempDir()
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		Claude: func(a ...string) error {
			ran = append(ran, strings.Join(a, " "))
			return nil
		},
		InstalledPlugins: func() ([]Plugin, error) { return nil, errors.New("claude timed out") },
	}
	os.WriteFile(env.Ledger, []byte(`{"plugins": {"old@bud-plugins": "installed"}}`), 0o644)
	// nothing installed either: an install could put a second copy beside one it can't see
	actions, err := Sync(&Kit{Plugins: []string{"bud@bud-plugins"}}, env, false)
	if err == nil || !strings.Contains(err.Error(), "claude timed out") {
		t.Fatalf("err %v", err)
	}
	for _, id := range []string{"bud@bud-plugins", "old@bud-plugins"} {
		if !slices.Contains(actions, Action{"plugin", id, "skip", "couldn't list the installed plugins"}) {
			t.Errorf("no skip for %s in %v", id, actions)
		}
	}
	if len(ran) > 0 {
		t.Fatalf("ran %q", ran)
	}
	if l, _, _ := ReadLedger(env.Ledger); l.Plugins["old@bud-plugins"] != "installed" {
		t.Fatalf("ledger forgot bud's copy: %v", l.Plugins)
	}
}

func TestADroppedPluginAlreadyGoneIsLetGoQuietly(t *testing.T) {
	dir := t.TempDir()
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		Claude: func(a ...string) error {
			ran = append(ran, strings.Join(a, " "))
			return nil
		},
		InstalledPlugins: func() ([]Plugin, error) { return []Plugin{{ID: "pol@m", Scope: "managed"}}, nil },
	}
	os.WriteFile(env.Ledger, []byte(`{"plugins": {"gone@m": "installed", "pol@m": "installed"}}`), 0o644)
	actions, _ := Sync(&Kit{}, env, true)
	for _, want := range []Action{
		{"plugin", "gone@m", "skip", "no longer in the kit, and already uninstalled"},
		{"plugin", "pol@m", "skip", "no longer in the kit, and the copy left isn't bud's: pol@m (managed) (your organisation's policy installs it)"},
	} {
		if !slices.Contains(actions, want) {
			t.Errorf("no %v in %v", want, actions)
		}
	}
	if len(ran) > 0 {
		t.Fatalf("a dry run ran %q", ran)
	}
}

func TestSyncSaysWhyAMachineWideCopyIsntBuds(t *testing.T) {
	plugins := []Plugin{{ID: "a@m", Scope: "synced"}, {ID: "b@m", Scope: "managed"}}
	if got := machineWhy(plugins, "a@m"); got != "claude.ai syncs it" {
		t.Errorf("synced: %q", got)
	}
	if got := machineWhy(plugins, "b@m"); got != "your organisation's policy installs it" {
		t.Errorf("managed: %q", got)
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

func TestASyncedPluginHiddenByYourOwnCopyNamesTheUninstall(t *testing.T) {
	dir := t.TempDir()
	// claude.ai syncs bud, but copies bud's ledger doesn't hold are installed too
	f := &fakeMarketplaces{registered: []string{"bud-plugins"}, installed: []string{"bud@synced", "bud@bud-plugins", "bud@clod", "bud-ide@synced"}}
	env := f.env(dir)
	os.WriteFile(env.Ledger, []byte(`{"marketplaces": {"bud-plugins": "x"}}`), 0o644)
	k := &Kit{
		Marketplaces: map[string]string{"bud-plugins": "x"},
		Plugins:      []string{"bud@bud-plugins", "bud-ide@bud-plugins"},
	}

	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	// every copy that hides it, each with its command
	want := Action{"plugin", "bud@bud-plugins", "skip", "claude.ai syncs it, but bud@bud-plugins, bud@clod load instead: " +
		"claude plugin uninstall --scope user bud@bud-plugins; claude plugin uninstall --scope user bud@clod"}
	if !slices.Contains(actions, want) {
		t.Fatalf("actions %v, want %v", actions, want)
	}
	if !slices.Contains(actions, Action{"plugin", "bud-ide@bud-plugins", "skip", "claude.ai syncs it"}) {
		t.Fatalf("bud-ide has no other copy: %v", actions)
	}
	// they aren't bud's, so bud doesn't take them out
	if slices.ContainsFunc(f.ran, func(r string) bool { return strings.Contains(r, "uninstall") }) {
		t.Fatalf("uninstalled a copy bud didn't install: %q", f.ran)
	}
}

func TestRemovingBudsCopyStillNamesYoursThatHidesTheSyncedOne(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMarketplaces{registered: []string{"bud-plugins"}, installed: []string{"bud@synced", "bud@bud-plugins", "bud@clod"}}
	env := f.env(dir)
	os.WriteFile(env.Ledger, []byte(`{"plugins": {"bud@bud-plugins": "installed"}, "marketplaces": {"bud-plugins": "x"}}`), 0o644)
	k := &Kit{Marketplaces: map[string]string{"bud-plugins": "x"}, Plugins: []string{"bud@bud-plugins"}}

	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	want := Action{"plugin", "bud@bud-plugins", "remove", "claude.ai syncs it, but bud@clod loads instead: claude plugin uninstall --scope user bud@clod"}
	if !slices.Contains(actions, want) || !slices.Contains(f.ran, "plugin uninstall --scope user bud@bud-plugins") {
		t.Fatalf("actions %v, ran %q", actions, f.ran)
	}
}

func TestShadowed(t *testing.T) {
	k := &Kit{Plugins: []string{"bud@bud-plugins", "bud-ide@bud-plugins", "superpowers@bud-plugins"}}
	// bud's own copy from a marketplace the kit left: sync takes it out, so it isn't yours to
	ledger := &Ledger{Plugins: map[string]string{"bud@old-market": "installed"}}
	cases := []struct {
		installed []string
		want      []string
	}{
		{[]string{"bud@synced", "bud-ide@synced"}, nil},
		{[]string{"bud@synced", "bud@bud-plugins"}, []string{"bud@bud-plugins"}},
		{[]string{"superpowers@synced", "superpowers@claude-plugins-official"}, []string{"superpowers@claude-plugins-official"}},
		{[]string{"bud@synced", "bud@old-market"}, nil},
		// not synced: the installed copy is the only one, nothing hidden
		{[]string{"bud@bud-plugins"}, nil},
	}
	for _, c := range cases {
		if got := IDs(Shadowed(k, asPlugins(c.installed), ledger)); !slices.Equal(got, c.want) {
			t.Errorf("Shadowed(%v) = %v, want %v", c.installed, got, c.want)
		}
	}
}

func TestUninstallHintUsesThePluginsScope(t *testing.T) {
	cases := map[Plugin]string{
		{ID: "bud@clod"}:                                         "claude plugin uninstall --scope user bud@clod",
		{ID: "bud@clod", Scope: "user"}:                          "claude plugin uninstall --scope user bud@clod",
		{ID: "bud@clod", Scope: "local", Project: "/home/j/app"}: "cd /home/j/app && claude plugin uninstall --scope local bud@clod",
		{ID: "bud@clod", Scope: "project", Project: "/x/my app"}: "cd '/x/my app' && claude plugin uninstall --scope project bud@clod",
		{ID: "bud@corp", Scope: "managed"}:                       "bud@corp is installed by your organisation's policy, so an administrator has to remove it",
	}
	for p, want := range cases {
		if got := UninstallHint(p); got != want {
			t.Errorf("UninstallHint(%+v) = %q, want %q", p, got, want)
		}
	}
}

func TestOwnedIsTheLedgersCopyAtUserScope(t *testing.T) {
	ledger := &Ledger{Plugins: map[string]string{"bud@bud-plugins": "installed"}}
	cases := map[Plugin]bool{
		{ID: "bud@bud-plugins", Scope: "user"}:                 true,
		{ID: "bud@bud-plugins"}:                                true,
		{ID: "bud@bud-plugins", Scope: "local", Project: "/w"}: false,
		{ID: "bud@clod", Scope: "user"}:                        false,
	}
	for p, want := range cases {
		if got := Owned(p, ledger); got != want {
			t.Errorf("Owned(%+v) = %v, want %v", p, got, want)
		}
	}
	if Owned(Plugin{ID: "bud@bud-plugins"}, nil) {
		t.Error("a nil ledger owns nothing")
	}
}

func TestShadowNoteTellsCopiesOfOneIDApart(t *testing.T) {
	got := shadowNote([]Plugin{{ID: "bud@clod", Scope: "user"}, {ID: "bud@clod", Scope: "local", Project: "/w"}})
	want := ", but bud@clod, bud@clod (local, /w) load instead: claude plugin uninstall --scope user bud@clod; cd /w && claude plugin uninstall --scope local bud@clod"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestParsePlugins(t *testing.T) {
	out := `[{"id": "bud@synced", "scope": "synced"}, {"id": "headroom@hm", "scope": "local", "projectPath": "/Users/j", "enabled": true}]`
	got, err := ParsePlugins([]byte(out))
	want := []Plugin{{ID: "bud@synced", Scope: "synced"}, {ID: "headroom@hm", Scope: "local", Project: "/Users/j"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %v (%v), want %v", got, err, want)
	}
	if _, err := ParsePlugins([]byte("nope")); err == nil {
		t.Fatal("bad JSON read")
	}
}

// asPlugins is ids as claude plugin list shows them: name@synced ones in scope
// synced, the rest at user scope
func asPlugins(ids []string) []Plugin {
	out := make([]Plugin, len(ids))
	for i, id := range ids {
		out[i] = Plugin{ID: id, Scope: "user"}
		if strings.HasSuffix(id, "@synced") {
			out[i].Scope = "synced"
		}
	}
	return out
}
