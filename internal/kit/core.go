package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Core is bud-plugins' core.md (JGA-179): the always-on instructions that
// claude.ai's personal preferences (and Desktop's organization instructions
// on a cloud provider) hold a pasted copy of, so bud says when that copy is
// out of date
type Core struct {
	Path   string // the file in the marketplace's checkout; "" when there's none
	Text   string
	Hash   string
	Pasted bool // its hash is the one recorded when you last pasted it
}

// coreState is what bud kit core --pasted records
type coreState struct {
	Pasted string `json:"pasted"` // hash of the core.md you last pasted
}

// ReadCore finds the kit's core ("marketplace/path in it") in the checkout
// Claude Code keeps of that marketplace (installLocation in
// plugins/known_marketplaces.json, from bud-plugins whoever added it), and
// says whether it's the one recorded in statePath. Only files are read, so
// doctor's quick checks can use it. A Core with no Path means there's no
// checkout (yet)
func ReadCore(core, claudeDir, statePath string) (Core, error) {
	market, rel, ok := strings.Cut(core, "/")
	if !ok || market == "" || rel == "" {
		return Core{}, nil
	}
	data, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "known_marketplaces.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Core{}, nil
	}
	if err != nil {
		return Core{}, err
	}
	var known map[string]struct {
		InstallLocation string `json:"installLocation"`
	}
	if err := json.Unmarshal(data, &known); err != nil {
		return Core{}, fmt.Errorf("known_marketplaces.json: %w", err)
	}
	loc := known[market].InstallLocation
	if loc == "" {
		return Core{}, nil
	}
	p := filepath.Join(loc, filepath.FromSlash(rel))
	text, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return Core{}, nil
	}
	if err != nil {
		return Core{}, err
	}
	c := Core{Path: p, Text: string(text), Hash: hash(text)}
	var st coreState
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	c.Pasted = st.Pasted == c.Hash

	return c, nil
}

// MarkCorePasted records c as the core.md you pasted
func MarkCorePasted(statePath string, c Core) error {
	data, err := json.MarshalIndent(coreState{Pasted: c.Hash}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}

	return atomicfile.WriteFile(statePath, append(data, '\n'), 0o644)
}

// CoreReminder is what bud says when core.md changed since you last pasted it
const CoreReminder = "core.md changed since you last pasted it: paste it into claude.ai (Settings, Profile, personal preferences; on a cloud provider, Desktop's organization instructions) with 'bud kit core | pbcopy', then run 'bud kit core --pasted'"
