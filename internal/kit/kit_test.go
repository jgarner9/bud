package kit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud"
)

func TestShippedKit(t *testing.T) {
	k, err := Load(bud.Kit())
	if err != nil {
		t.Fatal(err)
	}
	if len(k.MCP) == 0 || k.MCP[0].Name != "context7" {
		t.Fatalf("mcp %+v", k.MCP)
	}
	server, missing, err := k.MCP[0].Server(func(string) bool { return false })
	if err != nil || len(missing) > 0 {
		t.Fatalf("context7 without a key: %s %v %v", server, missing, err)
	}
	if strings.Contains(string(server), "headers") {
		t.Errorf("an optional secret that isn't set should drop its header: %s", server)
	}
	server, _, _ = k.MCP[0].Server(func(string) bool { return true })
	if !strings.Contains(string(server), `"CONTEXT7_API_KEY":"${CONTEXT7_API_KEY}"`) {
		t.Errorf("secret reference: %s", server)
	}

	// Linear comes from the kit only where the claude.ai connector doesn't
	// give it (Bedrock, JGA-148), as claude mcp list shows that connector
	i := slices.IndexFunc(k.MCP, func(m MCP) bool { return m.Name == "linear" })
	if i < 0 {
		t.Fatal("no linear entry")
	}
	connectors := ParseConnectors("claude.ai Linear: https://mcp.linear.app/mcp - ✔ Connected\n")
	if _, ok := k.MCP[i].ProvidedBy(connectors); !ok {
		t.Errorf("the Linear connector doesn't stand in for the kit's linear: %+v", connectors)
	}
	if _, ok := k.MCP[i].ProvidedBy(nil); ok {
		t.Error("provided without a connector")
	}
}

func TestServerRequiredSecret(t *testing.T) {
	m := MCP{Name: "gh", Transport: "stdio", Command: "gh-mcp", Env: map[string]string{"TOKEN": "$secret:GH_TOKEN", "MODE": "x"}}
	if _, missing, _ := m.Server(func(string) bool { return false }); !slices.Equal(missing, []string{"GH_TOKEN"}) {
		t.Fatalf("missing %v", missing)
	}
	server, _, _ := m.Server(func(string) bool { return true })
	var got map[string]any
	json.Unmarshal(server, &got)
	if got["command"] != "gh-mcp" || got["env"].(map[string]any)["TOKEN"] != "${GH_TOKEN}" {
		t.Fatalf("server %s", server)
	}
}

func TestWithBlock(t *testing.T) {
	doc := withBlock("# mine\n", "be a bud")
	if doc != "# mine\n\n"+blockStart+"\nbe a bud\n"+blockEnd+"\n" {
		t.Fatalf("added: %q", doc)
	}
	if again := withBlock(doc+"\nmore of mine\n", "be a good bud"); !strings.Contains(again, "be a good bud") || strings.Contains(again, "be a bud\n") || !strings.HasSuffix(again, "more of mine\n") {
		t.Fatalf("replaced: %q", again)
	}
	if gone := withBlock(doc, ""); gone != "# mine\n" {
		t.Fatalf("removed: %q", gone)
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

func TestSync(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	fc := &fakeClaude{servers: map[string]string{"mine": "{}"}, json: filepath.Join(dir, ".claude.json")}
	fc.run("mcp", "add-json", "--scope", "user", "mine", "{}")
	fc.ran = nil
	env := Env{
		ClaudeDir: claudeDir, ClaudeJSON: fc.json, Ledger: filepath.Join(dir, "ledger.json"), Profile: "full",
		Claude: fc.run, SecretSet: func(string) bool { return false },
	}
	fsys := fstest.MapFS{
		"kit.json":                {Data: []byte(`{"mcp": ["docs", "needs-key", "mine"], "skills": ["bud-dev"], "commands": ["commit"]}`)},
		"mcp/docs.json":           {Data: []byte(`{"transport": "http", "url": "https://docs.example/mcp"}`)},
		"mcp/needs-key.json":      {Data: []byte(`{"transport": "http", "url": "https://x", "headers": {"K": "$secret:X_KEY"}}`)},
		"mcp/mine.json":           {Data: []byte(`{"transport": "http", "url": "https://mine"}`)},
		"skills/bud-dev/SKILL.md": {Data: []byte("skill")},
		"commands/commit.md":      {Data: []byte("commit style")},
		"CLAUDE.md":               {Data: []byte("be a bud\n")},
	}
	k, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}

	// a dry run changes nothing
	actions, err := Sync(k, env, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.ran) != 0 || exists(filepath.Join(claudeDir, "commands", "commit.md")) {
		t.Fatalf("dry run did something: %v", fc.ran)
	}
	got := map[string]string{}
	for _, a := range actions {
		got[a.Name] = a.Do + " " + a.Why
	}
	for name, want := range map[string]string{
		"docs": "add ", "needs-key": "skip set X_KEY", "mine": "skip you have your own mine",
		"skills/bud-dev/SKILL.md": "add ", "commands/commit.md": "add ", "CLAUDE.md": "add ",
	} {
		if got[name] != want {
			t.Errorf("%s: %q, want %q", name, got[name], want)
		}
	}

	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fc.ran, []string{"mcp add-json docs"}) {
		t.Fatalf("claude ran %q", fc.ran)
	}
	if data, _ := os.ReadFile(filepath.Join(claudeDir, "skills", "bud-dev", "SKILL.md")); string(data) != "skill" {
		t.Fatalf("skill %q", data)
	}
	if md, _ := os.ReadFile(filepath.Join(claudeDir, "CLAUDE.md")); !strings.Contains(string(md), "be a bud") {
		t.Fatalf("instructions %q", md)
	}

	// again: nothing to do
	fc.ran = nil
	actions, _ = Sync(k, env, false)
	for _, a := range actions {
		if a.Do != "ok" && a.Do != "skip" {
			t.Errorf("second sync: %v", a)
		}
	}

	// the kit drops docs and the command: they're removed; yours stays
	k.MCP = k.MCP[1:]
	k.Commands = nil
	k.Instructions = ""
	fc.ran = nil
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fc.ran, []string{"mcp remove docs"}) {
		t.Fatalf("claude ran %q", fc.ran)
	}
	if _, ok := fc.servers["mine"]; !ok {
		t.Fatal("removed your own server")
	}
	if exists(filepath.Join(claudeDir, "commands")) {
		t.Fatal("dropped command left behind")
	}
	if md, _ := os.ReadFile(filepath.Join(claudeDir, "CLAUDE.md")); strings.Contains(string(md), "bud:begin") {
		t.Fatalf("instructions block left: %q", md)
	}

	// a skill file you edited isn't overwritten
	os.WriteFile(filepath.Join(claudeDir, "skills", "bud-dev", "SKILL.md"), []byte("my edit"), 0o644)
	k.Skills = []string{"bud-dev"}
	fsys["skills/bud-dev/SKILL.md"] = &fstest.MapFile{Data: []byte("skill v2")}
	actions, _ = Sync(k, env, false)
	if data, _ := os.ReadFile(filepath.Join(claudeDir, "skills", "bud-dev", "SKILL.md")); string(data) != "my edit" {
		t.Fatalf("overwrote your edit: %q (%v)", data, actions)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestSecretInsideAValue(t *testing.T) {
	m := MCP{Transport: "http", URL: "https://gh", Headers: map[string]string{"Authorization": "Bearer $secret:GITHUB_PAT"}}
	if _, missing, _ := m.Server(func(string) bool { return false }); !slices.Equal(missing, []string{"GITHUB_PAT"}) {
		t.Fatalf("missing %v", missing)
	}
	server, _, _ := m.Server(func(string) bool { return true })
	if !strings.Contains(string(server), `"Authorization":"Bearer ${GITHUB_PAT}"`) {
		t.Fatalf("server %s", server)
	}
}

func TestSyncSkipsMissingBinary(t *testing.T) {
	dir := t.TempDir()
	k := &Kit{MCP: []MCP{{Name: "serena", Transport: "stdio", Command: "serena", Requires: &Requires{Bin: "serena", Install: "uv tool install serena-agent"}}}}
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		Claude:    func(a ...string) error { ran = append(ran, strings.Join(a, " ")); return nil },
		SecretSet: func(string) bool { return false },
		HasBin:    func(string) bool { return false },
	}
	actions, err := Sync(k, env, false)
	if err != nil || len(ran) != 0 || actions[0].Do != "skip" || !strings.Contains(actions[0].Why, "uv tool install") {
		t.Fatalf("actions %v, ran %q, %v", actions, ran, err)
	}
	env.HasBin = func(string) bool { return true }
	if _, err := Sync(k, env, false); err != nil || len(ran) != 1 {
		t.Fatalf("with the binary: ran %q, %v", ran, err)
	}
}

func TestSyncPlugins(t *testing.T) {
	dir := t.TempDir()
	installed := []string{"mine@somewhere"}
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		SecretSet: func(string) bool { return false },
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

func TestLargeOnly(t *testing.T) {
	big := MCP{Name: "code-index", Transport: "stdio", Command: "mcp-index", Args: []string{"stdio"}, When: &When{Large: true}, Requires: &Requires{Bin: "mcp-index"}}
	small := MCP{Name: "docs", Transport: "http", URL: "https://d"}
	k := &Kit{MCP: []MCP{big, small}}
	none := func(string) bool { return false }
	all := func(string) bool { return true }

	// never in the machine-wide sync
	dir := t.TempDir()
	var ran []string
	env := Env{ClaudeDir: dir, ClaudeJSON: filepath.Join(dir, "c.json"), Ledger: filepath.Join(dir, "l.json"), SecretSet: none, HasBin: all,
		Claude: func(a ...string) error { ran = append(ran, a[4]); return nil }}
	Sync(k, env, false)
	if !slices.Equal(ran, []string{"docs"}) {
		t.Fatalf("synced %v", ran)
	}

	// per session: only in a project past either threshold, with its binary
	for _, c := range []struct {
		files int
		loc   int64
		bin   func(string) bool
		want  bool
	}{
		{100, 5000, all, false},
		{10000, 5000, all, true},
		{100, 1000000, all, true},
		{20000, 0, none, false},
	} {
		cfg, err := k.SessionServers("full", Project{Files: c.files, LOC: c.loc}, 10000, 1000000, nil, none, c.bin)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(cfg), `"code-index"`); got != c.want {
			t.Errorf("%d files, %d loc: %s", c.files, c.loc, cfg)
		}
	}
	// its own thresholds win over the defaults; disabled is off
	k.MCP[0].When = &When{MinFiles: 50}
	if cfg, _ := k.SessionServers("full", Project{Files: 60}, 10000, 1000000, nil, none, all); cfg == nil {
		t.Error("min_files ignored")
	}
	if cfg, _ := k.SessionServers("full", Project{Files: 60}, 10000, 1000000, []string{"code-index"}, none, all); cfg != nil {
		t.Error("a disabled entry loaded")
	}
}

func TestParseConnectors(t *testing.T) {
	out := `  ║            HEADROOM WRAP: CLAUDE              ║
Checking MCP server health…

claude.ai Context7: https://mcp.context7.com/mcp - ✔ Connected
claude.ai Basic Memory: https://memory.example.ts.net/mcp - ✔ Connected
claude.ai Resend: https://mcp.resend.com - ! Needs authentication
headroom: /Users/x/.local/bin/headroom mcp serve - ✔ Connected
plugin:engineering:slack: https://mcp.slack.com/mcp (HTTP) - ! Needs authentication
`
	got := ParseConnectors(out)
	want := []Connector{
		{"Context7", "https://mcp.context7.com/mcp"},
		{"Basic Memory", "https://memory.example.ts.net/mcp"},
		{"Resend", "https://mcp.resend.com"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestProvidedBy(t *testing.T) {
	connectors := []Connector{{"Context7", "https://mcp.context7.com/mcp"}, {"Basic Memory", "https://memory.example.ts.net/mcp"}}
	for _, tc := range []struct {
		m    MCP
		want string
	}{
		{MCP{Name: "context7"}, "Context7"},                                    // by name, any case
		{MCP{Name: "docs", URL: "https://MCP.context7.com/other"}, "Context7"}, // by host
		{MCP{Name: "basic-memory", Connector: "Basic Memory"}, "Basic Memory"}, // named
		{MCP{Name: "github", URL: "https://api.githubcopilot.com/mcp/"}, ""},   // none
		{MCP{Name: "serena", Transport: "stdio", Command: "serena"}, ""},       // stdio, no host
	} {
		c, ok := tc.m.ProvidedBy(connectors)
		if (tc.want == "" && ok) || (tc.want != "" && c.Name != tc.want) {
			t.Errorf("%s: got %+v %v, want %q", tc.m.Name, c, ok, tc.want)
		}
	}
}

func TestSyncSkipsConnectors(t *testing.T) {
	dir := t.TempDir()
	var ran []string
	var connectors []Connector
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"), Ledger: filepath.Join(dir, "l.json"),
		SecretSet: func(string) bool { return false },
		Claude: func(a ...string) error {
			ran = append(ran, strings.Join(a[:2], " ")+" "+a[len(a)-1])
			return nil
		},
		Connectors: func() ([]Connector, error) { return connectors, nil },
	}
	k := &Kit{MCP: []MCP{{Name: "context7", Transport: "http", URL: "https://mcp.context7.com/mcp"}}}

	// no connector: the kit adds its own
	if _, err := Sync(k, env, false); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || !strings.HasPrefix(ran[0], "mcp add-json") {
		t.Fatalf("first sync ran %q", ran)
	}
	os.WriteFile(env.ClaudeJSON, []byte(`{"mcpServers":{"context7":{"type":"http","url":"https://mcp.context7.com/mcp"}}}`), 0o644)

	// the connector turns up: the kit's copy goes, saying why
	connectors = []Connector{{"Context7", "https://mcp.context7.com/mcp"}}
	ran = nil
	actions, err := Sync(k, env, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, []string{"mcp remove context7"}) {
		t.Fatalf("ran %q", ran)
	}
	if len(actions) != 1 || actions[0].Do != "remove" || !strings.Contains(actions[0].Why, "claude.ai Context7 connector") {
		t.Fatalf("actions %+v", actions)
	}

	// from then on it's a skip, and nothing runs
	os.WriteFile(env.ClaudeJSON, []byte(`{}`), 0o644)
	ran = nil
	actions, _ = Sync(k, env, true)
	if len(ran) != 0 || len(actions) != 1 || actions[0].Do != "skip" || actions[0].Why != "provided by the claude.ai Context7 connector" {
		t.Fatalf("ran %q, actions %+v", ran, actions)
	}

	// a failed listing is the same as none
	env.Connectors = func() ([]Connector, error) { return nil, errors.New("no claude") }
	actions, _ = Sync(k, env, true)
	if actions[0].Do != "add" {
		t.Fatalf("without the listing: %+v", actions)
	}
}

func TestServerURLSecret(t *testing.T) {
	m := MCP{Name: "basic-memory", Transport: "http", URL: "$secret:BASIC_MEMORY_URL", OptionalSecrets: true}
	if _, missing, _ := m.Server(func(string) bool { return false }); !slices.Equal(missing, []string{"BASIC_MEMORY_URL"}) {
		t.Fatalf("missing URL secret: %v", missing)
	}
	server, missing, err := m.Server(func(string) bool { return true })
	if err != nil || len(missing) > 0 || !strings.Contains(string(server), `"url":"${BASIC_MEMORY_URL}"`) {
		t.Fatalf("got %s %v %v", server, missing, err)
	}
	// a secret URL has no host to match a connector by
	if _, ok := m.ProvidedBy([]Connector{{"Other", "https://x.y/mcp"}, {"Odd", ""}}); ok {
		t.Fatal("matched a connector by an empty host")
	}
}

func TestWhenFits(t *testing.T) {
	web, plain, configured := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"devDependencies": {"vite": "^6"}}`), 0o644)
	os.WriteFile(filepath.Join(plain, "package.json"), []byte(`{"dependencies": {"lodash": "^4"}}`), 0o644)
	os.WriteFile(filepath.Join(configured, "playwright.config.ts"), nil, 0o644)
	w := When{Files: []string{"playwright.config.ts"}, Packages: []string{"react", "vite"}}
	for _, c := range []struct {
		root string
		want bool
	}{{web, true}, {configured, true}, {plain, false}, {t.TempDir(), false}} {
		if got := w.Fits(Project{Root: c.root}, 0, 0); got != c.want {
			t.Errorf("%s: got %v", c.root, got)
		}
	}
	// size and use both have to hold
	big := When{Large: true, Packages: []string{"vite"}}
	if big.Fits(Project{Root: web, Files: 5}, 100, 0) || !big.Fits(Project{Root: web, Files: 500}, 100, 0) {
		t.Error("large and web")
	}
	// no conditions: every project
	if !(When{}).Fits(Project{Root: plain}, 0, 0) {
		t.Error("empty when")
	}
	if !strings.Contains(w.Describe(), "playwright.config.ts, react, vite") {
		t.Errorf("describe %q", w.Describe())
	}
}
