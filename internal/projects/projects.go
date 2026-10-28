// Package projects is the project registry, ~/.local/state/bud/projects.json:
// the projects bud has opened or been told about, which the dashboard and the
// project picker list (BASE.md 5.4). It's plain JSON so Lua can read it with
// vim.json.decode. Recent files aren't kept here: Neovim's oldfiles already
// has them, filtered by project root where they're shown
package projects

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
)

// Project is one registered project
type Project struct {
	Name   string `json:"name"` // unique in the registry; the dir's name unless that's taken
	Path   string `json:"path"` // absolute, symlinks resolved
	Pinned bool   `json:"pinned"`
	// the pane layout it opens in (D45); empty means config.json's
	Layout string `json:"layout,omitempty"`
	// zero, and left out of the file, when it was added but never opened through bud
	LastOpened time.Time `json:"last_opened,omitzero"`
	// how big it is, measured in the background when it opens (JGA-117); nil until then
	Size *Size `json:"size,omitempty"`
	// where issues filed from it go (JGA-123); nil means config.json's ai.issues
	Issues *IssueTarget `json:"issues,omitempty"`

	// keys this binary doesn't know, kept as they were (D2)
	extra map[string]json.RawMessage
}

var projectKeys = []string{"name", "path", "pinned", "layout", "last_opened", "size", "issues"}

// IssueTarget is the Linear team and project issues are filed in
type IssueTarget struct {
	Team    string `json:"team"`
	Project string `json:"project,omitempty"`
}

// Size is a project's tracked source: files, and lines estimated from bytes
type Size struct {
	Files    int       `json:"files"`
	LOC      int64     `json:"loc"`
	Measured time.Time `json:"measured"`
}

func (p *Project) UnmarshalJSON(data []byte) error {
	type plain Project
	if err := json.Unmarshal(data, (*plain)(p)); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for _, key := range projectKeys {
		delete(all, key)
	}
	p.extra = all

	return nil
}

func (p Project) MarshalJSON() ([]byte, error) {
	type plain Project
	known, err := json.Marshal(plain(p))
	if err != nil || len(p.extra) == 0 {
		return known, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(known, &all); err != nil {
		return nil, err
	}
	for k, v := range p.extra {
		all[k] = v
	}

	return json.Marshal(all)
}

// Missing reports whether the project's directory is gone (deleted, moved, or
// on a drive that isn't mounted). It stays registered until removed
func (p Project) Missing() bool {
	info, err := os.Stat(p.Path)
	return err != nil || !info.IsDir()
}

// Registry is projects.json as it is on disk
type Registry struct {
	Path     string
	Projects []Project

	// top-level keys this binary doesn't know (D2)
	extra map[string]json.RawMessage
}

// Load reads path. A missing file is an empty registry
func Load(path string) (*Registry, error) {
	r := &Registry{Path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if raw, ok := doc["projects"]; ok {
		if err := json.Unmarshal(raw, &r.Projects); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		delete(doc, "projects")
	}
	r.extra = doc

	return r, nil
}

// Save writes the registry atomically
func (r *Registry) Save() error {
	doc := map[string]any{}
	for k, v := range r.extra {
		doc[k] = v
	}
	projects := r.Projects
	if projects == nil {
		projects = []Project{}
	}
	doc["projects"] = projects

	// marshal, then indent the whole document: an encoder's indent doesn't
	// reach into what Project.MarshalJSON returns
	compact, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')

	return atomicfile.WriteFile(r.Path, buf.Bytes(), 0o644)
}

// canonical makes dir absolute and resolves symlinks, so one project can't be
// registered twice under two spellings
func canonical(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}

	return real, nil
}

// Add registers dir, or returns the project it already is. opened sets its last
// opened time (bud <dir> does; bud projects add doesn't)
func (r *Registry) Add(dir string, opened time.Time) (Project, error) {
	path, err := canonical(dir)
	if err != nil {
		return Project{}, err
	}

	if i := slices.IndexFunc(r.Projects, func(p Project) bool { return p.Path == path }); i >= 0 {
		if !opened.IsZero() {
			r.Projects[i].LastOpened = opened
		}
		return r.Projects[i], nil
	}

	p := Project{Name: r.uniqueName(path), Path: path, LastOpened: opened}
	r.Projects = append(r.Projects, p)

	return p, nil
}

// uniqueName is the dir's name, with more of its path in front while that's
// taken: api, then work/api, then the whole path
func (r *Registry) uniqueName(path string) string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	for n := 1; n <= len(parts); n++ {
		name := strings.Join(parts[len(parts)-n:], "/")
		if _, taken := r.Get(name); !taken {
			return name
		}
	}

	return path
}

// Get finds a project by name, or by its path
func (r *Registry) Get(nameOrPath string) (Project, bool) {
	i := r.index(nameOrPath)
	if i < 0 {
		return Project{}, false
	}

	return r.Projects[i], true
}

func (r *Registry) index(nameOrPath string) int {
	if i := slices.IndexFunc(r.Projects, func(p Project) bool { return p.Name == nameOrPath }); i >= 0 {
		return i
	}
	// a path may be relative, or a symlink to the registered one
	path, err := canonical(nameOrPath)
	if err != nil {
		path = filepath.Clean(nameOrPath)
	}

	return slices.IndexFunc(r.Projects, func(p Project) bool { return p.Path == path })
}

// Remove drops a project from the registry; its files are untouched
func (r *Registry) Remove(nameOrPath string) (Project, error) {
	i := r.index(nameOrPath)
	if i < 0 {
		return Project{}, fmt.Errorf("no project %q (see 'bud projects ls')", nameOrPath)
	}
	p := r.Projects[i]
	r.Projects = slices.Delete(r.Projects, i, i+1)

	return p, nil
}

// SetPinned pins or unpins a project; pinned ones come first and show on the dashboard
func (r *Registry) SetPinned(nameOrPath string, pinned bool) (Project, error) {
	i := r.index(nameOrPath)
	if i < 0 {
		return Project{}, fmt.Errorf("no project %q (see 'bud projects ls')", nameOrPath)
	}
	r.Projects[i].Pinned = pinned

	return r.Projects[i], nil
}

// SetLayout picks the layout a project opens in; "" goes back to the default
func (r *Registry) SetLayout(nameOrPath, layout string) (Project, error) {
	i := r.index(nameOrPath)
	if i < 0 {
		return Project{}, fmt.Errorf("no project %q (see 'bud projects ls')", nameOrPath)
	}
	r.Projects[i].Layout = layout

	return r.Projects[i], nil
}

// SetSize records how big a project is
func (r *Registry) SetSize(nameOrPath string, size Size) (Project, error) {
	i := r.index(nameOrPath)
	if i < 0 {
		return Project{}, fmt.Errorf("no project %q", nameOrPath)
	}
	r.Projects[i].Size = &size

	return r.Projects[i], nil
}

// SetIssues picks where a project's issues are filed; nil goes back to config.json's
func (r *Registry) SetIssues(nameOrPath string, target *IssueTarget) (Project, error) {
	i := r.index(nameOrPath)
	if i < 0 {
		return Project{}, fmt.Errorf("no project %q (see 'bud projects ls')", nameOrPath)
	}
	r.Projects[i].Issues = target

	return r.Projects[i], nil
}

// Sorted returns the projects the way they're listed: pinned first, then the
// most recently opened, then by name

func (r *Registry) Sorted() []Project {
	out := slices.Clone(r.Projects)
	slices.SortStableFunc(out, func(a, b Project) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		if c := b.LastOpened.Compare(a.LastOpened); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	return out
}
