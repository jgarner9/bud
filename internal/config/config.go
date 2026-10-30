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
	"reflect"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Settings is the machine config merged over the defaults
type Settings struct {
	Profile  string `json:"profile"` // "full" (desktop) or "minimal" (SSH/server)
	Terminal string `json:"terminal"`
	// the pane layout projects open in, a file in layouts/ (bud.layout)
	Layout string `json:"layout"`
	Update Update `json:"update"`
	// nil when config.json lists none, which means the profile's preset of
	// the specs (lang.Set.Preset); an empty list enables nothing
	Languages []string `json:"languages"`
	// bud.lang formats a buffer of an enabled language when it's written (D30)
	FormatOnSave bool      `json:"format_on_save"`
	Dashboard    Dashboard `json:"dashboard"`
	UI           UI        `json:"ui"`
	AI           AI        `json:"ai"`
	Tools        Disabled  `json:"tools"` // bud tools enable/disable (D54)
	Kit          Kit       `json:"kit"`
}

// Kit is the kit's settings on this machine
type Kit struct {
	Disabled []string `json:"disabled"` // bud kit enable/disable (D58)
	// how big a project is "large", for entries that only load there (JGA-117)
	LargeFiles int   `json:"large_files"`
	LargeLOC   int64 `json:"large_loc"`
}

// Disabled lists what's turned off on this machine
type Disabled struct {
	Disabled []string `json:"disabled"`
}

type Update struct {
	Channel string `json:"channel"` // "stable" or "nightly" (D9)
	Auto    bool   `json:"auto"`    // false drops to notify-only
}

type Dashboard struct {
	Style string `json:"style"` // "silly" or "minimal"
	// the default extras (JGA-62), on unless turned off
	Git     bool `json:"git"`     // branch and changes per project
	Health  bool `json:"health"`  // startup time, plugins, Claude Code
	Banners bool `json:"banners"` // doctor problems with a one-key fix
	Tip     bool `json:"tip"`     // a keymap a day
	// optional extras, off by default (JGA-99)
	Scratchpad bool `json:"scratchpad"` // x opens a persistent notes buffer
	Sessions   bool `json:"sessions"`   // opening a project restores its last session
	PRs        bool `json:"prs"`        // open PRs and review requests per project, via gh
	Todos      bool `json:"todos"`      // TODO/FIXME count per project, via rg
	Chats      bool `json:"chats"`      // the last few chats, a b e to resume
	Clock      bool `json:"clock"`      // the date and time under the robot
	// the robot's idle moves (JGA-155), on unless turned off
	Animate bool `json:"animate"`
}

// UI is how the editor looks beyond the colorscheme
type UI struct {
	// the command line and messages in floats: Neovim's own ui2 (JGA-137),
	// experimental, so it can be turned off
	Messages bool `json:"messages"`
	// path and symbol breadcrumbs at the top of file windows (dropbar, JGA-139)
	Breadcrumbs bool `json:"breadcrumbs"`
}

type AI struct {
	Claude Claude `json:"claude"`
	Chat   Chat   `json:"chat"`
	Issues Issues `json:"issues"`
}

// Issues is where issues filed from the editor go (JGA-123): Linear's team and
// project, for every project without its own in projects.json
type Issues struct {
	Team    string `json:"team"`
	Project string `json:"project"`
	// issues about bud itself (<leader>aB): bud's own tracker
	Bud IssueTarget `json:"bud"`
}

type IssueTarget struct {
	Team    string `json:"team"`
	Project string `json:"project"`
}

type Chat struct {
	// the layout's chat pane: bud's own ("native", JGA-89) or Claude Code's TUI ("term", D48)
	Pane string `json:"pane"`
}

type Claude struct {
	Autostart bool   `json:"autostart"`
	Resume    string `json:"resume"` // "continue" or "new"
	// a session nothing has used for this long is stopped (D49); 0 never
	IdleMinutes int      `json:"idle_minutes"`
	Args        []string `json:"args"`
	// what runs claude, when it isn't claude itself: a wrapper that passes
	// what follows on, such as ["headroom", "wrap", "claude", "--"]
	Command []string `json:"command"`
	// whose Claude Code config bud's sessions use: bud's own dir ("bud") or
	// Claude Code's usual ~/.claude ("home"), JGA-135
	Config string `json:"config"`
}

// Profiles are the install profiles
var (
	Profiles  = []string{"full", "minimal"}
	Terminals = []string{"kitty", "ghostty"} // D11
)

// Defaults are the settings with no config.json at all
func Defaults() Settings {
	return Settings{
		Profile:      "full",
		Terminal:     "kitty", // D11
		Layout:       "default",
		Update:       Update{Channel: "stable", Auto: true},
		FormatOnSave: true,
		Dashboard:    Dashboard{Style: "silly", Git: true, Health: true, Banners: true, Tip: true, Animate: true},
		UI:           UI{Messages: true, Breadcrumbs: true},
		AI: AI{Claude: Claude{Autostart: true, Resume: "continue", IdleMinutes: 30, Args: []string{}, Config: "bud"}, Chat: Chat{Pane: "native"},
			Issues: Issues{Bud: IssueTarget{Team: "JGA", Project: "Bud IDE"}}},
		Kit: Kit{LargeFiles: 10000, LargeLOC: 1000000},
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
		{"ai.claude.config", s.AI.Claude.Config, []string{"bud", "home"}},
		{"ai.chat.pane", s.AI.Chat.Pane, []string{"native", "term"}},
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
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			return s, fmt.Errorf("%s: %s must be %s, not %s", f.Path, te.Field, kindName(te.Type), article(te.Value))
		}
		return s, fmt.Errorf("%s: %w", f.Path, err)
	}

	return s, s.Validate()
}

// kindName says what a setting's Go type is in a user's words
func kindName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int64, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Slice:
		return "a list"
	case reflect.Map, reflect.Struct:
		return "an object"
	}
	return "a " + t.String()
}

// article puts a or an before a JSON kind ("string", "array", ...)
func article(kind string) string {
	if kind == "array" {
		return "a list"
	}
	if kind != "" && strings.ContainsAny(kind[:1], "aeiou") {
		return "an " + kind
	}
	return "a " + kind
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

// Unset removes a dotted key, so it goes back to its default, and drops the
// objects it leaves empty. A key that isn't there is fine
func (f *File) Unset(key string) {
	parts := strings.Split(key, ".")
	var walk func(m map[string]any, i int)
	walk = func(m map[string]any, i int) {
		if i == len(parts)-1 {
			delete(m, parts[i])
			return
		}
		child, ok := m[parts[i]].(map[string]any)
		if !ok {
			return
		}
		walk(child, i+1)
		if len(child) == 0 {
			delete(m, parts[i])
		}
	}
	walk(f.doc, 0)
}

// Doc is the whole file as written, for printing
func (f *File) Doc() map[string]any {
	return f.doc
}

// Known says whether a dotted key is a setting bud has: a leaf of Defaults
// (objects themselves can't be set whole)
func Known(key string) bool {
	_, ok := lookup(defaultsTree(), key)
	return ok
}

// Effective returns a dotted key's value with defaults filled in: the setting
// as bud sees it, or an unknown key as written in the file
func (f *File) Effective(key string) (any, bool) {
	s, err := f.Settings()
	if err == nil {
		data, _ := json.Marshal(s)
		var tree map[string]any
		if json.Unmarshal(data, &tree) == nil {
			if v, ok := lookup(tree, key); ok {
				return v, true
			}
		}
	}

	return f.Get(key)
}

func defaultsTree() map[string]any {
	data, _ := json.Marshal(Defaults())
	var tree map[string]any
	_ = json.Unmarshal(data, &tree)
	return tree
}

func lookup(tree map[string]any, key string) (any, bool) {
	var cur any = tree
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

// Save writes the file atomically, so a crash never leaves a half-written config behind
func (f *File) Save() error {
	data, err := json.MarshalIndent(f.doc, "", "  ")
	if err != nil {
		return err
	}

	return atomicfile.WriteFile(f.Path, append(data, '\n'), 0o644)
}
