// Package config reads and writes the machine config, ~/.local/state/bud/config.json
// (D13). The file only holds what was changed from the defaults, and it's kept as a
// raw JSON tree so keys this binary doesn't know survive a rewrite (D2)
package config

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
)

// Settings is the machine config merged over the defaults
type Settings struct {
	Profile   string    `json:"profile"` // "full" (desktop) or "minimal" (SSH/server)
	Terminal  string    `json:"terminal"`
	Update    Update    `json:"update"`
	Languages []string  `json:"languages"`
	Dashboard Dashboard `json:"dashboard"`
	AI        AI        `json:"ai"`
}

type Update struct {
	Channel string `json:"channel"` // "stable" or "nightly" (D9)
	Auto    bool   `json:"auto"`    // false drops to notify-only
}

type Dashboard struct {
	Style string `json:"style"` // "silly" or "minimal"
}

type AI struct {
	Claude Claude `json:"claude"`
}

type Claude struct {
	Autostart bool     `json:"autostart"`
	Resume    string   `json:"resume"` // "continue" or "new"
	Args      []string `json:"args"`
}

// Profiles are the install profiles; Languages is every language bud ships a spec for (D10)
var (
	Profiles  = []string{"full", "minimal"}
	Languages = []string{"go", "python", "typescript", "c", "bash", "html", "css"}
	Terminals = []string{"kitty", "ghostty"} // D11
)

// PresetLanguages are the languages a profile enables when none are configured
func PresetLanguages(profile string) []string {
	if profile == "minimal" {
		return []string{"python", "bash"}
	}

	return slices.Clone(Languages)
}

// Defaults are the settings with no config.json at all
func Defaults() Settings {
	return Settings{
		Profile:   "full",
		Terminal:  "kitty", // D11
		Update:    Update{Channel: "stable", Auto: true},
		Languages: PresetLanguages("full"),
		Dashboard: Dashboard{Style: "silly"},
		AI:        AI{Claude: Claude{Autostart: true, Resume: "continue", Args: []string{}}},
	}
}

// Validate rejects values outside the ones bud understands
func (s Settings) Validate() error {
	checks := []struct {
		key, value string
		allowed    []string
	}{
		{"profile", s.Profile, Profiles},
		{"terminal", s.Terminal, Terminals},
		{"update.channel", s.Update.Channel, []string{"stable", "nightly"}},
		{"dashboard.style", s.Dashboard.Style, []string{"silly", "minimal"}},
		{"ai.claude.resume", s.AI.Claude.Resume, []string{"continue", "new"}},
	}
	for _, c := range checks {
		if !slices.Contains(c.allowed, c.value) {
			return fmt.Errorf("%s: %q is not one of %s", c.key, c.value, strings.Join(c.allowed, ", "))
		}
	}

	return nil
}

// File is config.json as it is on disk
type File struct {
	Path string
	doc  map[string]any
}

// Load reads path. A missing file is an empty config, not an error
func Load(path string) (*File, error) {
	f := &File{Path: path, doc: map[string]any{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}

	// UseNumber keeps numbers this binary doesn't know about exactly as written
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&f.doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.doc == nil {
		// the file said null
		f.doc = map[string]any{}
	}

	return f, nil
}

// Settings decodes the file over Defaults, so anything it leaves out keeps its default
func (f *File) Settings() (Settings, error) {
	s := Defaults()

	data, err := json.Marshal(f.doc)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s: %w", f.Path, err)
	}

	return s, s.Validate()
}

// Get returns the value at a dotted key like "update.channel", as written in the file
func (f *File) Get(key string) (any, bool) {
	var cur any = f.doc
	for _, part := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}

	return cur, true
}

// Set stores value at a dotted key, creating objects along the way. It doesn't
// touch the disk until Save
func (f *File) Set(key string, value any) error {
	parts := strings.Split(key, ".")
	if slices.Contains(parts, "") {
		return fmt.Errorf("invalid key %q", key)
	}

	m := f.doc
	for i, part := range parts[:len(parts)-1] {
		next, ok := m[part]
		if !ok {
			child := map[string]any{}
			m[part] = child
			m = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is not an object", strings.Join(parts[:i+1], "."))
		}
		m = child
	}
	m[parts[len(parts)-1]] = value

	return nil
}

// Save writes the file atomically: a temp file in the same dir, then a rename,
// so a crash never leaves a half-written config behind
func (f *File) Save() error {
	data, err := json.MarshalIndent(f.doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	// a no-op once the rename succeeds
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), f.Path)
}
