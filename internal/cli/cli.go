// Package cli provides CLI functions to run commands and invoke/process the CLI
package cli

import (
	"flag"
	"os"
)

var CommandRegistry = map[string]*Command{}

func RegisterCommand(cmd *Command) {
	if _, exists := CommandRegistry[cmd.Name]; exists {
		panic("panic: " + cmd.Name + " command registered twice")
	}
	CommandRegistry[cmd.Name] = cmd
}

func NewApp() *App {
	args := os.Args[1:]

	return &App{
		Cmds: CommandRegistry,
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

	// default to open when no command is given, or when the first arg isn't a known command
	cmd := a.Cmds["open"]
	if len(*args) > 0 {
		if c, ok := a.Cmds[(*args)[0]]; ok {
			cmd = c
			*args = (*args)[1:]
		}
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
