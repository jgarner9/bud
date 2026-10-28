package main

import (
	"os"

	"jgarner.dev/bud/internal/cli"
)

func main() {
	cli.UseClaudeConfig()
	app := cli.NewApp()
	os.Exit(cli.ExitCode(app.Run(), os.Stderr))
}
