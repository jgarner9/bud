// Package plugin hands bud's embedded bud-ide plugin (D142) to Claude Code
// sessions the IDE starts, with --plugin-dir. Nothing is installed into Claude
// Code's config, so a claude run outside bud never loads it
package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	bud "jgarner.dev/bud"
	"jgarner.dev/bud/internal/paths"
)

// Name is the plugin's name, and so the prefix of its agent and skills
// (bud-ide:chat, bud-ide:file-issue)
const Name = "bud-ide"

// source is the embedded plugin; swappable in tests
var source = bud.Plugin

// Dir extracts the embedded plugin under p.Plugins() and returns its dir. The
// dir name carries a hash of the content, so a new bud with a changed plugin
// gets a new dir and an unchanged one reuses the old. It is written to a temp
// dir and renamed into place, so a reader never sees a half-written plugin
func Dir(p *paths.Paths) (string, error) {
	return extract(source(), p.Plugins())
}

func extract(src fs.FS, root string) (string, error) {
	sum, err := hash(src)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, Name+"-"+sum)
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".tmp-"+Name+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := write(src, tmp); err != nil {
		return "", err
	}
	// a half-made dir from before the rename was atomic has no manifest, so
	// clear it; a concurrent bud that renamed first wins and we keep its copy
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		if _, serr := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); serr == nil {
			prune(root, dir)
			return dir, nil
		}
		return "", err
	}
	prune(root, dir)

	return dir, nil
}

// hash is a short digest of every path and file in src, in walk order
func hash(src fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", path, len(data))
		h.Write(data)

		return nil
	})
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

func write(src fs.FS, dir string) error {
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o644)
	})
}

// prune removes the other versions. A
// session still running from an older one has already read what it needs at
// start; failures are ignored since a stray dir does no harm
func prune(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		if path != keep && strings.HasPrefix(e.Name(), Name+"-") {
			os.RemoveAll(path)
		}
	}
}

// Args is the claude flag that loads the plugin, for the sessions the IDE
// starts; bud agent and bud chat add it, and the editor asks for the dir with
// bud plugin path
func Args() ([]string, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}
	dir, err := Dir(p)
	if err != nil {
		return nil, err
	}

	return []string{"--plugin-dir", dir}, nil
}
