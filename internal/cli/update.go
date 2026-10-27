package cli

import (
	"jgarner.dev/bud/internal/nvim"
)

func init() {
	RegisterCommand(Update())
}

func Update() *Command {
	return &Command{
		Name:  "update",
		Usage: "Syncs the Bud Neovim config and restores pinned plugins",
		Run:   update,
	}
}

func update(g *Globals, args []string) error {
	if len(args) > 0 {
		return usageErrorf("update takes no arguments")
	}

	return nvim.Apply()
}
