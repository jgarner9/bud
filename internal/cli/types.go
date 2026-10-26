package cli

import "flag"

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
}
