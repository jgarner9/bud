package plugin

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

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
	// a version in use recently stays: a running session or another bud may read it
	if _, err := os.Stat(old); err != nil {
		t.Fatal("recently used old version removed")
	}
	// one idle for more than staleAfter goes the next time a dir is handed out
	long := time.Now().Add(-staleAfter - time.Hour)
	os.Chtimes(old, long, long)
	if again, _ := extract(fake("two"), root); again != cur {
		t.Fatalf("second extract = %q", again)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale version kept")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("root holds %d entries, want 1 (no temp dirs)", len(entries))
	}
}

func TestIncompleteDirIsRebuiltByFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	dir, _ := extract(fake("one"), root)
	// plugin.json is there but another file is missing, or has the wrong size
	os.WriteFile(filepath.Join(dir, "agents", "chat.md"), []byte("cut off"), 0o644)
	got, err := extract(fake("one"), root)
	if err != nil || got != dir {
		t.Fatal(got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "agents", "chat.md")); string(b) != "one" {
		t.Fatalf("chat.md = %q", b)
	}
	os.Remove(filepath.Join(dir, "agents", "chat.md"))
	if _, err := extract(fake("one"), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "chat.md")); err != nil {
		t.Fatal("missing file not restored")
	}
}

func TestConcurrentExtractsAllGetACompleteDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	var wg sync.WaitGroup
	dirs := make([]string, 8)
	for i := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := extract(fake("one"), root)
			if err != nil {
				t.Error(err)
			}
			dirs[i] = d
		}()
	}
	wg.Wait()
	for _, d := range dirs {
		if d != dirs[0] || !complete(fake("one"), d) {
			t.Fatalf("dirs = %v", dirs)
		}
	}
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
