package syncer

import (
	"errors"
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
		if args[len(args)-1] == "broken" {
			return []byte("curator: no skill named 'broken'"), errors.New("exit status 1")
		}
		return nil, nil
	}
	if notes := lockSkills("", []string{"a", "b"}, func(string, ...any) {}); notes != nil {
		t.Errorf("notes: %q", notes)
	}
	if notes := lockSkills("cloud", []string{"a"}, func(string, ...any) {}); notes != nil {
		t.Errorf("notes: %q", notes)
	}
	if want := []string{"/usr/bin/hermes curator lock a", "/usr/bin/hermes curator lock b", "/usr/bin/hermes -p cloud curator lock a"}; !slices.Equal(ran, want) {
		t.Errorf("ran %q, want %q", ran, want)
	}

	notes := lockSkills("cloud", []string{"broken"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "no skill named 'broken'") || !strings.Contains(notes[0], "hermes -p cloud curator lock") {
		t.Errorf("a failed lock is named, with the profile: %q", notes)
	}
	notes = lockSkills("", []string{"broken"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "no skill named 'broken'") {
		t.Errorf("a failed lock is named: %q", notes)
	}

	hermesBin = func() (string, error) { return "", errors.New("not found") }
	notes = lockSkills("", []string{"a"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "Hermes isn't on PATH") {
		t.Errorf("without Hermes: %q", notes)
	}
	if lockSkills("", nil, nil) != nil {
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
		locked = append(locked, strings.Join(args[:len(args)-3], " ")+":"+args[len(args)-1])
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
	for _, d := range []string{dir, filepath.Join(o.Paths.HermesCloud(), "skills")} {
		for _, id := range []string{"learn", "plan"} {
			if got := read(t, filepath.Join(d, id, "SKILL.md")); !strings.Contains(got, "name: "+id) {
				t.Errorf("Hermes's copy of %s in %s = %q", id, d, got)
			}
		}
	}
	if !slices.Equal(locked, []string{":learn", ":plan", "-p cloud:learn", "-p cloud:plan"}) {
		t.Errorf("locked %q, want both skills in both profiles", locked)
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
	if !slices.Equal(locked, []string{":learn", "-p cloud:learn"}) {
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

// Suite D95: the cloud profile is rendered beside the private one, and a
// private .env holding a cloud provider's key stops the sync
func TestAServerSyncRendersBothHermesProfilesAndRefusesACloudKey(t *testing.T) {
	s := newServer(t)
	content := tree(t)
	content["env/server.jsonc"] = &fstest.MapFile{Data: repoFile(t, "server.jsonc")}
	content["env/registry.jsonc"] = &fstest.MapFile{Data: repoFile(t, "registry.jsonc")}
	s.publish("2026.11.17.1", "edge", content)
	o := options(t, s)
	o.Device = "home"
	examples := o.Profiles
	o.Profiles = func() []profile.Dir {
		return append(examples(), profile.Dir{Name: "home", FS: fstest.MapFS{
			"home.json": {Data: []byte(`{"name": "home", "components": ["server"], "channel": "edge", "content": {"env": ["base", "server"]}}`)},
		}})
	}
	if err := os.MkdirAll(filepath.Dir(o.Paths.HermesEnv()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.Paths.HermesEnv(), []byte("OP_SERVICE_ACCOUNT_TOKEN=x\nexport OPENCODE_GO_API_KEY=sk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), o); err == nil || !strings.Contains(err.Error(), "OPENCODE_GO_API_KEY") || !strings.Contains(err.Error(), "profiles/cloud/.env") {
		t.Fatalf("a private .env with the Go key: got %v", err)
	}
	if _, err := os.Stat(o.Paths.HermesConfig()); !os.IsNotExist(err) {
		t.Errorf("nothing should be written when the key is found; stat: %v", err)
	}

	if err := os.WriteFile(o.Paths.HermesEnv(), []byte("OP_SERVICE_ACCOUNT_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, o)
	private := read(t, o.Paths.HermesConfig())
	cloud := read(t, filepath.Join(o.Paths.HermesCloud(), "config.yaml"))
	if strings.Contains(private, "opencode.ai") || strings.Contains(private, "OPENCODE_GO_API_KEY") {
		t.Errorf("the private profile reaches the Go provider:\n%s", private)
	}
	if !strings.Contains(cloud, "opencode.ai") || !strings.Contains(cloud, "standalone: true") {
		t.Errorf("the cloud profile should list Go and stay standalone:\n%s", cloud)
	}
}
