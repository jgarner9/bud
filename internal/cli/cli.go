// Package cli provides CLI functions to run commands and invoke/process the CLI
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/config"
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

	g := &Globals{stderr: a.stderr()}
	global := flag.NewFlagSet("bud", flag.ContinueOnError)
	g.flags(global)
	global.Usage = func() { a.globalUsage(global) }
	if err := a.parse(global, *args); err != nil {
		return err
	}
	*args = global.Args()

	if g.Profile != "" && !slices.Contains(config.Profiles, g.Profile) {
		return usageErrorf("--profile must be one of %s, not %q", strings.Join(config.Profiles, ", "), g.Profile)
	}

	// default to open when no command is given, or when the first arg isn't a known command
	cmd := a.Cmds["open"]
	if len(*args) > 0 {
		if c, ok := a.Cmds[(*args)[0]]; ok {
			cmd = c
			*args = (*args)[1:]
		}
	}

	if g.Layout != "" && cmd.Name != "open" {
		return usageErrorf("--layout only applies when opening a project or files: bud --layout %s <path>", g.Layout)
	}

	// a command with Subs takes one next, as in bud lang enable go
	name := cmd.Name
	for len(cmd.Subs) > 0 && len(*args) > 0 {
		i := slices.IndexFunc(cmd.Subs, func(sub *Command) bool { return sub.Name == (*args)[0] })
		if i < 0 {
			break
		}
		cmd = cmd.Subs[i]
		name += " " + cmd.Name
		*args = (*args)[1:]
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	fs.Usage = func() { commandUsage(fs, name, cmd) }
	if err := a.parse(fs, *args); err != nil {
		return err
	}
	if cmd.Run == nil {
		// a command that's only a group of subcommands
		if len(fs.Args()) > 0 {
			return usageErrorf("unknown %s command %q (one of: %s)", name, fs.Arg(0), subNames(cmd))
		}
		return usageErrorf("%s needs a command: %s", name, subNames(cmd))
	}

	if err := cmd.Run(g, fs.Args()); err != nil {
		return err
	}

	return nil
}

func (g *Globals) flags(fs *flag.FlagSet) {
	fs.BoolVar(&g.Verbose, "verbose", false, "log what bud is doing on stderr")
	fs.BoolVar(&g.NoUpdate, "no-update", false, "skip the background update check at boot")
	fs.StringVar(&g.Profile, "profile", "", "install profile: "+strings.Join(config.Profiles, " or "))
	fs.StringVar(&g.Layout, "layout", "", "pane layout to open the editor with")
}

// Logf writes one line to stderr when --verbose is on
func (g *Globals) Logf(format string, a ...any) {
	if !g.Verbose {
		return
	}
	w := g.stderr
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintln(w, paint(w, ansiDim, fmt.Sprintf("bud: "+format, a...)))
}

// isGlobalFlag reports whether arg, like "--verbose" or "-layout=focus", names a global flag
func isGlobalFlag(arg string) bool {
	name, ok := flagName(arg)
	if !ok {
		return false
	}
	fs := flag.NewFlagSet("bud", flag.ContinueOnError)
	(&Globals{}).flags(fs)

	return fs.Lookup(name) != nil
}

func flagName(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "-") {
		return "", false
	}
	name := strings.TrimLeft(arg, "-")
	name, _, _ = strings.Cut(name, "=")

	return name, name != ""
}

func (a *App) globalUsage(fs *flag.FlagSet) {
	w := fs.Output()
	fmt.Fprintln(w, "Usage: bud [global flags] <command> [command flags] [args]")
	fmt.Fprintln(w, "       bud [global flags] [path ...]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")

	names := make([]string, 0, len(a.Cmds))
	for name := range a.Cmds {
		// __complete is for the shell scripts, not for people
		if !strings.HasPrefix(name, "__") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-10s %s\n", name, a.Cmds[name].Usage)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags (they go before the command):")
	fs.PrintDefaults()
}

func commandUsage(fs *flag.FlagSet, name string, cmd *Command) {
	w := fs.Output()
	if len(cmd.Subs) > 0 && cmd.Run == nil {
		fmt.Fprintf(w, "Usage: bud [global flags] %s <command> [flags] [args]\n", name)
	} else {
		fmt.Fprintf(w, "Usage: bud [global flags] %s [flags] [args]\n", name)
	}
	if cmd.Usage != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, cmd.Usage)
	}
	if len(cmd.Subs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Commands:")
		for _, sub := range cmd.Subs {
			fmt.Fprintf(w, "  %-10s %s\n", sub.Name, sub.Usage)
		}
	}

	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if hasFlags {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		fs.PrintDefaults()
	}
}

func subNames(cmd *Command) string {
	var names []string
	for _, sub := range cmd.Subs {
		names = append(names, sub.Name)
	}

	return strings.Join(names, ", ")
}

// parse runs fs.Parse and prints the usage itself: on stdout when help was asked
// for, on stderr after the error when the flags were wrong. ErrHelp passes through
// so callers can tell a help request from a mistake.
func (a *App) parse(fs *flag.FlagSet, args []string) error {
	// the flag package writes help and errors to one output, so silence it and split them here
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		fs.SetOutput(a.stdout())
		fs.Usage()
		return err
	default:
		fs.SetOutput(a.stderr())
		fmt.Fprintln(a.stderr(), err)
		if hint := flagOrderHint(fs, args); hint != "" {
			fmt.Fprintln(a.stderr(), hint)
		}
		fs.Usage()
		return &UsageError{Err: err, Printed: true}
	}
}

// flagOrderHint explains D4 when a global flag was put after the command, like
// `bud update --verbose`
func flagOrderHint(fs *flag.FlagSet, args []string) string {
	if fs.Name() == "bud" {
		return ""
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if name, _ := flagName(arg); isGlobalFlag(arg) && fs.Lookup(name) == nil {
			return fmt.Sprintf("%s is a global flag, so it goes before the command: bud %s %s ...", arg, arg, fs.Name())
		}
	}

	return ""
}

func (a *App) stdout() io.Writer {
	if a.Stdout == nil {
		return os.Stdout
	}

	return a.Stdout
}

func (a *App) stderr() io.Writer {
	if a.Stderr == nil {
		return os.Stderr
	}

	return a.Stderr
}
