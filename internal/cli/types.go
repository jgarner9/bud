package cli

import (
	"flag"
	"io"
)

// Workspace is where nvim runs and what it opens
type Workspace struct {
	Root  string   // project root, nvim's working directory
	Files []string // absolute paths, one buffer each; none opens the root itself
}

type Command struct {
	Name  string
	Usage string
	Flags func(fs *flag.FlagSet)
	Run   func(g *Globals, args []string) error
	Subs  []*Command
}

// Globals are the flags that go before the command (D4)
type Globals struct {
	Verbose  bool
	NoUpdate bool   // skip the boot update check (D9)
	Profile  string // "full" or "minimal", for install
	Layout   string // pane layout for open

	stderr io.Writer // where Logf writes; nil means os.Stderr
}

type App struct {
	Cmds map[string]*Command
	Args []string

	// Stdout gets requested help, Stderr gets usage errors; nil means os.Stdout/os.Stderr
	Stdout io.Writer
	Stderr io.Writer
}
