package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"jgarner.dev/bud/internal/claude"
)

func init() {
	RegisterCommand(Claude())
}

// Claude checks and sets up Claude Code, which every AI part of bud runs on
// (D26). Logging in is Claude Code's own; bud stores no keys
func Claude() *Command {
	var yes bool
	return &Command{
		Name:  "claude",
		Usage: "Checks and sets up Claude Code, which bud's AI runs on",
		Subs: []*Command{
			{Name: "status", Usage: "show whether Claude Code is installed and logged in", Run: func(_ *Globals, args []string) error { return claudeStatus(args) }},
			{
				Name:  "setup",
				Usage: "install Claude Code if it's missing, then log in with its own login",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&yes, "yes", false, "install without asking (logging in still needs a terminal)")
					fs.BoolVar(&yes, "y", false, "short for -yes")
				},
				Run: func(_ *Globals, args []string) error {
					if len(args) > 0 {
						return usageErrorf("claude setup takes no arguments")
					}
					return claudeSetup(terminalPrompter(yes), yes)
				},
			},
		},
	}
}

// swappable in tests
var (
	checkClaude   = claude.Check
	runOnTerminal = func(argv []string) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
)

func claudeStatus(args []string) error {
	if len(args) > 0 {
		return usageErrorf("claude status takes no arguments")
	}
	s, err := checkClaude()
	if err != nil {
		return err
	}
	switch {
	case s.Bin == "":
		fmt.Println("Claude Code isn't installed: run bud claude setup")
	case !s.LoggedIn:
		fmt.Printf("Claude Code %s (%s), not logged in: run bud claude setup\n", s.Version, s.Bin)
	default:
		fmt.Printf("Claude Code %s (%s), logged in with %s\n", s.Version, s.Bin, s.Method)
	}

	return nil
}

// claudeSetup installs Claude Code when it's missing and logs it in when it
// isn't. pr is nil when nobody can be asked: then --yes still installs, but
// the login, which is interactive, is left for later
func claudeSetup(pr *prompter, yes bool) error {
	s, err := checkClaude()
	if err != nil {
		return err
	}

	if s.Bin == "" {
		argv := claude.InstallCommand()
		fmt.Printf("Claude Code isn't installed. bud will run Anthropic's installer:\n  %s\n", claude.InstallScript)
		if !yes {
			if pr == nil {
				return errors.New("nothing changed: there's no terminal to confirm on, run 'bud claude setup --yes' to skip asking")
			}
			ok, err := pr.confirm("Continue?")
			if err != nil {
				return err
			}
			if !ok {
				return errNotConfirmed
			}
		}
		if err := runOnTerminal(argv); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
		if s, err = checkClaude(); err != nil {
			return err
		}
		if s.Bin == "" {
			return errors.New("the installer finished but claude still isn't on PATH or in ~/.local/bin")
		}
	}

	if !s.LoggedIn {
		login := claude.LoginCommand(s.Bin)
		if pr == nil {
			fmt.Printf("Claude Code %s is installed. Log in when you're at a terminal:\n  %s\n", s.Version, strings.Join(login, " "))
			return nil
		}
		fmt.Printf("Claude Code %s needs logging in; handing over to its login.\n", s.Version)
		if err := runOnTerminal(login); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(login, " "), err)
		}
		if s, err = checkClaude(); err != nil {
			return err
		}
		if !s.LoggedIn {
			return fmt.Errorf("Claude Code still isn't logged in; try %s", strings.Join(login, " "))
		}
	}
	fmt.Printf("Claude Code %s is ready, logged in with %s.\n", s.Version, s.Method)

	return nil
}
