package cli

import (
	"fmt"
	"os"

	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Dev())
}

// Dev is for working on bud itself: link points ~/.config/bud at a checkout so
// Lua edits show up on the next nvim start, without a release (D5)
func Dev() *Command {
	return &Command{
		Name:  "dev",
		Usage: "Points the Neovim config at a bud checkout, for working on bud",
		Subs: []*Command{
			{
				Name:  "link",
				Usage: "link ~/.config/bud to a checkout (default: the current dir)",
				Run:   func(_ *Globals, args []string) error { return devLink(args) },
			},
			{
				Name:  "unlink",
				Usage: "go back to the config this bud ships",
				Run:   func(_ *Globals, args []string) error { return devUnlink(args) },
			},
		},
	}
}

// swappable in tests
var applyDevConfig = nvim.Apply

func devLink(args []string) error {
	if len(args) > 1 {
		return usageErrorf("dev link takes at most one checkout path")
	}
	checkout := "."
	if len(args) == 1 {
		checkout = args[0]
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	if err := nvim.Link(checkout, p.Config); err != nil {
		return err
	}
	linked, _ := nvim.DevLink(p.Config)
	fmt.Printf("Linked %s to %s. bud update leaves it alone until bud dev unlink.\n", p.Config, linked)
	fmt.Println("Restoring plugins from the checkout's lockfile...")

	return restorePlugins()
}

func devUnlink(args []string) error {
	if len(args) > 0 {
		return usageErrorf("dev unlink takes no arguments")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	checkout, err := nvim.Unlink(p.Config)
	if err != nil {
		return err
	}
	fmt.Printf("Unlinked %s from %s.\n", p.Config, checkout)
	// the synced copy comes back, plugins included
	if err := os.MkdirAll(p.Config, 0o755); err != nil {
		return err
	}

	return applyDevConfig()
}
