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
	"regexp"
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
	Clod         Clod      `json:"clod"`
}

// Clod is your private Claude Code setup repo (D89, D90)
type Clod struct {
	// owner/name; empty means <gh login>/clod
	Repo string `json:"repo"`
}

// Kit is the kit's settings on this machine
type Kit struct {
	Disabled []string `json:"disabled"` // bud kit enable/disable (D58)
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
	// optional extras, off by default (JGA-99)
	Scratchpad bool `json:"scratchpad"` // x opens a persistent notes buffer
	Sessions   bool `json:"sessions"`   // opening a project restores its last session
	PRs        bool `json:"prs"`        // open PRs and review requests per project, via gh
	Todos      bool `json:"todos"`      // TODO/FIXME count per project, via rg
	Clock      bool `json:"clock"`      // the date and time under the robot
	// the robot's idle moves (JGA-155), on unless turned off
	Animate bool `json:"animate"`
}

// UI is how the editor looks beyond the colorscheme
type UI struct {
	// the command line and messages in floats: Neovim's own ui2 (JGA-137),
	// experimental, so it can be turned off
	Messages bool `json:"messages"`
	// the command line in a centered float while you type (D86); with
	// Messages off there's no ui2 to move, so it does nothing then
	CmdlineFloat bool `json:"cmdline_float"`
	// path and symbol breadcrumbs at the top of file windows (dropbar, JGA-139)
	Breadcrumbs bool `json:"breadcrumbs"`
	// markdown files open rendered (render-markdown, D88); off, they open as
	// text and <leader>um still renders one
	Markdown bool `json:"markdown"`
	// the robot in the bottom right corner of the file window, playing the
	// dashboard's moves (D108); off, <leader>ub still turns him on
	Buddy bool `json:"buddy"`
}

type AI struct {
	// which agent bud talks to (D131): "claude" (the default for now, D140:
	// Claude Code directly, not yet over ACP), or over ACP "openclaw" (D132),
	// "claude", "codex", "opencode", or "custom" for ai.custom.command
	Backend  string   `json:"backend"`
	Agent    Agent    `json:"agent"`
	OpenClaw OpenClaw `json:"openclaw"`
	Custom   Custom   `json:"custom"`
	Claude   Claude   `json:"claude"`
	Issues   Issues   `json:"issues"`
	Work     Work     `json:"work"`
}

// Agent is how the agent shows in the editor, whatever the backend
type Agent struct {
	// how the AI chat opens (D137): "pane", the pull-out on the right, or
	// "full", a tab of its own
	ChatLayout string `json:"chat_layout"`
	// what draws the AI chat (D138): "tui", the backend's own TUI, or
	// "bud", bud's own chat drawn from the agent's events
	ChatUI string `json:"chat_ui"`
	// start the project's agent when it opens; off, on first use
	Autostart bool `json:"autostart"`
	// an agent nothing has used for this long is stopped (D49); 0 never
	IdleMinutes int `json:"idle_minutes"`
}

// OpenClaw is the Gateway the openclaw backend reaches (D132)
type OpenClaw struct {
	// the Gateway's WebSocket URL, such as wss://home.tailnet.ts.net:18789;
	// empty uses the openclaw CLI's own gateway.remote.url
	URL string `json:"url"`
	// a file holding the Gateway token, kept out of config.json and
	// process listings; empty uses the CLI's own credentials
	TokenFile string `json:"token_file"`
	// the OpenClaw agent bud's sessions run as: each project gets the
	// Gateway session agent:<this>:bud:<project>
	Agent string `json:"agent"`
}

// Custom is an ACP agent bud doesn't know by name (ai.backend = "custom")
type Custom struct {
	// the agent's command line, such as ["gemini", "--experimental-acp"]
	Command []string `json:"command"`
}

// Backends are the ACP agents bud knows by name (D131)
var Backends = []string{"openclaw", "claude", "codex", "opencode", "custom"}

// Work is where <leader>aw has Claude fix an issue (JGA-237)
type Work struct {
	// "local": a git worktree next to the repo, in a Claude Code session of
	// bud's own (the default); "cloud": a Claude Code cloud session that clones
	// the repo from GitHub and pushes a branch
	Where string `json:"where"`
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
	// the bud checkout Claude works in for issues about bud, remembered by
	// 'bud issue checkout'; only read for ai.issues.bud
	Checkout string `json:"checkout"`
}

type Claude struct {
	Autostart bool   `json:"autostart"`
	Resume    string `json:"resume"` // "new" (the default, D106) or "continue"
	// a session nothing has used for this long is stopped (D49); 0 never
	IdleMinutes int      `json:"idle_minutes"`
	Args        []string `json:"args"`
	// what runs claude, when it isn't claude itself: a wrapper that passes
	// what follows on, such as ["headroom", "wrap", "claude", "--"]
	Command []string `json:"command"`
	// whose Claude Code config bud's sessions use: Claude Code's usual
	// ~/.claude ("home", the default, D77) or bud's own dir ("bud", JGA-135)
	Config string `json:"config"`
}

// githubRepo is a GitHub owner/name: an owner can't start with a hyphen, so
// neither can be read as a flag by gh
var githubRepo = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)

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
		Dashboard:    Dashboard{Style: "silly", Git: true, Health: true, Banners: true, Animate: true},
		UI:           UI{Messages: true, CmdlineFloat: true, Breadcrumbs: true, Markdown: true},
		AI: AI{Backend: "claude", Agent: Agent{ChatLayout: "pane", ChatUI: "tui", Autostart: true, IdleMinutes: 30}, OpenClaw: OpenClaw{Agent: "main"}, Custom: Custom{Command: []string{}},
			Claude: Claude{Autostart: true, Resume: "new", IdleMinutes: 30, Args: []string{}, Config: "home"},
			Issues: Issues{Bud: IssueTarget{Team: "JGA", Project: "Bud IDE"}}, Work: Work{Where: "local"}},
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
		{"ai.backend", s.AI.Backend, Backends},
		{"ai.agent.chat_layout", s.AI.Agent.ChatLayout, []string{"pane", "full"}},
		{"ai.agent.chat_ui", s.AI.Agent.ChatUI, []string{"tui", "bud"}},
		{"ai.claude.resume", s.AI.Claude.Resume, []string{"continue", "new"}},
		{"ai.claude.config", s.AI.Claude.Config, []string{"home", "bud"}},
		{"ai.work.where", s.AI.Work.Where, []string{"local", "cloud"}},
	}
	for _, c := range checks {
		if !slices.Contains(c.allowed, c.value) {
			return fmt.Errorf("%s: %q is not one of %s", c.key, c.value, strings.Join(c.allowed, ", "))
		}
	}
	if s.AI.Backend == "custom" && len(s.AI.Custom.Command) == 0 {
		return fmt.Errorf("ai.backend: custom needs ai.custom.command, the agent's command line")
	}
	if r := s.Clod.Repo; r != "" && !githubRepo.MatchString(r) {
		return fmt.Errorf("clod.repo: %q is not a GitHub owner/name", r)
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
