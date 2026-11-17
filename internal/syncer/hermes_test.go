package syncer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/profile"
)

func TestSyncedSkillsAreLockedInHermes(t *testing.T) {
	origBin, origRun := hermesBin, runHermes
	t.Cleanup(func() { hermesBin, runHermes = origBin, origRun })
	hermesBin = func() (string, error) { return "/usr/bin/hermes", nil }
	var ran []string
	runHermes = func(bin string, args ...string) ([]byte, error) {
		ran = append(ran, bin+" "+strings.Join(args, " "))
		if args[2] == "broken" {
			return []byte("curator: no skill named 'broken'"), errors.New("exit status 1")
		}
		return nil, nil
	}
	if notes := lockSkills([]string{"a", "b"}, func(string, ...any) {}); notes != nil {
		t.Errorf("notes: %q", notes)
	}
	if want := []string{"/usr/bin/hermes curator lock a", "/usr/bin/hermes curator lock b"}; !slices.Equal(ran, want) {
		t.Errorf("ran %q, want %q", ran, want)
	}

	notes := lockSkills([]string{"broken"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "no skill named 'broken'") {
		t.Errorf("a failed lock is named: %q", notes)
	}

	hermesBin = func() (string, error) { return "", errors.New("not found") }
	notes = lockSkills([]string{"a"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "Hermes isn't on PATH") {
		t.Errorf("without Hermes: %q", notes)
	}
	if lockSkills(nil, nil) != nil {
		t.Error("no skills, no notes")
	}
}

func TestSkillFilesCopyASkillsRegularFilesForHermes(t *testing.T) {
	src := t.TempDir()
	write := func(rel string, perm os.FileMode) {
		path := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(rel), perm); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", 0o644)
	write("private.md", 0o600)
	write("scripts/run.sh", 0o755)
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	got, err := skillFiles(src, "/h/.hermes/skills/demo", "public")
	if err != nil {
		t.Fatal(err)
	}
	want := []want{
		{"/h/.hermes/skills/demo/SKILL.md", []byte("SKILL.md"), 0o644, "hermes-skill", "public"},
		{"/h/.hermes/skills/demo/private.md", []byte("private.md"), 0o600, "hermes-skill", "public"},
		{"/h/.hermes/skills/demo/scripts/run.sh", []byte("scripts/run.sh"), 0o755, "hermes-skill", "public"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d (the link isn't copied): %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.path != w.path || string(g.data) != string(w.data) || g.perm != w.perm || g.kind != w.kind || g.layer != w.layer {
			t.Errorf("file %d: got %+v, want %+v", i, g, w)
		}
	}

	if _, err := skillFiles(filepath.Join(src, "missing"), "/h/x", "public"); err == nil {
		t.Error("a missing skill dir is an error")
	}
}

// Suite D48: on a server, each synced skill is copied into Hermes's own
// skills dir under the ledger and locked there, and a skill the profile
// drops is removed from it
func TestAServerSyncCopiesItsSkillsIntoHermesAndLocksThem(t *testing.T) {
	origBin, origRun := hermesBin, runHermes
	t.Cleanup(func() { hermesBin, runHermes = origBin, origRun })
	hermesBin = func() (string, error) { return "/usr/bin/hermes", nil }
	var locked []string
	runHermes = func(bin string, args ...string) ([]byte, error) {
		locked = append(locked, args[2])
		return nil, nil
	}

	s := newServer(t)
	content := tree(t)
	content["env/server.jsonc"] = &fstest.MapFile{Data: repoFile(t, "server.jsonc")}
	s.publish("2026.11.17.1", "edge", content)
	o := options(t, s)
	o.Device = "home"
	examples := o.Profiles
	skills := `["learn", "plan"]`
	o.Profiles = func() []profile.Dir {
		return append(examples(), profile.Dir{Name: "home", FS: fstest.MapFS{
			"home.json": {Data: []byte(`{"name": "home", "components": ["server"], "channel": "edge", "content": {"skills": ` + skills + `, "env": ["base", "server"]}}`)},
		}})
	}
	run(t, o)

	dir := o.Paths.HermesSkills()
	for _, id := range []string{"learn", "plan"} {
		if got := read(t, filepath.Join(dir, id, "SKILL.md")); !strings.Contains(got, "name: "+id) {
			t.Errorf("Hermes's copy of %s = %q", id, got)
		}
	}
	if !slices.Equal(locked, []string{"learn", "plan"}) {
		t.Errorf("locked %q, want both skills", locked)
	}
	if cfg := read(t, o.Paths.HermesConfig()); !strings.Contains(cfg, "external_dirs: []") {
		t.Errorf("config.yaml should set no external skills dir:\n%s", cfg)
	}
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	tracked := false
	for _, f := range led.Files {
		if f.Path == filepath.Join(dir, "learn", "SKILL.md") {
			tracked = true
		}
	}
	if !tracked {
		t.Error("the ledger doesn't track Hermes's copy of learn")
	}

	skills = `["learn"]`
	locked = nil
	run(t, o)
	if _, err := os.Stat(filepath.Join(dir, "plan", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("a dropped skill's copy should be removed; stat: %v", err)
	}
	if !slices.Equal(locked, []string{"learn"}) {
		t.Errorf("locked %q, want learn", locked)
	}

	// adding it back finds no leftover dir in the way
	skills = `["learn", "plan"]`
	rep := run(t, o)
	if got := read(t, filepath.Join(dir, "plan", "SKILL.md")); !strings.Contains(got, "name: plan") {
		t.Errorf("plan wasn't copied back: %q (notes %q)", got, rep.Notes)
	}
}

// A skill bud didn't write, or one whose path goes through a link, gets no
// files and no lock, and the notes say why
func TestHermesSkillLeavesAloneWhatIsntBuds(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"SKILL.md", "scripts/run.sh"} {
		if err := os.WriteFile(filepath.Join(src, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	ws, note, err := hermesSkill(root, "demo", src, "public", nil)
	if err != nil || note != "" || len(ws) != 2 {
		t.Fatalf("a new skill: %d files, note %q, err %v", len(ws), note, err)
	}

	// an empty dir is what's left of a skill bud removed
	if err := os.MkdirAll(filepath.Join(root, "demo", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesSkill(root, "demo", src, "public", nil); note != "" || len(ws) != 2 {
		t.Errorf("an empty leftover dir: %d files, note %q", len(ws), note)
	}

	// Hermes's own skill of that name
	if err := os.WriteFile(filepath.Join(root, "demo", "SKILL.md"), []byte("Hermes's"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, note, err = hermesSkill(root, "demo", src, "public", nil)
	if err != nil || ws != nil || !strings.Contains(note, "didn't write") {
		t.Errorf("an untracked skill dir: %d files, note %q, err %v", len(ws), note, err)
	}
	tracked := []File{{Path: filepath.Join(root, "demo", "SKILL.md")}}
	if ws, note, _ = hermesSkill(root, "demo", src, "public", tracked); note != "" || len(ws) != 2 {
		t.Errorf("bud's own skill dir: %d files, note %q", len(ws), note)
	}

	// a link anywhere on the way, even in bud's own skill
	if err := os.Remove(filepath.Join(root, "demo", "scripts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "demo", "scripts")); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesSkill(root, "demo", src, "public", tracked); ws != nil || !strings.Contains(note, "is a link") {
		t.Errorf("a linked subdir: %d files, note %q", len(ws), note)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "other")); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesSkill(root, "other", src, "public", nil); ws != nil || !strings.Contains(note, "is a link") {
		t.Errorf("a linked skill dir: %d files, note %q", len(ws), note)
	}
}

func TestTheSignalPluginIsInstalledForHermesWhenItsDirIsBuds(t *testing.T) {
	plugin := fstest.MapFS{
		"plugin.yaml": {Data: []byte("name: bud-signal\n")},
		"__init__.py": {Data: []byte("def register(ctx): pass\n")},
	}
	root := filepath.Join(t.TempDir(), "plugins")
	dst := filepath.Join(root, HermesPluginName)

	ws, note, err := hermesPlugin(root, plugin, nil)
	if err != nil || note != "" || len(ws) != 2 {
		t.Fatalf("a new plugin: %d files, note %q, err %v", len(ws), note, err)
	}
	for _, w := range ws {
		if filepath.Dir(w.path) != dst || w.perm != 0o644 || w.kind != "hermes-plugin" || w.layer != "binary" {
			t.Errorf("file %+v", w)
		}
	}
	if got := pluginNames(note); !slices.Equal(got, []string{"bud-signal"}) {
		t.Errorf("enabled %q", got)
	}

	// a plugin dir bud didn't write is left alone, and not enabled
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "plugin.yaml"), []byte("Jason's"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, note, err = hermesPlugin(root, plugin, nil)
	if err != nil || ws != nil || !strings.Contains(note, "didn't write") || !strings.Contains(note, "/approve") {
		t.Errorf("an untracked plugin dir: %d files, note %q, err %v", len(ws), note, err)
	}
	if pluginNames(note) != nil {
		t.Error("a plugin bud couldn't install is enabled")
	}
	tracked := []File{{Path: filepath.Join(dst, "plugin.yaml"), SHA256: hash([]byte("Jason's"))}}
	if ws, note, _ = hermesPlugin(root, plugin, tracked); note != "" || len(ws) != 2 {
		t.Errorf("bud's own plugin dir: %d files, note %q", len(ws), note)
	}

	// a file of bud's edited by hand, or swapped for a link, holds the whole plugin back
	if err := os.WriteFile(filepath.Join(dst, "plugin.yaml"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesPlugin(root, plugin, tracked); ws != nil || !strings.Contains(note, "changed by hand") || pluginNames(note) != nil {
		t.Errorf("an edited plugin file: %d files, note %q", len(ws), note)
	}
	if err := os.Remove(filepath.Join(dst, "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "x"), filepath.Join(dst, "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesPlugin(root, plugin, tracked); ws != nil || !strings.Contains(note, "isn't a plain file") {
		t.Errorf("a linked plugin file: %d files, note %q", len(ws), note)
	}

	// through a link
	linked := filepath.Join(t.TempDir(), "plugins")
	if err := os.Symlink(t.TempDir(), linked); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesPlugin(linked, plugin, nil); ws != nil || !strings.Contains(note, "is a link") {
		t.Errorf("a linked plugins dir: %d files, note %q", len(ws), note)
	}
}

func TestTheShippedSignalPluginIsInTheBinary(t *testing.T) {
	for _, f := range []string{"plugin.yaml", "__init__.py", "adapter.py", "approvals.py"} {
		if _, err := fs.Stat(hermesPluginFS(), f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	err := fs.WalkDir(hermesPluginFS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.Contains(path, "__pycache__") {
			t.Errorf("%s is embedded", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAHeldDirsTrackedFilesAreNeitherPlannedNorDropped(t *testing.T) {
	files := []File{{Path: "/h/plugins/bud-signal/a.py"}, {Path: "/h/plugins/bud-signal-x/b.py"}, {Path: "/h/config.yaml"}}
	plan, kept := holdBack(files, []string{"/h/plugins/bud-signal"})
	if len(kept) != 1 || kept[0].Path != "/h/plugins/bud-signal/a.py" || len(plan) != 2 {
		t.Errorf("plan %v, kept %v", plan, kept)
	}
}
