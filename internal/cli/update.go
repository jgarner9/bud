package cli

import (
	"fmt"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/nvim"
)

func init() {
	RegisterCommand(Update())
}

func Update() *Command {
	cmd := &Command{
		Name:  "update",
		Usage: "Opens Bud chat interface",
		Flags: nil,
		Run:   update,
		Subs:  nil,
	}

	return cmd
}

func update(args []string) error {
	dst, _ := nvim.ConfigDir()
	res, err := nvim.SyncConfig(bud.NvimConfig(), dst)
	if err != nil {
		return err
	}

	fmt.Println("Added:")
	for _, file := range res.Added {
		fmt.Println(file)
	}
	fmt.Println("Updated:")
	for file := range res.Updated {
		fmt.Println(file)
	}
	fmt.Println("Removed:")
	for file := range res.Removed {
		fmt.Println(file)
	}

	if err := nvim.RestorePlugins(); err != nil {
		return err
	}

	return nil
}
