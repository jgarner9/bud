package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/updater"
	"jgarner.dev/bud/internal/version"
)

func init() {
	RegisterCommand(Update())
}

func Update() *Command {
	var check bool
	var channel string
	return &Command{
		Name:  "update",
		Usage: "Syncs the Bud Neovim config and restores pinned plugins; --check looks for a newer release",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&check, "check", false, "only report whether a newer release exists")
			fs.StringVar(&channel, "channel", "", "release channel to check: "+strings.Join(updater.Channels, " or ")+" (default: update.channel in config.json)")
		},
		Run: func(g *Globals, args []string) error { return update(g, args, check, channel) },
	}
}

// swappable in tests
var checkRelease = updater.Check

func update(g *Globals, args []string, check bool, channel string) error {
	if len(args) > 0 {
		return usageErrorf("update takes no arguments")
	}
	if channel != "" && !slices.Contains(updater.Channels, channel) {
		return usageErrorf("--channel must be one of %s, not %q", strings.Join(updater.Channels, ", "), channel)
	}

	if check {
		return checkForUpdate(g, channel)
	}

	return nvim.Apply()
}

// checkForUpdate reports the channel's newest release against this binary
func checkForUpdate(g *Globals, channel string) error {
	if channel == "" {
		p, err := paths.Resolve()
		if err != nil {
			return err
		}
		f, err := config.Load(p.MachineConfig())
		if err != nil {
			return err
		}
		s, err := f.Settings()
		if err != nil {
			return err
		}
		channel = s.Update.Channel
	}
	g.Logf("checking the %s channel", channel)

	s, err := checkRelease(context.Background(), channel, version.String())
	if errors.Is(err, updater.ErrNoRelease) {
		fmt.Printf("Nothing is published on the %s channel yet.\n", channel)
		return nil
	}
	if err != nil {
		return err
	}

	switch {
	case s.Dev:
		fmt.Printf("This is a development build (%s); the newest %s release is %s.\n", s.Current, channel, s.Release.Version)
	case s.Newer:
		fmt.Printf("bud %s is available on %s (you have %s).\n", s.Release.Version, channel, s.Current)
	default:
		fmt.Printf("bud %s is up to date on %s.\n", s.Current, channel)
	}

	return nil
}
