// Package bud holds the repo files that ship inside the bud binary
package bud

import (
	"embed"
	"io/fs"
	"slices"
	"strings"
)

// languages/*.lua names the files, so _template.lua comes along: a directory
// pattern would leave out names starting with _
//
//go:embed nvim languages/*.lua
var repoFS embed.FS

// Config returns what bud syncs into ~/.config/bud: the repo's nvim/ directory
// at the root, so paths read like "init.lua", with languages/ inside it
func Config() fs.FS {
	return configFS{
		root:   sub("nvim"),
		mounts: map[string]fs.FS{"languages": sub("languages")},
	}
}

func sub(dir string) fs.FS {
	s, err := fs.Sub(repoFS, dir)
	if err != nil {
		// only fails on a bad path, which the embed directive above rules out
		panic(err)
	}

	return s
}

// configFS is root with each mount showing up as a top-level directory
type configFS struct {
	root   fs.FS
	mounts map[string]fs.FS
}

// resolve returns the filesystem that holds name and the path inside it
func (c configFS) resolve(name string) (fs.FS, string) {
	top, rest, _ := strings.Cut(name, "/")
	if m, ok := c.mounts[top]; ok {
		if rest == "" {
			rest = "."
		}
		return m, rest
	}

	return c.root, name
}

func (c configFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	fsys, rel := c.resolve(name)

	return fsys.Open(rel)
}

// ReadDir lists the mounts alongside root's own entries, which is what lets
// fs.WalkDir find them
func (c configFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if name != "." {
		fsys, rel := c.resolve(name)
		return fs.ReadDir(fsys, rel)
	}

	entries, err := fs.ReadDir(c.root, ".")
	if err != nil {
		return nil, err
	}
	for dir := range c.mounts {
		// the Sub'd FS opens "." as the embedded dir, so the name is dir's own
		info, err := fs.Stat(c, dir)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })

	return entries, nil
}
