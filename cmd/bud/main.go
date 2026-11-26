package main

import (
	"os"

	"jgarner.dev/bud/internal/cli"
)

func main() {
	cli.UseManagedBin()
	app := cli.NewApp()
	os.Exit(cli.ExitCode(app.Run(), os.Stderr))
}
