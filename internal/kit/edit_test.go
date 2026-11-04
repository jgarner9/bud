package kit

import (
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
	os.WriteFile(filepath.Join(dir, "kit.json"), []byte(`{"skills": [], "agents": [], "commands": []}`), 0o644)
	return Repo{Dir: dir}
}

func TestAddScaffoldRemove(t *testing.T) {
	r := repoKit(t)
	if _, err := r.Scaffold("skill", "Bad Name"); err == nil {
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
	if _, err := r.Scaffold("skill", "my-skill"); err == nil {
		t.Fatal("added my-skill twice")
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
}

func TestAdopt(t *testing.T) {
	r := repoKit(t)
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(filepath.Join(claudeDir, "skills", "notes", "ref"), 0o755)
	os.WriteFile(filepath.Join(claudeDir, "skills", "notes", "SKILL.md"), []byte("notes skill"), 0o644)
	os.WriteFile(filepath.Join(claudeDir, "skills", "notes", "ref", "a.md"), []byte("ref"), 0o644)

	if kind, err := r.Adopt("notes", claudeDir); err != nil || kind != "skill" {
		t.Fatalf("adopt notes: %s %v", kind, err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "skills", "notes", "ref", "a.md")); string(data) != "ref" {
		t.Fatalf("skill files: %q", data)
	}
	if _, err := r.Adopt("nothing", claudeDir); err == nil {
		t.Fatal("adopted nothing")
	}

	// bud owns it now, in sync as it is
	ledger := filepath.Join(home, "ledger.json")
	Own(ledger, claudeDir, "skill", "notes")
	l, _, _ := ReadLedger(ledger)
	if len(l.Files) != 2 {
		t.Fatalf("ledger %+v", l)
	}
}

func TestSyncSkipsDisabled(t *testing.T) {
	r := repoKit(t)
	r.AddPlugin("docs@market")
	k, _ := Load(os.DirFS(r.Dir))
	dir := t.TempDir()
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"),
		Ledger: filepath.Join(dir, "l.json"), Disabled: []string{"docs@market"},
		Claude: func(a ...string) error { ran = append(ran, strings.Join(a, " ")); return nil },
	}
	if _, err := Sync(k, env, false); err != nil || len(ran) != 0 {
		t.Fatalf("synced a disabled entry: %q %v", ran, err)
	}
}

func TestScaffoldOutputStyle(t *testing.T) {
	r := repoKit(t)
	p, err := r.Scaffold("output-style", "terse")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(r.Dir, "output-styles", "terse.md") {
		t.Errorf("path %s", p)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "name: terse") || !strings.Contains(string(data), "keep-coding-instructions: true") {
		t.Errorf("scaffold:\n%s", data)
	}
	idx, _ := r.index()
	if got := list(idx, "output-styles"); !slices.Equal(got, []string{"terse"}) {
		t.Errorf("kit.json output-styles %v", got)
	}
}
