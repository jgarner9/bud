package nvim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/paths"
)

// manifestName records every file SyncConfig wrote, so a file dropped from the
// repo is removed on the next sync while anything else in the dir (local/, user
// tools) is never touched
const manifestName = ".bud-manifest.json"

type SyncResult struct {
	Added   []string
	Updated []string
	Removed []string
}

func (r *SyncResult) Changed() bool {
	return len(r.Added)+len(r.Updated)+len(r.Removed) > 0
}

func Apply() error {
	fmt.Println("Configuring Neovim...")

	p, err := paths.Resolve()
	if err != nil {
		return fmt.Errorf("finding config directory: %w", err)
	}
	dst := p.Config
	fmt.Println("Bud config directory: " + dst)

	fmt.Println("Syncing config...")
	res, err := SyncConfig(bud.NvimConfig(), dst)
	if err != nil {
		return err
	}

	changes := 0
	if len(res.Added) > 0 {
		changes += len(res.Added)
		fmt.Println("Added:")
		for _, file := range res.Added {
			fmt.Println(file)
		}
	}

	if len(res.Updated) > 0 {
		changes += len(res.Updated)
		fmt.Println("Updated:")
		for _, file := range res.Updated {
			fmt.Println(file)
		}
	}

	if len(res.Removed) > 0 {
		changes += len(res.Removed)
		fmt.Println("Removed:")
		for _, file := range res.Removed {
			fmt.Println(file)
		}
	}

	if changes == 0 {
		fmt.Println("Already up to date.")
	} else {
		fmt.Println("Restoring lazy.nvim plugins for changes...")
		if err := RestorePlugins(); err != nil {
			return err
		}
	}

	return nil
}

// SyncConfig makes dst match src for every file bud owns. It serves both
// install (empty dst, everything is added) and update (only differences are written)
func SyncConfig(src fs.FS, dst string) (*SyncResult, error) {
	return syncConfig(src, dst, true)
}

// CheckConfig reports what SyncConfig would change without writing anything:
// Added are managed files that are missing, Updated ones that differ from src,
// and Removed ones a newer release dropped that are still there
func CheckConfig(src fs.FS, dst string) (*SyncResult, error) {
	return syncConfig(src, dst, false)
}

func syncConfig(src fs.FS, dst string, write bool) (*SyncResult, error) {
	res := &SyncResult{}

	prev, err := readManifest(dst)
	if err != nil {
		return nil, err
	}

	var files []string
	err = fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files = append(files, path)

		want, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, filepath.FromSlash(path))
		have, err := os.ReadFile(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			res.Added = append(res.Added, path)
		case err != nil:
			return err
		case bytes.Equal(have, want):
			return nil
		default:
			res.Updated = append(res.Updated, path)
		}

		if !write {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		return os.WriteFile(target, want, 0o644)
	})
	if err != nil {
		return nil, err
	}

	for _, path := range prev {
		if slices.Contains(files, path) {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(path))
		if !write {
			if _, err := os.Lstat(target); err == nil {
				res.Removed = append(res.Removed, path)
			}
			continue
		}
		err := os.Remove(target)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		res.Removed = append(res.Removed, path)
	}

	if !write {
		return res, nil
	}
	if err := writeManifest(dst, files); err != nil {
		return nil, err
	}

	return res, nil
}

// RestorePlugins checks out every plugin at the commit pinned in lazy-lock.json
func RestorePlugins() error {
	bin, err := exec.LookPath("nvim")
	if err != nil {
		return err
	}

	cmd := exec.Command(bin, "--headless", "+Lazy! restore", "+qa")
	cmd.Env = env()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// ManagedFiles lists the files the last sync wrote into dir, nil when there was none
func ManagedFiles(dir string) ([]string, error) {
	return readManifest(dir)
}

// RemoveConfig deletes what sync wrote into dir: the managed files, the manifest,
// and any directory that leaves empty, dir included. Anything else stays. A
// symlink at dir (a dev checkout) is removed itself, never followed
func RemoveConfig(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(dir)
	}

	files, err := readManifest(dir)
	if err != nil {
		return err
	}

	dirs := map[string]bool{dir: true}
	for _, path := range append(files, manifestName) {
		target := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for d := filepath.Dir(target); d != dir; d = filepath.Dir(d) {
			dirs[d] = true
		}
	}

	// deepest first, so a parent is only tried once its children are gone
	sorted := slices.Collect(maps.Keys(dirs))
	slices.SortFunc(sorted, func(a, b string) int { return len(b) - len(a) })
	for _, d := range sorted {
		// a dir that still holds something of the user's stays
		if entries, err := os.ReadDir(d); err == nil && len(entries) == 0 {
			if err := os.Remove(d); err != nil {
				return err
			}
		}
	}

	return nil
}

// HasManifest reports whether dir has been synced at least once
func HasManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, manifestName))
	return err == nil
}

func readManifest(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var files []string
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, err
	}

	// the manifest drives deletes, so an entry escaping dir must never reach os.Remove
	for _, path := range files {
		if !filepath.IsLocal(filepath.FromSlash(path)) {
			return nil, fmt.Errorf("%s: path %q is outside the config directory", manifestName, path)
		}
	}

	return files, nil
}

func writeManifest(dir string, files []string) error {
	data, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, manifestName), append(data, '\n'), 0o644)
}
