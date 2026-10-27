// Package lang reads the language specs (BASE.md 2) for the CLI. Specs are Lua,
// so rather than a second format there's one reader: Load runs headless Neovim
// with this binary's bud.lang and gets the specs back as JSON
package lang

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"jgarner.dev/bud"
)

// Spec is the part of a language spec the CLI uses
type Spec struct {
	Name       string   `json:"name"`
	Filetypes  []string `json:"filetypes"`
	Treesitter []string `json:"treesitter"`
	Mason      []string `json:"mason"`
	// binaries the language needs from the system package manager
	System []string `json:"system"`
	Root   []string `json:"root"`
	// in the minimal profile's preset (D30)
	Minimal bool `json:"minimal"`
}

// Set is every spec that was found
type Set struct {
	Specs  []Spec            // the ones that loaded, by name
	Errors map[string]string // the ones that didn't, by name, with why
}

// swappable in tests
var nvimBin = "nvim"

// Load reads the specs in dirs; when two have a spec of the same name, the
// earlier dir's wins. With shipped, the specs embedded in this binary come
// first, so the language list doesn't depend on what's been synced. A missing
// dir has no specs. It needs nvim on PATH
func Load(shipped bool, dirs ...string) (*Set, error) {
	bin, err := exec.LookPath(nvimBin)
	if err != nil {
		return nil, fmt.Errorf("reading language specs needs Neovim: %w", err)
	}

	tmp, err := os.MkdirTemp("", "bud-lang-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	// the engine and the dump script come from this binary, never a synced copy
	cfg := bud.Config()
	if err := extract(cfg, tmp, "lua/bud/lang/init.lua", "lua/bud/lang/dump.lua"); err != nil {
		return nil, err
	}
	if shipped {
		files, err := fs.Glob(cfg, "languages/*.lua")
		if err != nil {
			return nil, err
		}
		if err := extract(cfg, tmp, files...); err != nil {
			return nil, err
		}
		dirs = append([]string{filepath.Join(tmp, "languages")}, dirs...)
	}

	out := filepath.Join(tmp, "specs.json")
	args := []string{"--clean", "--headless", "-l", filepath.Join(tmp, "lua/bud/lang/dump.lua"), tmp, out}
	cmd := exec.Command(bin, append(args, dirs...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("reading language specs: %w: %s", err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("reading language specs: %w", err)
	}
	var set Set
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("reading language specs: %w", err)
	}

	return &set, nil
}

// extract copies files from src into dir at the same paths
func extract(src fs.FS, dir string, files ...string) error {
	for _, file := range files {
		data, err := fs.ReadFile(src, file)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}

	return nil
}

// Names lists the specs that loaded, by name
func (s *Set) Names() []string {
	names := make([]string, 0, len(s.Specs))
	for _, spec := range s.Specs {
		names = append(names, spec.Name)
	}

	return names
}

func (s *Set) Get(name string) (Spec, bool) {
	i := slices.IndexFunc(s.Specs, func(spec Spec) bool { return spec.Name == name })
	if i < 0 {
		return Spec{}, false
	}

	return s.Specs[i], true
}

// Preset is the languages a profile enables when config.json lists none, the
// same rule bud.lang uses: every spec for full, the minimal ones for minimal
func (s *Set) Preset(profile string) []string {
	var names []string
	for _, spec := range s.Specs {
		if profile != "minimal" || spec.Minimal {
			names = append(names, spec.Name)
		}
	}

	return names
}

// Enabled returns the specs named by languages (or, when it's nil, by the
// profile's preset) in that order, and the names with no spec. A spec that
// failed to load is neither: Errors already reports it
func (s *Set) Enabled(languages []string, profile string) (specs []Spec, unknown []string) {
	if languages == nil {
		languages = s.Preset(profile)
	}
	for _, name := range languages {
		if spec, ok := s.Get(name); ok {
			specs = append(specs, spec)
		} else if _, failed := s.Errors[name]; !failed {
			unknown = append(unknown, name)
		}
	}

	return specs, unknown
}

// System lists the binaries specs need from the system package manager,
// without repeats, in the specs' order
func System(specs []Spec) []string {
	var bins []string
	for _, spec := range specs {
		for _, bin := range spec.System {
			if !slices.Contains(bins, bin) {
				bins = append(bins, bin)
			}
		}
	}

	return bins
}

// File is where a spec named name lives inside a config dir like ~/.config/bud,
// as a slash path, the way the sync manifest records it
func File(name string) string {
	return path.Join("languages", name+".lua")
}
