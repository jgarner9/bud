package cli

import (
	"fmt"

	models "jgarner.dev/bud/internal/registry"
)

func init() {
	RegisterCommand(Registry())
}

// Registry checks the provider and model registry (Suite D46, D58), the file
// bud sync renders every runtime's config from
func Registry() *Command {
	return &Command{
		Name:  "registry",
		Usage: "Checks the provider and model registry",
		Subs: []*Command{
			{
				Name:  "check",
				Usage: "validate a registry, with an overlay merged on top: check <registry.jsonc> [overlay.jsonc]",
				Run:   func(_ *Globals, args []string) error { return registryCheck(args) },
			},
		},
	}
}

func registryCheck(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return usageErrorf("registry check takes the registry file and, optionally, an overlay's")
	}
	r, err := models.Read(args[0])
	if err != nil {
		return err
	}
	name := args[0]
	if len(args) == 2 {
		overlay, err := models.Read(args[1])
		if err != nil {
			return err
		}
		if r, err = models.Merge(r, overlay); err != nil {
			return err
		}
		name += " with " + args[1] + " over it"
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	fmt.Printf("%s: %d providers, %d models, all %d roles filled\n", name, len(r.Providers), len(r.Models), len(models.Roles))
	return nil
}
