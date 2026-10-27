package bud

import (
	"io/fs"
	"slices"
	"testing"
)

func TestConfig(t *testing.T) {
	cfg := Config()

	for _, path := range []string{"init.lua", "nvim-pack-lock.json", "lua/bud/pack.lua", "languages/_template.lua"} {
		if _, err := fs.Stat(cfg, path); err != nil {
			t.Errorf("%s missing from embedded config: %v", path, err)
		}
	}

	// rooted at nvim/, so the prefix shouldn't appear
	if _, err := fs.Stat(cfg, "nvim"); err == nil {
		t.Error("embedded config should be rooted inside nvim/")
	}

	if info, err := fs.Stat(cfg, "languages"); err != nil || !info.IsDir() || info.Name() != "languages" {
		t.Errorf("languages should be a directory named languages, got %v, %v", info, err)
	}
}

func TestConfigWalk(t *testing.T) {
	var files []string
	err := fs.WalkDir(Config(), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// SyncConfig walks the tree, so the mounted dir has to show up in it
	for _, want := range []string{"init.lua", "lua/bud/pack.lua", "languages/_template.lua"} {
		if !slices.Contains(files, want) {
			t.Errorf("walk missed %s: %v", want, files)
		}
	}
}

func TestConfigInvalidPath(t *testing.T) {
	if _, err := Config().Open("../embed.go"); err == nil {
		t.Error("expected an error for a path outside the tree")
	}
	if _, err := fs.ReadDir(Config(), "/languages"); err == nil {
		t.Error("expected an error for an absolute path")
	}
}
