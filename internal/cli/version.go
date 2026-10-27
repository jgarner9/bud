package cli

import (
	"fmt"

	"jgarner.dev/bud/internal/version"
)

func init() {
	RegisterCommand(Version())
}

func Version() *Command {
	return &Command{
		Name:  "version",
		Usage: "Prints the bud version",
		Run:   printVersion,
	}
}

func printVersion(g *Globals, args []string) error {
	if len(args) > 0 {
		return usageErrorf("version takes no arguments")
	}

	fmt.Println("bud " + version.String())
	return nil
}
