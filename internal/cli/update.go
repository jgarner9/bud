package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
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
		Usage: "Installs a newer release if there is one, then syncs the Bud Neovim config and restores pinned plugins; --check only looks",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&check, "check", false, "only report whether a newer release exists")
			fs.StringVar(&channel, "channel", "", "release channel to check: "+strings.Join(updater.Channels, " or ")+" (default: update.channel in config.json)")
		},
		Run: func(g *Globals, args []string) error { return update(g, args, check, channel) },
	}
}

// updatedFromEnv tells a freshly swapped-in bud which version it replaced, so
// it skips the release check and goes straight to the sync
const updatedFromEnv = "BUD_UPDATED_FROM"

// swappable in tests
var (
	checkRelease = updater.Check
	fetchRelease = updater.Fetch
	checkBinary  = updater.CheckBinary
	execBinary   = syscall.Exec
	executable   = os.Executable
)

func update(g *Globals, args []string, check bool, channel string) error {
	if len(args) > 0 {
		return usageErrorf("update takes no arguments")
	}
	if channel != "" && !slices.Contains(updater.Channels, channel) {
		return usageErrorf("--channel must be one of %s, not %q", strings.Join(updater.Channels, ", "), channel)
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if channel == "" {
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

	if check {
		return checkForUpdate(g, channel)
	}

	if from := os.Getenv(updatedFromEnv); from != "" {
		os.Unsetenv(updatedFromEnv)
		fmt.Printf("Updated bud from %s to %s.\n", from, version.String())
	} else if err := selfUpdate(g, p, channel); err != nil {
		return err
	}

	return nvim.Apply()
}

// selfUpdate swaps in the channel's newest release and re-execs it to finish
// the update (BASE.md 5.7 step 1), so it only returns when there's nothing to
// install or this binary isn't bud's to replace (D20). Failing to reach
// GitHub isn't an error: the sync still repairs this version's config
func selfUpdate(g *Globals, p *paths.Paths, channel string) error {
	g.Logf("checking the %s channel", channel)
	s, err := checkRelease(context.Background(), channel, version.String())
	if errors.Is(err, updater.ErrNoRelease) {
		g.Logf("nothing published on %s", channel)
		return nil
	}
	if err != nil {
		fmt.Printf("Couldn't check for a newer release (%v); syncing this version's config.\n", err)
		return nil
	}
	if s.Dev || !s.Newer {
		g.Logf("no newer release (have %s, newest %s)", s.Current, s.Release.Version)
		return nil
	}

	receipt, err := installer.ReadReceipt(p.InstallReceipt())
	if err != nil {
		return err
	}
	exe, _ := executable()
	if receipt == nil {
		fmt.Printf("bud %s is available, but install.sh didn't install this bud, so update it %s.\n", s.Release.Version, updateHint(exe))
		return nil
	}
	if exe == "" || !samePath(exe, receipt.Path) {
		fmt.Printf("bud %s is available, but this bud (%s) isn't the one install.sh installed; run %s update.\n", s.Release.Version, exe, receipt.Path)
		return nil
	}

	fmt.Printf("Updating bud %s to %s...\n", s.Current, s.Release.Version)
	dir, err := os.MkdirTemp("", "bud-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx := context.Background()
	bin, err := fetchRelease(ctx, s.Release, runtime.GOOS, runtime.GOARCH, dir, g.Logf)
	if err != nil {
		return err
	}
	if err := checkBinary(ctx, bin); err != nil {
		return err
	}
	if err := updater.Swap(bin, receipt.Path, p.PrevBinary()); err != nil {
		return err
	}
	g.Logf("kept %s as %s", s.Current, p.PrevBinary())

	// the new binary holds the new embedded config, so it does the sync
	args := []string{receipt.Path}
	if g.Verbose {
		args = append(args, "--verbose")
	}
	args = append(args, "update")
	os.RemoveAll(dir)
	env := append(os.Environ(), updatedFromEnv+"="+s.Current)
	if err := execBinary(receipt.Path, args, env); err != nil {
		return fmt.Errorf("starting the new bud: %w", err)
	}

	return nil
}

// updateHint names the tool that owns a bud install.sh didn't put in place
func updateHint(exe string) string {
	exe, _ = filepath.EvalSymlinks(exe)
	switch {
	case strings.Contains(exe, "/Cellar/") || strings.HasPrefix(exe, "/opt/homebrew/") || strings.Contains(exe, "/linuxbrew/"):
		return "with brew upgrade bud"
	case strings.Contains(exe, "/go/bin/") || os.Getenv("GOBIN") != "" && filepath.Dir(exe) == os.Getenv("GOBIN"):
		return "with go install jgarner.dev/bud/cmd/bud@latest"
	default:
		return "the way you installed it (or reinstall with install.sh)"
	}
}

// checkForUpdate reports the channel's newest release against this binary
func checkForUpdate(g *Globals, channel string) error {
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
