package kit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Kinds are the kinds of kit entry bud kit add takes
var Kinds = []string{"plugin", "marketplace"}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(@[a-z0-9][a-z0-9._-]*)?$`)

// Repo is a kit/ dir in a bud checkout, which bud kit add and rm edit
type Repo struct {
	Dir string // <checkout>/kit
}

func (r Repo) index() (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(r.Dir, "kit.json"))
	if err != nil {
		return nil, err
	}
	var idx map[string]any
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}

	return idx, nil
}

func (r Repo) saveIndex(idx map[string]any) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(r.Dir, "kit.json"), append(data, '\n'), 0o644)
}

func list(idx map[string]any, key string) []string {
	raw, _ := idx[key].([]any)
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func setList(idx map[string]any, key string, names []string) {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = n
	}
	idx[key] = out
}

func marketplaces(idx map[string]any) map[string]any {
	m, _ := idx["marketplaces"].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// Find says which kind an entry in kit.json is, or "" when there's none
func (r Repo) Find(name string) (string, error) {
	idx, err := r.index()
	if err != nil {
		return "", err
	}
	if slices.Contains(list(idx, "plugins"), name) {
		return "plugin", nil
	}
	if _, ok := marketplaces(idx)[name]; ok {
		return "marketplace", nil
	}

	return "", nil
}

// AddPlugin lists a Claude Code plugin (name@marketplace) in the kit
func (r Repo) AddPlugin(id string) error {
	if !strings.Contains(id, "@") || !namePattern.MatchString(id) {
		return fmt.Errorf("give the plugin as name@marketplace, e.g. bud@bud-plugins")
	}
	idx, err := r.index()
	if err != nil {
		return err
	}
	if have, _ := r.Find(id); have != "" {
		return fmt.Errorf("the kit already has the %s %s", have, id)
	}
	setList(idx, "plugins", append(list(idx, "plugins"), id))

	return r.saveIndex(idx)
}

// AddMarketplace lists a plugin marketplace in the kit: its name (the one in
// its marketplace.json) and where claude plugin marketplace add gets it
func (r Repo) AddMarketplace(name, source string) error {
	if !namePattern.MatchString(name) || strings.Contains(name, "@") {
		return fmt.Errorf("%q isn't a usable name: lowercase letters, digits, dots and dashes", name)
	}
	if source == "" {
		return fmt.Errorf("a marketplace needs a source, e.g. https://github.com/jgarner9/bud-plugins.git")
	}
	idx, err := r.index()
	if err != nil {
		return err
	}
	if have, _ := r.Find(name); have != "" {
		return fmt.Errorf("the kit already has the %s %s", have, name)
	}
	m := marketplaces(idx)
	m[name] = source
	idx["marketplaces"] = m

	return r.saveIndex(idx)
}

// Remove takes an entry out of kit.json; the next sync removes it from
// Claude Code
func (r Repo) Remove(name string) (string, error) {
	kind, err := r.Find(name)
	if err != nil {
		return "", err
	}
	idx, err := r.index()
	if err != nil {
		return "", err
	}
	switch kind {
	case "plugin":
		setList(idx, "plugins", slices.DeleteFunc(list(idx, "plugins"), func(n string) bool { return n == name }))
	case "marketplace":
		m := marketplaces(idx)
		delete(m, name)
		idx["marketplaces"] = m
	default:
		return "", fmt.Errorf("the kit has nothing called %s", name)
	}

	return kind, r.saveIndex(idx)
}
