package cli

import (
	"errors"
	"os"
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
	return syscall.Exec(argv[0], argv, os.Environ())
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
	if s.AI.Claude.Resume == "continue" {
		argv = append(argv, "--continue")
	}
	argv = append(argv, s.AI.Claude.Args...)
	g.Logf("running %q in %s", argv, ws.Root)

	return execClaude(ws.Root, argv)
}
