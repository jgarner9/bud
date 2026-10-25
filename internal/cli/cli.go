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

	commandRegistry := map[string]*Command{
		"open":    Open(),
		"install": Install(),
		"update":  Update(),
		"chat":    Chat(),
	}

	return &App{
		Cmds: commandRegistry,
		Args: args,
	}
}

func (a *App) Run() error {
	// app args reference
	args := &a.Args

	global := flag.NewFlagSet("bud", flag.ContinueOnError)
	if err := global.Parse(*args); err != nil {
		return err
	}
	*args = global.Args()

	// default to open if no args provided
	name := "open"
	if len(*args) > 0 {
		name = (*args)[0]
		*args = (*args)[1:]
	}

	cmd, ok := a.Cmds[name]
	if !ok {
		return errors.New("command not found")
	}

	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	if err := fs.Parse(*args); err != nil {
		return err
	}

	if err := cmd.Run(fs.Args()); err != nil {
		return err
	}

	return nil
}
