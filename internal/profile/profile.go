// Package profile reads device profiles (Suite D18, D57, D61). A profile
// names what one device runs (its components), which update channel it
// follows and which content it takes. The public repo ships examples in
// profiles/; real ones live in the private overlay, and an overlay profile
// replaces the example of the same name. The format is in docs/profiles.md
package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Components are the parts a device can run
var Components = []string{
	"ide",     // the Neovim IDE (ide/)
	"harness", // OpenCode, the local coding harness (Suite D15)
	"app",     // the Tauri chat app (Suite D6)
	"server",  // the home-server units: Hermes, planning OpenCode, router, curator, KB site (Suite D48, D49)
	"qwen",    // llama.cpp and the pinned local models (Suite D25)
}

// Channels are the update channels (Suite D61)
var Channels = []string{"stable", "edge"}

// Kinds are the kinds of content a profile selects
var Kinds = []string{"skills", "agents", "commands", "plugins", "env"}

// DefaultEnv is the env fragment a profile takes when it names none
var DefaultEnv = []string{"base"}

type Profile struct {
	Name       string   `json:"name"`
	Components []string `json:"components"`
	Channel    string   `json:"channel"`
	Content    Content  `json:"content"`
}

// Content holds a glob list per kind of content. A kind left out takes
// everything (Suite D57: skills cost only a name and a description until
// used), except env, which takes DefaultEnv, since fragments are per device
type Content struct {
	Skills   []string `json:"skills,omitempty"`
	Agents   []string `json:"agents,omitempty"`
	Commands []string `json:"commands,omitempty"`
	Plugins  []string `json:"plugins,omitempty"`
	Env      []string `json:"env,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Parse decodes a profile; a field it doesn't know is an error
func Parse(b []byte) (*Profile, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("there is more after the profile's closing }")
	}

	return &p, nil
}

// Validate checks the profile and reports every problem at once, each with
// what to fix
func (p *Profile) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if !nameRe.MatchString(p.Name) {
		add("name: %q must be lowercase letters, digits and '-'", p.Name)
	}
	if len(p.Components) == 0 {
		add("components: empty; list what this device runs, from %s", strings.Join(Components, ", "))
	}
	seen := map[string]bool{}
	for _, c := range p.Components {
		if !slices.Contains(Components, c) {
			add("components: %q isn't one of %s", c, strings.Join(Components, ", "))
		}
		if seen[c] {
			add("components: %q is listed twice", c)
		}
		seen[c] = true
	}
	if !slices.Contains(Channels, p.Channel) {
		add("channel: %q isn't one of %s", p.Channel, strings.Join(Channels, ", "))
	}
	for _, kind := range Kinds {
		for _, g := range p.Content.globs(kind) {
			if _, err := path.Match(g, ""); err != nil || g == "" || strings.Contains(g, "/") {
				add("content.%s: %q isn't a glob over IDs (like \"*\" or \"plan-*\")", kind, g)
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	slices.Sort(errs)
	word := "problems"
	if len(errs) == 1 {
		word = "problem"
	}

	return fmt.Errorf("%d %s:\n  %s", len(errs), word, strings.Join(errs, "\n  "))
}

// Has says whether the device runs component c
func (p *Profile) Has(c string) bool { return slices.Contains(p.Components, c) }

// Selects says whether the profile takes the content item id of kind
func (p *Profile) Selects(kind, id string) bool {
	for _, g := range p.Content.globs(kind) {
		if ok, _ := path.Match(g, id); ok {
			return true
		}
	}

	return false
}

func (c Content) globs(kind string) []string {
	var g []string
	switch kind {
	case "skills":
		g = c.Skills
	case "agents":
		g = c.Agents
	case "commands":
		g = c.Commands
	case "plugins":
		g = c.Plugins
	case "env":
		// nil is left out; an explicit [] takes nothing, as for the other kinds
		if c.Env == nil {
			return DefaultEnv
		}
		return c.Env
	}
	if g == nil {
		return []string{"*"}
	}

	return g
}

// Find loads the profile called name from the first of dirs that has it, so
// passing the overlay's profiles/ before the public one lets the overlay win.
// The profile's name must match its file's
func Find(name string, dirs ...string) (*Profile, string, error) {
	if !nameRe.MatchString(name) {
		return nil, "", fmt.Errorf("%q isn't a profile name: lowercase letters, digits and '-'", name)
	}
	for _, dir := range dirs {
		file := filepath.Join(dir, name+".json")
		b, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("read the profile: %w", err)
		}
		p, err := Parse(b)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", file, err)
		}
		if err := p.Validate(); err != nil {
			return nil, "", fmt.Errorf("%s: %w", file, err)
		}
		if p.Name != name {
			return nil, "", fmt.Errorf("%s: its name is %q; rename the file or set \"name\": %q", file, p.Name, name)
		}
		return p, file, nil
	}

	names, _ := List(dirs...)
	hint := "none found"
	if len(names) > 0 {
		hint = "there are " + strings.Join(names, ", ")
	}

	return nil, "", fmt.Errorf("no profile called %q in %s (%s)", name, strings.Join(dirs, ", "), hint)
}

// List returns the profile names across dirs, sorted, each once
func List(dirs ...string) ([]string, error) {
	var names []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	slices.Sort(names)

	return names, nil
}
