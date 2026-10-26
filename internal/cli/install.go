package cli

import (
	"fmt"

	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/nvim"
)

func init() {
	RegisterCommand(Install())
}

func Install() *Command {
	cmd := &Command{
		Name:  "install",
		Usage: "Installs system dependencies and the Bud Neovim config",
		Flags: nil,
		Run:   install,
		Subs:  nil,
	}

	return cmd
}

func install(args []string) error {
	for _, dep := range installer.Deps {
		status := "missing"
		if dep.Installed() {
			status = "ok"
		}
		fmt.Printf("%-8s %s\n", dep.Name, status)
	}

	if err := installer.EnsureAll(); err != nil {
		return err
	}

	return nvim.Apply()
}
