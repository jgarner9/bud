package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Agent())
}

// Agent opens Claude Code's own TUI in a project from the shell, outside
// Neovim, on the same conversation the editor's session uses (BASE.md 4.2)
func Agent() *Command {
	return &Command{
		Name:  "agent",
		Usage: "Opens Claude Code in a project, outside the editor: agent [path] (default: the current dir)",
		Run:   agent,
	}
}

// swappable in tests
var execClaude = func(dir string, argv []string) error {
	if err := os.Chdir(dir); err != nil {
		return err
	}
	vars := os.Environ()
	// the kit's MCP servers read their secrets from the environment (D15)
	if extra, err := keychainEnv(); err == nil {
		for k, v := range extra {
			vars = append(vars, k+"="+v)
		}
	}
	return syscall.Exec(argv[0], argv, vars)
}

func agent(g *Globals, args []string) error {
	if len(args) > 1 {
		return usageErrorf("agent takes at most one path")
	}
	ws, err := resolveWorkspace(args)
	if err != nil {
		return err
	}
	bin := claude.Find()
	if bin == "" {
		return errors.New("Claude Code isn't installed: run bud claude setup")
	}

	s := config.Defaults()
	if p, err := paths.Resolve(); err == nil {
		if f, err := config.Load(p.MachineConfig()); err == nil {
			if loaded, err := f.Settings(); err == nil {
				s = loaded
			}
		}
	}
	argv := []string{bin}
	if len(s.AI.Claude.Command) > 0 {
		wrapper, err := exec.LookPath(s.AI.Claude.Command[0])
		if err != nil {
			return fmt.Errorf("ai.claude.command: %w", err)
		}
		argv = append([]string{wrapper}, s.AI.Claude.Command[1:]...)
	}
	if s.AI.Claude.Resume == "continue" {
		argv = append(argv, "--continue")
	}
	argv = append(argv, s.AI.Claude.Args...)
	g.Logf("running %q in %s", argv, ws.Root)

	return execClaude(ws.Root, argv)
}
