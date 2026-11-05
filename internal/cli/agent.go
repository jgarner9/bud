package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
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
var execClaude = func(dir string, argv, env []string) error {
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return syscall.Exec(argv[0], argv, env)
}

// agentEnv is the environment Claude Code's TUI gets: this one, with
// BUD_BIN pointing at this bud, as nvim's sessions have it, so bud-ide's
// SessionStart hook knows the session is bud's and adds its notes on
// working inside bud (D77)
func agentEnv() []string {
	env := os.Environ()
	self, err := os.Executable()
	if err != nil {
		return env
	}
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "BUD_BIN=") })

	return append(env, "BUD_BIN="+self)
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

	s := machineSettings()
	var flags []string
	if s.AI.Claude.Resume == "continue" {
		flags = append(flags, "--continue")
	}
	argv, err := tuiArgv(s, bin, flags)
	if err != nil {
		return err
	}
	g.Logf("running %q in %s", argv, ws.Root)

	return execClaude(ws.Root, argv, agentEnv())
}

// machineSettings is config.json's settings, the defaults when it's missing
// or broken
func machineSettings() config.Settings {
	s := config.Defaults()
	if p, err := paths.Resolve(); err == nil {
		if f, err := config.Load(p.MachineConfig()); err == nil {
			if loaded, err := f.Settings(); err == nil {
				s = loaded
			}
		}
	}
	return s
}

// tuiArgv is the command line for Claude Code's TUI, as nvim builds it: bin,
// or ai.claude.command in its place, then flags, then ai.claude.args
func tuiArgv(s config.Settings, bin string, flags []string) ([]string, error) {
	argv := []string{bin}
	if len(s.AI.Claude.Command) > 0 {
		wrapper, err := exec.LookPath(s.AI.Claude.Command[0])
		if err != nil {
			return nil, fmt.Errorf("ai.claude.command: %w", err)
		}
		argv = append([]string{wrapper}, s.AI.Claude.Command[1:]...)
	}
	argv = append(argv, flags...)

	return append(argv, s.AI.Claude.Args...), nil
}
