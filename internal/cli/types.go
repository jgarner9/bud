package cli

import (
	"flag"
	"io"
)

type Workspace struct {
	Root string
	File string
}

type Command struct {
	Name  string
	Usage string
	Flags func(fs *flag.FlagSet)
	Run   func(args []string) error
	Subs  []*Command
}

type App struct {
	Cmds map[string]*Command
	Args []string

	// Stdout gets requested help, Stderr gets usage errors; nil means os.Stdout/os.Stderr
	Stdout io.Writer
	Stderr io.Writer
}
