package syncer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	bud "jgarner.dev/bud"
	"jgarner.dev/bud/internal/profile"
)

// repoContent is the repo's content/ as a tree to publish
func repoContent(t *testing.T) fstest.MapFS {
	t.Helper()
	root := filepath.Join("..", "..", "content")
	tree := fstest.MapFS{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		tree[filepath.ToSlash(rel)] = &fstest.MapFile{Data: b}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tree
}

// Build plan task 1.11: the content this repo ships, synced with the
// dev-machine example profile, gives OpenCode its skills, agents and the
// coding fragment
func TestTheShippedContentSyncsOnADevMachine(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", repoContent(t))
	o := options(t, s)
	o.Device = "dev-machine"
	o.Profiles = func() []profile.Dir { return []profile.Dir{{Name: "bud's examples", FS: bud.Profiles()}} }

	run(t, o)

	skills, err := os.ReadDir(filepath.Join(o.Paths.Active(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range skills {
		ids = append(ids, e.Name())
		b, err := os.ReadFile(filepath.Join(o.Paths.Active(), "skills", e.Name(), "SKILL.md"))
		if err != nil {
			t.Errorf("skill %s: %v", e.Name(), err)
			continue
		}
		// OpenCode lists a skill only when it has a description
		if !strings.HasPrefix(string(b), "---\nname: "+e.Name()+"\ndescription: ") {
			t.Errorf("skill %s's front matter doesn't start with its name and a description:\n%.200s", e.Name(), b)
		}
	}
	for _, want := range []string{"bud-dev", "file-issue"} {
		if !strings.Contains(strings.Join(ids, " "), want) {
			t.Errorf("active skills %v lack %s", ids, want)
		}
	}

	if got := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "agents", "chat.md")); !strings.Contains(got, "mode: primary") {
		t.Errorf("agents/chat.md = %q", got)
	}
	cfg := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"))
	for _, want := range []string{
		filepath.Join(o.Paths.ContentCurrent(), "AGENTS.md"),
		`"formatter": true`,
		`"action": "external_directory"`,
		`"resource": "go test *"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("opencode.jsonc lacks %s:\n%s", want, cfg)
		}
	}
}
