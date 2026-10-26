package nvim

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// ConfigDir is stdpath("config") for NVIM_APPNAME=bud. Neovim uses
// ~/.config on macOS too, not ~/Library
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, AppName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", AppName), nil
}

// SyncConfig makes dst match src for every file bud owns. It serves both
// install (empty dst, everything is added) and update (only differences are written)
func SyncConfig(src fs.FS, dst string) (*SyncResult, error) {
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
		err := os.Remove(filepath.Join(dst, filepath.FromSlash(path)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		res.Removed = append(res.Removed, path)
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

	return files, nil
}

func writeManifest(dir string, files []string) error {
	data, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, manifestName), append(data, '\n'), 0o644)
}
