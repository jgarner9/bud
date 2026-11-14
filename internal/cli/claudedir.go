package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/paths"
)

// claudeConfigDir is the Claude Code config dir bud uses: "" for Claude
// Code's usual ~/.claude, which bud shares by default (D77), or with
// ai.claude.config "bud" its own (JGA-135, D73), so its plugins,
// settings and login stay apart from the claude you run yourself
func claudeConfigDir(p *paths.Paths, s config.Settings) string {
	if s.AI.Claude.Config == "bud" {
		return p.ClaudeConfig()
	}
	return ""
}

// UseClaudeConfig points everything this bud runs at its Claude Code config
// dir by setting CLAUDE_CONFIG_DIR: claude itself and nvim,
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

// UseManagedBin puts the tools bud downloaded (D107) ahead of the system's on
// PATH, for bud and what it starts. main calls it, so tests never do
func UseManagedBin() {
	if p, err := paths.Resolve(); err == nil {
		installer.UseManagedBin(p.Data)
	}
}

// budClaudeDir says whether CLAUDE_CONFIG_DIR is bud's own dir, which
// UseClaudeConfig set, rather than one you set yourself
func budClaudeDir() bool {
	p, err := paths.Resolve()
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	return err == nil && dir != "" && filepath.Clean(dir) == filepath.Clean(p.ClaudeConfig())
}

// shareClaudeDir offers to switch bud to ~/.claude when that runs Claude Code
// on a cloud provider bud's own dir doesn't see (JGA-146). On a yes it unsets
// ai.claude.config, whose default is "home", and points this process at
// ~/.claude, so the rest of the command sees it. A CLAUDE_CONFIG_DIR you set
// yourself is left alone
var shareClaudeDir = func(pr *prompter, yes bool, provider string) (bool, error) {
	// with nobody to ask, the caller's hint says it all
	if !budClaudeDir() || (!yes && pr == nil) {
		return false, nil
	}
	fmt.Printf("~/.claude/settings.json runs Claude Code on %s, which bud's own config dir doesn't see.\n", provider)
	fmt.Println("bud can share ~/.claude instead, with the Claude Code setup you already have there.")
	if !yes {
		ok, err := pr.confirmYes("Share ~/.claude with bud?")
		if err != nil || !ok {
			return false, err
		}
	}
	p, err := paths.Resolve()
	if err != nil {
		return false, err
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return false, err
	}
	f.Unset("ai.claude.config")
	if _, err := f.Settings(); err != nil {
		return false, err
	}
	if err := f.Save(); err != nil {
		return false, err
	}
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	fmt.Println("bud now shares ~/.claude; 'bud config set ai.claude.config bud' gives it its own again.")

	return true, nil
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
