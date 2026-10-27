package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/launcher"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Launch())
}

// Launch opens bud in a new terminal window (D11), which is what the app
// bundles run: bud launch [--terminal kitty] [--layout name] [path ...]
func Launch() *Command {
	var terminal, layout string
	return &Command{
		Name:  "launch",
		Usage: "Opens bud in a new terminal window: launch [--terminal kitty] [--layout name] [path ...] (no path: the dashboard)",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&terminal, "terminal", "", "terminal to open: "+strings.Join(config.Terminals, " or ")+" (default: terminal in config.json, else the first installed)")
			fs.StringVar(&layout, "layout", "", "pane layout to open the editor with (same as the global --layout)")
		},
		Run: func(g *Globals, args []string) error { return launch(g, args, terminal, layout) },
	}
}

// swappable in tests
var (
	launchWindow    = launcher.Launch
	launchTerminals = installer.DetectTerminals
)

func launch(g *Globals, args []string, terminal, layout string) error {
	if layout == "" {
		layout = g.Layout
	}
	// everything that can be wrong is reported here, not in a window that
	// flashes up and closes
	ws, err := resolveWorkspace(args)
	if err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if layout != "" {
		if err := checkLayout(p, layout); err != nil {
			return err
		}
	}
	name, err := pickTerminal(p, terminal)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}

	w := launcher.Window{Dir: ws.Root, Title: "bud", Argv: launchArgv(self, g, layout, args)}
	g.Logf("launching %s in %s: %s", name, w.Dir, strings.Join(w.Argv, " "))

	return launchWindow(name, w)
}

// pickTerminal is --terminal, else config.json's terminal, else the first one
// installed: the default (kitty, D11) only stands in when none of them is
func pickTerminal(p *paths.Paths, flagged string) (string, error) {
	if flagged != "" {
		if !slices.Contains(config.Terminals, flagged) {
			return "", usageErrorf("--terminal %q is not one of %s", flagged, strings.Join(config.Terminals, ", "))
		}
		return flagged, nil
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return "", err
	}
	s, err := f.Settings()
	if err != nil {
		return "", err
	}
	if _, set := f.Get("terminal"); !set {
		if found := launchTerminals(); len(found) > 0 {
			return found[0], nil
		}
		return "", fmt.Errorf("no terminal bud can launch is installed: install %s", strings.Join(config.Terminals, " or "))
	}

	return s.Terminal, nil
}

// launchArgv is what runs in the window: this bud opening the same paths.
// A terminal started by macOS's open doesn't inherit this environment, so
// PATH goes along explicitly, or nvim and the tools it runs wouldn't be found
func launchArgv(self string, g *Globals, layout string, args []string) []string {
	argv := []string{"/usr/bin/env", "PATH=" + os.Getenv("PATH"), self}
	if g.Verbose {
		argv = append(argv, "--verbose")
	}
	if g.NoUpdate {
		argv = append(argv, "--no-update")
	}
	if layout != "" {
		argv = append(argv, "--layout", layout)
	}
	// open's own command name, so a path called "update" is still a path
	argv = append(argv, "open")
	for _, arg := range args {
		// resolveWorkspace already checked each one
		abs, _ := filepath.Abs(arg)
		argv = append(argv, abs)
	}

	return argv
}
