package kit

import (
	"encoding/json"
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
