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
	"time"

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
// gets a new dir and an unchanged one reuses the old. It is built in a temp
// dir and renamed into place, so a reader never sees a half-written plugin,
// and a dir that already holds every file is left alone. Handing a dir out
// touches its mtime, which is what keeps prune from removing one in use
func Dir(p *paths.Paths) (string, error) {
	return extract(source(), p.Plugins())
}

func extract(src fs.FS, root string) (string, error) {
	sum, err := hash(src)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, Name+"-"+sum)
	if complete(src, dir) {
		return handOut(root, dir), nil
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
	// a concurrent bud may have finished while this one wrote: its copy is
	// complete, so keep it and drop ours. Only an incomplete dir is removed
	if complete(src, dir) {
		return handOut(root, dir), nil
	}
	err = os.Rename(tmp, dir)
	if err != nil {
		// the rename fails onto a dir that exists: it is a winner's, or an
		// incomplete one (an interrupted older bud) that is cleared once
		if complete(src, dir) {
			return handOut(root, dir), nil
		}
		os.RemoveAll(dir)
		if err = os.Rename(tmp, dir); err != nil {
			if complete(src, dir) {
				return handOut(root, dir), nil
			}
			return "", err
		}
	}

	return handOut(root, dir), nil
}

// complete says whether dir holds every file of src at its size
func complete(src fs.FS, dir string) bool {
	ok := true
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := d.Info()
		if err != nil {
			return err
		}
		got, err := os.Stat(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil || !got.Mode().IsRegular() || got.Size() != want.Size() {
			ok = false
			return fs.SkipAll
		}

		return nil
	})

	return ok && err == nil
}

// handOut marks dir as in use now, prunes old versions and returns dir
func handOut(root, dir string) string {
	now := time.Now()
	os.Chtimes(dir, now, now)
	prune(root, dir)

	return dir
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

// staleAfter is how long a version other than the one just handed out must
// have gone unused (its mtime is touched at every hand-out) before prune
// removes it. A running session reads its plugin dir after it starts (skills,
// the session-start hook on resume), and two bud binaries side by side hand
// out different versions, so only long-idle ones go
const staleAfter = 14 * 24 * time.Hour

// prune removes the other versions, and temp dirs a crash left behind, that
// nothing has used for staleAfter; keep is never removed. Failures are ignored
// since a stray dir does no harm
func prune(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		if path == keep || !(strings.HasPrefix(e.Name(), Name+"-") || strings.HasPrefix(e.Name(), ".tmp-"+Name+"-")) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleAfter {
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
