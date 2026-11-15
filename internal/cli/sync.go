package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
	"jgarner.dev/bud/internal/syncer"
)

func init() {
	RegisterCommand(Sync())
}

// Sync brings the device's content and OpenCode config up to its channel
// (Suite D46, D60); see docs/internals/sync.md
func Sync() *Command {
	var dryRun, rollback bool
	return &Command{
		Name:  "sync",
		Usage: "Syncs content, the overlay and OpenCode's config to this device's channel",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&dryRun, "dry-run", false, "list what would change and change nothing")
			fs.BoolVar(&rollback, "rollback", false, "switch back to the previous content version still on disk")
		},
		Run: func(g *Globals, args []string) error { return runSync(g, args, dryRun, rollback) },
	}
}

// syncRun and syncRollback are swappable in tests of the command line
var syncRun, syncRollback = syncer.Run, syncer.Rollback

func runSync(g *Globals, args []string, dryRun, rollback bool) error {
	if len(args) > 0 {
		return usageErrorf("sync takes no arguments")
	}
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
		return fmt.Errorf("%w\nfix or remove %s, then run 'bud sync' again", err, p.MachineConfig())
	}
	o := syncer.Options{
		Paths:    p,
		Device:   s.Device,
		Overlay:  syncer.OverlayRepo(s.Overlay.Repo),
		Mirror:   s.Content.Mirror,
		Profiles: func() []profile.Dir { return profileDirs(p) },
		DryRun:   dryRun,
		Logf:     g.Logf,
	}
	run := syncRun
	if rollback {
		run = syncRollback
	}
	rep, err := run(context.Background(), o)
	if rep != nil {
		printSync(os.Stdout, rep, dryRun)
	}

	return err
}

// printSync lists each change, then each file left alone and why
func printSync(w io.Writer, rep *syncer.Report, dryRun bool) {
	for _, n := range rep.Notes {
		fmt.Fprintln(w, "note: "+n)
	}
	verb := map[string]string{syncer.Write: "write", syncer.Delete: "delete", syncer.Switch: "switch"}
	if dryRun {
		verb = map[string]string{syncer.Write: "would write", syncer.Delete: "would delete", syncer.Switch: "would switch"}
	}
	for _, c := range rep.Changes {
		line := fmt.Sprintf("  %-12s %s", verb[c.Action], c.Path)
		if c.Why != "" {
			line += " (" + c.Why + ")"
		}
		fmt.Fprintln(w, line)
	}
	for _, c := range rep.Skipped {
		fmt.Fprintf(w, "  %-12s %s: %s\n", "skipped", c.Path, c.Why)
	}
	switch {
	case !rep.Changed() && dryRun:
		fmt.Fprintf(w, "nothing would change: content %s is in use\n", rep.Content)
	case !rep.Changed():
		fmt.Fprintf(w, "nothing changed: content %s is in use\n", rep.Content)
	case dryRun:
		fmt.Fprintf(w, "dry run: %d changes, none made\n", len(rep.Changes))
	default:
		fmt.Fprintf(w, "synced content %s: %d changes\n", rep.Content, len(rep.Changes))
	}
}
