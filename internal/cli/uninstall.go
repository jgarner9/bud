package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Uninstall())
}

func Uninstall() *Command {
	var yes, keepConfig bool
	return &Command{
		Name:  "uninstall",
		Usage: "Removes everything bud put on this machine, after listing it and asking",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "remove without asking")
			fs.BoolVar(&yes, "y", false, "short for -yes")
			fs.BoolVar(&keepConfig, "keep-config", false, "keep the machine config (config.json) and user-tools/")
		},
		Run: func(g *Globals, args []string) error { return uninstall(g, args, yes, keepConfig) },
	}
}

// removal is one thing uninstall deletes
type removal struct {
	path string
	what string
	do   func() error
}

func uninstall(g *Globals, args []string, yes, keepConfig bool) error {
	if len(args) > 0 {
		return usageErrorf("uninstall takes no arguments")
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)

	removals, kept, err := planUninstall(p, keepConfig, exe)
	if err != nil {
		return err
	}
	if len(removals) == 0 {
		fmt.Println("Nothing of bud's is installed here.")
		printKept(os.Stdout, kept)
		return nil
	}

	fmt.Println("bud uninstall will remove:")
	for _, r := range removals {
		fmt.Printf("  %s\n      %s\n", paint(os.Stdout, ansiBold, r.path), paint(os.Stdout, ansiDim, r.what))
	}
	printKept(os.Stdout, kept)

	err = confirmOnTerminal("Continue?", yes, "bud uninstall --yes")
	if errors.Is(err, errNotConfirmed) {
		fmt.Println("Nothing removed.")
		return nil
	}
	if err != nil {
		return err
	}

	// keep going after a failure, so one stuck path doesn't leave the rest behind
	var errs []error
	for _, r := range removals {
		g.Logf("removing %s", r.path)
		if err := r.do(); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", r.path, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	fmt.Println("bud is uninstalled.")

	return nil
}

// swappable in tests
var findBundles = bundle.Find

// planUninstall works out what to remove and what to deliberately leave. exe is
// the running binary, reported as kept when install.sh didn't put it there
func planUninstall(p *paths.Paths, keepConfig bool, exe string) (removals []removal, kept []string, err error) {
	// read before anything is removed: the receipt lives in the state dir
	receipt, err := installer.ReadReceipt(p.InstallReceipt())
	if err != nil {
		return nil, nil, err
	}

	// before the state dir goes, with the kit's ledger in it. A kit that can't
	// be listed doesn't block the rest
	if r, ok, err := kitRemoval(p); err != nil {
		kept = append(kept, fmt.Sprintf("what bud's kit added to Claude Code (couldn't list it: %v; 'bud kit unsync' takes it out)", err))
	} else if ok {
		removals = append(removals, r)
	}

	if r, ok, err := configRemoval(p.Config); err != nil {
		return nil, nil, err
	} else if ok {
		removals = append(removals, r)
	}

	dirs := []struct {
		dir, what, keep, keptWhat string
	}{
		{p.Data, "plugins, Mason packages, bud's own binaries and user tools", "user-tools", "everything except user-tools/"},
		{p.State, "machine config, project registry, Neovim history and undo", "config.json", "everything except config.json"},
		{p.Cache, "Neovim cache and logs", "", ""},
	}
	for _, d := range dirs {
		if !exists(d.dir) {
			continue
		}
		if keepConfig && d.keep != "" {
			dir, keep := d.dir, d.keep
			removals = append(removals, removal{dir, d.keptWhat, func() error { return removeAllExcept(dir, keep) }})
			kept = append(kept, filepath.Join(d.dir, d.keep)+"  (--keep-config)")
			continue
		}
		dir := d.dir
		removals = append(removals, removal{dir, d.what, func() error { return os.RemoveAll(dir) }})
	}

	for _, b := range findBundles(p) {
		removals = append(removals, removal{b.Path, b.What, b.Remove})
	}

	if receipt != nil && isFile(receipt.Path) {
		path := receipt.Path
		removals = append(removals, removal{path, "the bud binary, installed by install.sh", func() error { return os.Remove(path) }})
	}
	// a binary from go install or a package manager belongs to that tool (D20)
	if exe != "" && (receipt == nil || !samePath(exe, receipt.Path)) {
		kept = append(kept, exe+"  (not installed by install.sh: remove it with the tool that installed it)")
	}

	return removals, kept, nil
}

// kitRemoval takes out of Claude Code's config what the kit put there
// (JGA-151), the same as bud kit unsync. bud's own config dir goes with the
// data dir anyway; a shared ~/.claude (D76), or a CLAUDE_CONFIG_DIR you set,
// keeps everything of yours
func kitRemoval(p *paths.Paths) (removal, bool, error) {
	ledger, ok, err := kit.ReadLedger(p.KitLedger())
	if err != nil || !ok {
		return removal{}, false, err
	}
	dir := ledger.Dir
	if dir == "" {
		if dir, err = homeClaudeDir(); err != nil {
			return removal{}, false, err
		}
	}
	if rel, err := filepath.Rel(p.Data, dir); err == nil && filepath.IsLocal(rel) {
		return removal{}, false, nil
	}
	env, err := kitEnv(p)
	if err != nil {
		return removal{}, false, err
	}
	env.ClaudeDir, env.ClaudeJSON = dir, claudeJSONFor(dir)
	var actions []kit.Action
	err = withClaudeDir(dir, func() (err error) {
		actions, err = kit.Unsync(env, true)
		return err
	})
	if err != nil || len(actions) == 0 {
		return removal{}, false, err
	}
	noun := "things"
	if len(actions) == 1 {
		noun = "thing"
	}

	return removal{
		path: dir,
		what: fmt.Sprintf("the %d %s bud's kit added to Claude Code ('bud kit unsync --dry-run' lists them); everything of yours stays", len(actions), noun),
		do: func() error {
			return withClaudeDir(dir, func() error {
				_, err := kit.Unsync(env, false)
				return err
			})
		},
	}, true, nil
}

func configRemoval(dir string) (removal, bool, error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return removal{}, false, nil
	}
	if err != nil {
		return removal{}, false, err
	}

	r := removal{path: dir, do: func() error { return nvim.RemoveConfig(dir) }}
	if info.Mode()&fs.ModeSymlink != 0 {
		r.what = "symlink to a dev checkout (the checkout itself stays)"
		return r, true, nil
	}

	files, err := nvim.ManagedFiles(dir)
	if err != nil {
		return removal{}, false, err
	}
	noun := "files"
	if len(files) == 1 {
		noun = "file"
	}
	r.what = fmt.Sprintf("Neovim config: the %d %s bud synced; anything else in it stays", len(files), noun)

	return r, true, nil
}

// removeAllExcept removes everything in dir but the entry named keep
func removeAllExcept(dir, keep string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}

	return nil
}

func printKept(w io.Writer, kept []string) {
	if len(kept) == 0 {
		return
	}
	fmt.Fprintln(w, "kept:")
	for _, k := range slices.Compact(kept) {
		fmt.Fprintf(w, "  %s\n", k)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)

	return errA == nil && errB == nil && ra == rb
}
