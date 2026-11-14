package plugin

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/paths"
)

func fake(body string) fs.FS {
	return fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name": "bud-ide"}`)},
		"agents/chat.md":             {Data: []byte(body)},
	}
}

func TestExtractIsIdempotentAndComplete(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	first, err := extract(fake("one"), root)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(first, "agents", "chat.md")); string(got) != "one" {
		t.Fatalf("chat.md = %q", got)
	}
	if _, err := os.Stat(filepath.Join(first, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal(err)
	}
	// an unchanged plugin is the same dir, left alone
	mark := filepath.Join(first, "mark")
	os.WriteFile(mark, nil, 0o644)
	again, err := extract(fake("one"), root)
	if err != nil || again != first {
		t.Fatalf("second extract = %q, %v; want %q", again, err, first)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatal("an unchanged plugin was written again")
	}
}

func TestChangedPluginReplacesTheOldVersion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	old, _ := extract(fake("one"), root)
	cur, err := extract(fake("two"), root)
	if err != nil || cur == old {
		t.Fatalf("new version = %q, %v; old %q", cur, err, old)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old version kept")
	}
	// nothing else is left: no temp dirs
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("root holds %d entries, want 1", len(entries))
	}
}

func TestHalfWrittenDirIsRepaired(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	dir, _ := extract(fake("one"), root)
	os.Remove(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	got, err := extract(fake("one"), root)
	if err != nil || got != dir {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("not repaired")
	}
}

func TestDirUsesThePathsPluginsDir(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := Dir(p)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != p.Plugins() || !strings.HasPrefix(p.Plugins(), data) {
		t.Fatalf("dir %q, plugins %q", dir, p.Plugins())
	}
	args, err := Args()
	if err != nil || len(args) != 2 || args[0] != "--plugin-dir" || args[1] != dir {
		t.Fatalf("Args = %q, %v", args, err)
	}
}

// the embedded plugin is what claude --agent bud-ide:chat and bud-ide:<skill> resolve against
func TestEmbeddedPlugin(t *testing.T) {
	src := source()
	data, err := fs.ReadFile(src, ".claude-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Name string }
	if err := json.Unmarshal(data, &m); err != nil || m.Name != Name {
		t.Fatalf("plugin.json name = %q (%v), want %q", m.Name, err, Name)
	}
	if _, err := fs.Stat(src, "agents/chat.md"); err != nil {
		t.Fatal(err)
	}
	skills, _ := fs.Glob(src, "skills/*/SKILL.md")
	if len(skills) < 2 {
		t.Fatalf("skills = %v", skills)
	}
	for _, f := range skills {
		b, _ := fs.ReadFile(src, f)
		head, ok := strings.CutPrefix(string(b), "---\n")
		front, _, closed := strings.Cut(head, "\n---\n")
		dir := filepath.Base(filepath.Dir(f))
		if !ok || !closed || !strings.Contains(front, "name: "+dir+"\n") || !strings.Contains(front, "description: ") {
			t.Errorf("%s: front matter needs name %q and a description", f, dir)
		}
	}
}
