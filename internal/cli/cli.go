// Package cli provides CLI functions to run commands and invoke/process the CLI
package cli

import (
	"errors"
	"flag"
	"os"
)

type Command struct {
	Name  string
	Flags func(fs *flag.FlagSet)
	Run   func(args []string) error
}

type App struct {
	Cmds map[string]*Command
	Args []string
}

func NewApp() *App {
	args := os.Args[1:]

	return &App{
		Args: args,
	}
}

func (a *App) Run() error {
	global := flag.NewFlagSet("bud", flag.ContinueOnError)
	if err := global.Parse(a.Args); err != nil {
		return err
	}

	a.Args = global.Args()

	// default to open if no args provided
	name := "open"
	if len(a.Args) > 0 {
		name = a.Args[0]
		a.Args = a.Args[1:]
	}

	cmd, ok := a.Cmds[name]
	if !ok {
		return errors.New("command not found")
	}

	if err := cmd.Run(a.Args); err != nil {
		return err
	}

	return nil
}
