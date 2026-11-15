package overlay

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Layers name where an item came from, for the ledger and bud doctor
const (
	Public  = "public"
	Private = "overlay"
)

// Item is one piece of content after layering: a skill or plugin dir, an
// agent, command or env file, or an instructions file
type Item struct {
	Kind  string // skills, agents, commands, plugins, env or instructions
	ID    string // the skill's dir name, the file name without .md or .jsonc
	Layer string // Public or Private
	Path  string // the file or dir on disk
	// Overrides says a Private item replaced a Public one with its ID
	Overrides bool
}

// Layer lays the overlay over the public content dir and returns every
// item, sorted by kind, ID and then layer, public first. overlayDir is the
// overlay clone's root, or empty for none; its content/ holds personal
// skills, agents, commands, plugins and AGENTS.md, and its env/ holds env
// fragments (Suite D59).
//
// Skills, plugins, agents and commands are replaced by ID: an overlay skill
// replaces the public skill of the same name whole, so no public file of it
// is left behind. Env fragments and AGENTS.md are kept from both layers,
// public first, because they are merged when a config is rendered and the
// later one wins there
func Layer(publicDir, overlayDir string) ([]Item, error) {
	pub, err := scan(publicDir, filepath.Join(publicDir, "env"), Public)
	if err != nil {
		return nil, err
	}
	if overlayDir == "" {
		sortItems(pub)
		return pub, nil
	}
	if err := CheckFormat(overlayDir); err != nil {
		return nil, err
	}
	priv, err := scan(filepath.Join(overlayDir, "content"), filepath.Join(overlayDir, "env"), Private)
	if err != nil {
		return nil, err
	}

	replaced := map[string]bool{}
	for _, it := range priv {
		if replacedByID(it.Kind) {
			replaced[it.Kind+"/"+it.ID] = true
		}
	}
	var out []Item
	for _, it := range pub {
		if !replaced[it.Kind+"/"+it.ID] {
			out = append(out, it)
		}
	}
	for _, it := range priv {
		if replacedByID(it.Kind) && slices.ContainsFunc(pub, func(p Item) bool { return p.Kind == it.Kind && p.ID == it.ID }) {
			it.Overrides = true
		}
		out = append(out, it)
	}
	sortItems(out)

	return out, nil
}

// sortItems orders items by kind, then ID, then layer, public first so the
// overlay applies last
func sortItems(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int {
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return strings.Compare(layerOrder(a.Layer), layerOrder(b.Layer))
	})
}

func replacedByID(kind string) bool { return kind != "env" && kind != "instructions" }

func layerOrder(l string) string {
	if l == Public {
		return "0"
	}

	return "1"
}

// scan lists one layer's items: dir holds AGENTS.md, skills/, agents/,
// commands/ and plugins/, envDir the env fragments. A dir that's missing
// just has nothing in it
func scan(dir, envDir, layer string) ([]Item, error) {
	var items []Item
	add := func(kind, id, path string) { items = append(items, Item{Kind: kind, ID: id, Layer: layer, Path: path}) }

	if fi, err := os.Lstat(filepath.Join(dir, "AGENTS.md")); err == nil && fi.Mode().IsRegular() {
		add("instructions", "AGENTS", filepath.Join(dir, "AGENTS.md"))
	}
	for _, kind := range []string{"skills", "plugins"} {
		entries, err := readDir(filepath.Join(dir, kind))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if kind == "skills" {
				if _, err := os.Stat(filepath.Join(dir, kind, e.Name(), "SKILL.md")); err != nil {
					return nil, fmt.Errorf("%s: a skill dir needs a SKILL.md", filepath.Join(dir, kind, e.Name()))
				}
			}
			add(kind, e.Name(), filepath.Join(dir, kind, e.Name()))
		}
	}
	for kind, ext := range map[string]string{"agents": ".md", "commands": ".md"} {
		if err := files(filepath.Join(dir, kind), ext, func(id, path string) { add(kind, id, path) }); err != nil {
			return nil, err
		}
	}
	if err := files(envDir, ".jsonc", func(id, path string) { add("env", id, path) }); err != nil {
		return nil, err
	}

	return items, nil
}

// files calls fn for each regular file in dir ending in ext
func files(dir, ext string, fn func(id, path string)) error {
	entries, err := readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ext)
		if ok && e.Type().IsRegular() && !strings.HasPrefix(id, ".") {
			fn(id, filepath.Join(dir, e.Name()))
		}
	}

	return nil
}

func readDir(dir string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	return entries, err
}
