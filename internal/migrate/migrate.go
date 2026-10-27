// Package migrate runs the numbered Lua scripts in migrations/ once each, in
// order, when a release changes layout or paths (BASE.md 5.7 step 3)
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/paths"
)

// a migration is NNNN_name.lua
var namePattern = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.lua$`)

// state is migrations.json
type state struct {
	Applied []string `json:"applied"`
}

// List returns the migrations in src by name without ".lua", in run order
func List(src fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		if !namePattern.MatchString(e.Name()) {
			return nil, fmt.Errorf("migration %s isn't named NNNN_name.lua", e.Name())
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".lua"))
	}
	slices.Sort(names)

	return names, nil
}

// Runner runs one migration's script
type Runner func(name string, script []byte) error

// Run runs every migration in src not yet recorded in statePath, in order,
// recording each as it succeeds. The first failure stops it, so that one and
// the rest run next time. It returns the names it ran
func Run(src fs.FS, statePath string, run Runner) ([]string, error) {
	names, err := List(src)
	if err != nil {
		return nil, err
	}
	st, err := load(statePath)
	if err != nil {
		return nil, err
	}

	var ran []string
	for _, name := range names {
		if slices.Contains(st.Applied, name) {
			continue
		}
		script, err := fs.ReadFile(src, name+".lua")
		if err != nil {
			return ran, err
		}
		if err := run(name, script); err != nil {
			return ran, fmt.Errorf("migration %s: %w", name, err)
		}
		st.Applied = append(st.Applied, name)
		if err := save(statePath, st); err != nil {
			return ran, err
		}
		ran = append(ran, name)
	}

	return ran, nil
}

// MarkAll records every migration in src as done without running it, for a
// fresh install that has nothing to migrate
func MarkAll(src fs.FS, statePath string) error {
	names, err := List(src)
	if err != nil {
		return err
	}
	st, err := load(statePath)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !slices.Contains(st.Applied, name) {
			st.Applied = append(st.Applied, name)
		}
	}

	return save(statePath, st)
}

func load(path string) (*state, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &state{}, nil
	}
	if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &st, nil
}

func save(path string, st *state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return atomicfile.WriteFile(path, append(data, '\n'), 0o644)
}

// Nvim runs a migration with `nvim --clean --headless -l`, with bud's dirs in
// BUD_CONFIG_DIR, BUD_DATA_DIR, BUD_STATE_DIR and BUD_CACHE_DIR
func Nvim(p *paths.Paths) Runner {
	return func(name string, script []byte) error {
		bin, err := exec.LookPath("nvim")
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp("", "bud-migrate-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		file := filepath.Join(dir, name+".lua")
		if err := os.WriteFile(file, script, 0o644); err != nil {
			return err
		}

		cmd := exec.Command(bin, "--clean", "--headless", "-l", file)
		cmd.Env = append(os.Environ(),
			"BUD_CONFIG_DIR="+p.Config,
			"BUD_DATA_DIR="+p.Data,
			"BUD_STATE_DIR="+p.State,
			"BUD_CACHE_DIR="+p.Cache,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return cmd.Run()
	}
}
