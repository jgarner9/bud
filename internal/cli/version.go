package cli

import (
	"fmt"

	"jgarner.dev/bud/internal/version"
)

func init() {
	RegisterCommand(Version())
}

func Version() *Command {
	cmd := &Command{
		Name:  "version",
		Usage: "Prints the bud version",
		Flags: nil,
		Run:   printVersion,
		Subs:  nil,
	}

	return cmd
}

func printVersion(args []string) error {
	if len(args) > 0 {
		return usageErrorf("version takes no arguments")
	}

	fmt.Println("bud " + version.String())
	return nil
}
