package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/doctor"
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
	var check, background bool
	var channel string
	return &Command{
		Name:  "update",
		Usage: "Installs a newer release if there is one, then syncs the Bud Neovim config and restores pinned plugins; --check only looks",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&check, "check", false, "only report whether a newer release exists")
			fs.BoolVar(&background, "background", false, "the check bud starts at launch: install a stable release (update.auto), or note it for the dashboard")
			fs.StringVar(&channel, "channel", "", "release channel to check: "+strings.Join(updater.Channels, " or ")+" (default: update.channel in config.json)")
		},
		Run: func(g *Globals, args []string) error { return update(g, args, check, background, channel) },
	}
}

// Environment one bud passes the next across an update's re-exec.
// updatedFromEnv tells a freshly swapped-in bud which version it replaced, so
// it skips the release check and goes straight to the sync; failingBeforeEnv
// lists the doctor checks already failing then, which don't count against the
// new version. rolledBackEnv and rollbackWhyEnv tell the restored bud which
// version was rolled back and why
const (
	updatedFromEnv   = "BUD_UPDATED_FROM"
	failingBeforeEnv = "BUD_FAILING_BEFORE"
	rolledBackEnv    = "BUD_ROLLED_BACK_FROM"
	rollbackWhyEnv   = "BUD_ROLLBACK_WHY"
)

// swappable in tests
var (
	checkRelease  = updater.Check
	fetchRelease  = updater.Fetch
	checkBinary   = updater.CheckBinary
	execBinary    = syscall.Exec
	executable    = os.Executable
	applyConfig   = nvim.Apply
	failingChecks = failingNames
)

func update(g *Globals, args []string, check, background bool, channel string) error {
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
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return err
	}
	settings, err := f.Settings()
	if err != nil {
		return err
	}
	if channel == "" {
		channel = settings.Update.Channel
	}

	if check {
		return checkForUpdate(g, channel)
	}
	if background {
		return backgroundUpdate(g, p, channel, settings.Update.Auto)
	}

	if from := os.Getenv(rolledBackEnv); from != "" {
		os.Unsetenv(rolledBackEnv)
		reason := os.Getenv(rollbackWhyEnv)
		os.Unsetenv(rollbackWhyEnv)
		if err := applyConfig(); err != nil {
			return err
		}
		return fmt.Errorf("the update to %s was rolled back to %s: %s", from, version.String(), reason)
	}
	if from := os.Getenv(updatedFromEnv); from != "" {
		before := os.Getenv(failingBeforeEnv)
		os.Unsetenv(updatedFromEnv)
		os.Unsetenv(failingBeforeEnv)
		fmt.Printf("Updated bud from %s to %s.\n", from, version.String())
		return finishUpdate(g, p, from, strings.Split(before, ","))
	}
	if err := selfUpdate(g, p, channel); err != nil {
		return err
	}
	if err := applyConfig(); err != nil {
		return err
	}
	resyncKit(g, p)

	return nil
}

// finishUpdate runs in the freshly swapped-in binary: sync its config, then
// doctor. A sync error, or a check failing that passed before the swap,
// puts bud.prev back and re-execs it to sync its own config (5.7 step 6)
func finishUpdate(g *Globals, p *paths.Paths, from string, failingBefore []string) error {
	why := ""
	if err := applyConfig(); err != nil {
		why = "syncing its config failed: " + err.Error()
	} else {
		var broke []string
		for _, name := range failingChecks() {
			if !slices.Contains(failingBefore, name) {
				broke = append(broke, name)
			}
		}
		if len(broke) == 0 {
			editState(g, p, func(st *updater.State) { st.Available, st.Installed = "", "" })
			resyncKit(g, p)
			return nil
		}
		why = "bud doctor failed: " + strings.Join(broke, ", ")
	}

	receipt, err := installer.ReadReceipt(p.InstallReceipt())
	if err != nil || receipt == nil {
		return fmt.Errorf("%s, and there's no install record to roll back with: %v", why, err)
	}
	fmt.Printf("%s; going back to %s.\n", why, from)
	if err := updater.Restore(p.PrevBinary(), receipt.Path); err != nil {
		return fmt.Errorf("%s, and rolling back failed: %w", why, err)
	}

	env := append(os.Environ(), rolledBackEnv+"="+version.String(), rollbackWhyEnv+"="+why)
	if err := execBinary(receipt.Path, updateArgs(g, receipt.Path), env); err != nil {
		return fmt.Errorf("starting %s again: %w", from, err)
	}

	return nil
}

// updateArgs is the command line that hands an update to another bud
func updateArgs(g *Globals, bin string) []string {
	args := []string{bin}
	if g.Verbose {
		args = append(args, "--verbose")
	}

	return append(args, "update")
}

// failingNames runs the quick doctor checks and names the ones that fail
func failingNames() []string {
	var names []string
	for _, r := range doctor.Run(doctor.Options{Quick: true}) {
		if r.Status == doctor.Fail {
			names = append(names, r.Name)
		}
	}

	return names
}

// editState changes update.json; failing to write it only costs a banner
func editState(g *Globals, p *paths.Paths, edit func(*updater.State)) {
	st := updater.LoadState(p.UpdateState())
	edit(st)
	if err := st.Save(p.UpdateState()); err != nil {
		g.Logf("saving %s: %v", p.UpdateState(), err)
	}
}

// replaceable says where this bud lives when it's the one install.sh
// installed, so bud may replace it (D20); otherwise it says why not
func replaceable(p *paths.Paths, release string) (target, why string, err error) {
	receipt, err := installer.ReadReceipt(p.InstallReceipt())
	if err != nil {
		return "", "", err
	}
	exe, _ := executable()
	if receipt == nil {
		return "", fmt.Sprintf("bud %s is available, but install.sh didn't install this bud, so update it %s.", release, updateHint(exe)), nil
	}
	if exe == "" || !samePath(exe, receipt.Path) {
		return "", fmt.Sprintf("bud %s is available, but this bud (%s) isn't the one install.sh installed; run %s update.", release, exe, receipt.Path), nil
	}

	return receipt.Path, "", nil
}

// swapIn downloads and checks r, then swaps it in at target, keeping this
// bud as bud.prev. It returns the doctor checks failing beforehand: what's
// broken already isn't the new version's fault (finishUpdate)
func swapIn(g *Globals, p *paths.Paths, r *updater.Release, target string) ([]string, error) {
	before := failingChecks()
	dir, err := os.MkdirTemp("", "bud-update-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	ctx := context.Background()
	bin, err := fetchRelease(ctx, r, runtime.GOOS, runtime.GOARCH, dir, g.Logf)
	if err != nil {
		return nil, err
	}
	if err := checkBinary(ctx, bin); err != nil {
		return nil, err
	}
	if err := updater.Swap(bin, target, p.PrevBinary()); err != nil {
		return nil, err
	}
	g.Logf("kept %s as %s", version.String(), p.PrevBinary())

	return before, nil
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
		editState(g, p, func(st *updater.State) { st.Available = "" })
		return nil
	}

	target, why, err := replaceable(p, s.Release.Version)
	if err != nil {
		return err
	}
	if target == "" {
		fmt.Println(why)
		return nil
	}

	fmt.Printf("Updating bud %s to %s...\n", s.Current, s.Release.Version)
	before, err := swapIn(g, p, s.Release, target)
	if err != nil {
		return err
	}

	// the new binary holds the new embedded config, so it does the sync
	env := append(os.Environ(), updatedFromEnv+"="+s.Current, failingBeforeEnv+"="+strings.Join(before, ","))
	if err := execBinary(target, updateArgs(g, target), env); err != nil {
		return fmt.Errorf("starting the new bud: %w", err)
	}

	return nil
}

// backgroundUpdate is the check launching bud starts at most once a day (D9).
// On stable with update.auto it swaps a newer release in, and the next launch
// syncs its config (finishPendingUpdate); otherwise (nightly, D42) it only
// records the release for the dashboard's banner
func backgroundUpdate(g *Globals, p *paths.Paths, channel string, auto bool) error {
	// stamped first, so launches while this runs don't start another
	editState(g, p, func(st *updater.State) { st.CheckedAt, st.Channel = now(), channel })

	s, err := checkRelease(context.Background(), channel, version.String())
	if errors.Is(err, updater.ErrNoRelease) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.Dev || !s.Newer {
		editState(g, p, func(st *updater.State) { st.Available = "" })
		return nil
	}

	if channel == "stable" && auto {
		target, why, err := replaceable(p, s.Release.Version)
		if err != nil {
			return err
		}
		if target != "" {
			before, err := swapIn(g, p, s.Release, target)
			if err == nil {
				editState(g, p, func(st *updater.State) {
					st.Available, st.Installed, st.From, st.FailingBefore = "", s.Release.Version, s.Current, before
				})
				g.Logf("installed %s; its config syncs at the next launch", s.Release.Version)
				return nil
			}
			g.Logf("installing %s: %v", s.Release.Version, err)
		} else {
			g.Logf("%s", why)
		}
	}

	editState(g, p, func(st *updater.State) { st.Available = s.Release.Version })

	return nil
}

// finishPendingUpdate syncs the config of a release the background check
// swapped in, before this launch opens nvim, rolling back if it breaks
func finishPendingUpdate(g *Globals, p *paths.Paths) error {
	st := updater.LoadState(p.UpdateState())
	if st.Installed == "" {
		return nil
	}
	installed, from, before := st.Installed, st.From, st.FailingBefore
	// cleared first, so a sync that dies never runs on every launch
	editState(g, p, func(st *updater.State) { st.Installed, st.From, st.FailingBefore = "", "", nil })
	if installed != version.String() {
		g.Logf("update.json names %s, but this is %s", installed, version.String())
		return nil
	}

	fmt.Printf("Finishing the update from %s to %s...\n", from, installed)

	return finishUpdate(g, p, from, before)
}

// startUpdateCheck starts `bud update --background` detached, at most once a
// CheckInterval, so launching never waits on the network (D9). Skipped with
// --no-update and for development builds, which are never offered updates
func startUpdateCheck(g *Globals, p *paths.Paths) {
	if g.NoUpdate {
		return
	}
	if _, ok := version.Parse(version.String()); !ok {
		return
	}
	if !updater.LoadState(p.UpdateState()).Due(now()) {
		return
	}
	exe, err := executable()
	if err != nil {
		g.Logf("update check: %v", err)
		return
	}
	if err := startDetached(p, exe, "update", "--background"); err != nil {
		g.Logf("update check: %v", err)
	}
}

// startDetached runs bin in its own session, writing to the update log, and
// doesn't wait: this process is about to become nvim. Swappable in tests
var startDetached = func(p *paths.Paths, bin string, args ...string) error {
	if err := os.MkdirAll(filepath.Dir(p.UpdateLog()), 0o755); err != nil {
		return err
	}
	log, err := os.OpenFile(p.UpdateLog(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	return cmd.Process.Release()
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
