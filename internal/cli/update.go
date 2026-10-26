package cli

import (
	"jgarner.dev/bud/internal/nvim"
)

func init() {
	RegisterCommand(Update())
}

func Update() *Command {
	cmd := &Command{
		Name:  "update",
		Usage: "Syncs the Bud Neovim config and restores pinned plugins",
		Flags: nil,
		Run:   update,
		Subs:  nil,
	}

	return cmd
}

func update(args []string) error {
	return nvim.Apply()
}
