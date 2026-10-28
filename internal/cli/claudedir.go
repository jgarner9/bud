package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/paths"
)

// claudeConfigDir is the Claude Code config dir bud uses (JGA-135, D73):
// bud's own, so its kit, plugins, settings and login stay apart from the
// claude you run yourself, or "" for Claude Code's usual ~/.claude
func claudeConfigDir(p *paths.Paths, s config.Settings) string {
	if s.AI.Claude.Config == "home" {
		return ""
	}
	return p.ClaudeConfig()
}

// UseClaudeConfig points everything this bud runs at its Claude Code config
// dir by setting CLAUDE_CONFIG_DIR: claude itself, the kit sync, and nvim,
// whose sessions, chat history and IDE bridge read it too. A
// CLAUDE_CONFIG_DIR you set yourself wins. main calls it, so tests never do
func UseClaudeConfig() {
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return
	}
	p, err := paths.Resolve()
	if err != nil {
		return
	}
	s := config.Defaults()
	if f, err := config.Load(p.MachineConfig()); err == nil {
		if loaded, err := f.Settings(); err == nil {
			s = loaded
		}
	}
	dir := claudeConfigDir(p, s)
	if dir == "" || os.MkdirAll(dir, 0o700) != nil {
		return
	}
	os.Setenv("CLAUDE_CONFIG_DIR", dir)
}

// homeClaudeDir is where Claude Code keeps its config without
// CLAUDE_CONFIG_DIR
func homeClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// withClaudeDir runs fn with CLAUDE_CONFIG_DIR pointing at dir, or unset for
// ~/.claude, so the claude it runs works on that config
func withClaudeDir(dir string, fn func() error) error {
	home, err := homeClaudeDir()
	if err != nil {
		return err
	}
	orig, had := os.LookupEnv("CLAUDE_CONFIG_DIR")
	defer func() {
		if had {
			os.Setenv("CLAUDE_CONFIG_DIR", orig)
		} else {
			os.Unsetenv("CLAUDE_CONFIG_DIR")
		}
	}()
	if dir == home {
		os.Unsetenv("CLAUDE_CONFIG_DIR")
	} else {
		os.Setenv("CLAUDE_CONFIG_DIR", dir)
	}

	return fn()
}

// moveKit takes what the kit put into another Claude Code config dir back out
// before the kit syncs into this one (JGA-135): the ledger says where it last
// synced. It waits for a login there, lists what it removes and asks first. A no leaves those in place
// and forgets them; with nobody to ask it returns false and nothing changes,
// so a sync on a terminal does it later. A dry run only lists them
func moveKit(pr *prompter, env kit.Env, dryRun bool) (bool, error) {
	ledger, ok, err := kit.ReadLedger(env.Ledger)
	if err != nil || !ok {
		return err == nil, err
	}
	old := ledger.Dir
	if old == "" {
		if old, err = homeClaudeDir(); err != nil {
			return false, err
		}
	}
	if old == env.ClaudeDir {
		return true, nil
	}
	// the claude.ai connectors that stand in for some entries are listed only
	// once this config is logged in; without them the sync would add duplicates
	if s, err := checkClaude(); err != nil || s.Bin == "" || !s.LoggedIn {
		fmt.Printf("bud now keeps its own Claude Code config in %s: log in there with 'bud claude setup', then run 'bud kit sync'.\n", env.ClaudeDir)
		return false, err
	}
	oldEnv := env
	oldEnv.ClaudeDir, oldEnv.ClaudeJSON = old, claudeJSONFor(old)
	var actions []kit.Action
	err = withClaudeDir(old, func() (err error) {
		actions, err = kit.Unsync(oldEnv, true)
		return err
	})
	if err != nil {
		return false, err
	}
	var gone []kit.Action
	for _, a := range actions {
		if a.Do == "remove" {
			gone = append(gone, a)
		}
	}
	if len(gone) == 0 {
		if !dryRun {
			os.Remove(env.Ledger)
		}
		return true, nil
	}
	fmt.Printf("bud now keeps its own Claude Code config in %s. The kit synced these into %s:\n", env.ClaudeDir, old)
	for _, a := range gone {
		fmt.Printf("  %s %s\n", a.Kind, a.Name)
	}
	if dryRun {
		fmt.Println("bud kit sync removes them from there.")
		return true, nil
	}
	if pr == nil {
		fmt.Println("Run 'bud kit sync' on a terminal to remove them there and sync the kit here.")
		return false, nil
	}
	yes, err := pr.confirm(fmt.Sprintf("Remove them from %s?", old))
	if err != nil {
		return false, err
	}
	if !yes {
		fmt.Printf("Left in %s; they're yours now.\n", old)
		return true, os.Remove(env.Ledger)
	}
	err = withClaudeDir(old, func() error {
		_, err := kit.Unsync(oldEnv, false)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("removing the kit from %s: %w", old, err)
	}

	return true, nil
}

// claudeJSONFor is where Claude Code keeps user-scope MCP servers for a
// config dir: ~/.claude.json for ~/.claude, else inside the dir
func claudeJSONFor(dir string) string {
	if home, err := homeClaudeDir(); err == nil && dir == home {
		return filepath.Join(filepath.Dir(home), ".claude.json")
	}
	return filepath.Join(dir, ".claude.json")
}
