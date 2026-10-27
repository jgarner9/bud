package kit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func repoKit(t *testing.T) Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kit")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "kit.json"), []byte(`{"mcp": [], "skills": [], "agents": [], "commands": []}`), 0o644)
	return Repo{Dir: dir}
}

func TestAddScaffoldRemove(t *testing.T) {
	r := repoKit(t)
	if _, err := r.AddMCP("docs", "https://docs.example/mcp", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddMCP("local", "", "npx -y some-mcp --flag"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddMCP("docs", "https://x", ""); err == nil {
		t.Fatal("added docs twice")
	}
	if _, err := r.AddMCP("Bad Name", "https://x", ""); err == nil {
		t.Fatal("took a bad name")
	}
	for _, kind := range []string{"skill", "agent", "command"} {
		p, err := r.Scaffold(kind, "my-"+kind)
		if err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(p); !strings.Contains(string(data), "TODO") {
			t.Fatalf("%s scaffold %q", kind, data)
		}
	}
	k, err := Load(os.DirFS(r.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(k.MCP) != 2 || k.MCP[1].Command != "npx" || !slices.Equal(k.MCP[1].Args, []string{"-y", "some-mcp", "--flag"}) {
		t.Fatalf("mcp %+v", k.MCP)
	}
	if !slices.Equal(k.Skills, []string{"my-skill"}) || !slices.Equal(k.Commands, []string{"my-command"}) {
		t.Fatalf("lists %v %v", k.Skills, k.Commands)
	}

	if kind, err := r.Remove("my-skill"); err != nil || kind != "skill" {
		t.Fatalf("rm: %s %v", kind, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "my-skill")); err == nil {
		t.Fatal("skill files left")
	}
	if err := r.SetProfiles("docs", []string{"full"}); err != nil {
		t.Fatal(err)
	}
	k, _ = Load(os.DirFS(r.Dir))
	if k.MCP[0].For("minimal") || !k.MCP[0].For("full") {
		t.Fatalf("profiles %v", k.MCP[0].Profiles)
	}
}

func TestAdopt(t *testing.T) {
	r := repoKit(t)
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	claudeJSON := filepath.Join(home, ".claude.json")
	os.WriteFile(claudeJSON, []byte(`{"mcpServers": {"gh": {"type": "http", "url": "https://gh", "headers": {"Authorization": "Bearer real-token"}}}}`), 0o644)
	os.MkdirAll(filepath.Join(claudeDir, "skills", "notes", "ref"), 0o755)
	os.WriteFile(filepath.Join(claudeDir, "skills", "notes", "SKILL.md"), []byte("notes skill"), 0o644)
	os.WriteFile(filepath.Join(claudeDir, "skills", "notes", "ref", "a.md"), []byte("ref"), 0o644)

	kind, secrets, err := r.Adopt("gh", claudeDir, claudeJSON)
	if err != nil || kind != "mcp" || !slices.Equal(secrets, []string{"GH_AUTHORIZATION"}) {
		t.Fatalf("adopt gh: %s %v %v", kind, secrets, err)
	}
	data, _ := os.ReadFile(filepath.Join(r.Dir, "mcp", "gh.json"))
	if strings.Contains(string(data), "real-token") {
		t.Fatalf("a secret landed in the repo: %s", data)
	}
	var m MCP
	json.Unmarshal(data, &m)
	if m.Headers["Authorization"] != "$secret:GH_AUTHORIZATION" || m.URL != "https://gh" {
		t.Fatalf("adopted %+v", m)
	}

	if kind, _, err := r.Adopt("notes", claudeDir, claudeJSON); err != nil || kind != "skill" {
		t.Fatalf("adopt notes: %s %v", kind, err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "skills", "notes", "ref", "a.md")); string(data) != "ref" {
		t.Fatalf("skill files: %q", data)
	}
	if _, _, err := r.Adopt("nothing", claudeDir, claudeJSON); err == nil {
		t.Fatal("adopted nothing")
	}

	// bud owns them now: the skill is in sync as it is, the server gets rewritten
	ledger := filepath.Join(home, "ledger.json")
	Own(ledger, claudeDir, "skill", "notes")
	Own(ledger, claudeDir, "mcp", "gh")
	l, _, _ := ReadLedger(ledger)
	if _, ok := l.MCP["gh"]; !ok || len(l.Files) != 2 {
		t.Fatalf("ledger %+v", l)
	}
}

func TestSyncSkipsDisabled(t *testing.T) {
	r := repoKit(t)
	r.AddMCP("docs", "https://d", "")
	k, _ := Load(os.DirFS(r.Dir))
	dir := t.TempDir()
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"),
		Ledger: filepath.Join(dir, "l.json"), Profile: "full", Disabled: []string{"docs"},
		Claude: func(a ...string) error { ran = append(ran, strings.Join(a, " ")); return nil }, SecretSet: func(string) bool { return false },
	}
	if _, err := Sync(k, env, false); err != nil || len(ran) != 0 {
		t.Fatalf("synced a disabled entry: %q %v", ran, err)
	}
}
