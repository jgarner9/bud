package nvim

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// devLinkName marks a config dir that `bud dev link` pointed at a checkout.
// The dir holds a symlink for each top-level entry of the checkout's nvim/,
// plus languages, layouts and tools -> the checkout's own, the same tree
// bud.Config() serves, so Lua edits show up on the next nvim start (D5). One
// symlink to nvim/ can't do that: those dirs live beside it in the repo
const devLinkName = ".bud-dev-link"

type devLink struct {
	Checkout string `json:"checkout"`
}

// DevLink returns the checkout dir is linked to, if it is
func DevLink(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, devLinkName))
	if err != nil {
		return "", false
	}
	var l devLink
	if json.Unmarshal(data, &l) != nil || l.Checkout == "" {
		return "", false
	}

	return l.Checkout, true
}

// Link points dir at checkout. A synced copy in dir is removed first
// (RemoveConfig, so the user's own files stay); a previous link is replaced
func Link(checkout, dir string) error {
	checkout, err := filepath.Abs(checkout)
	if err != nil {
		return err
	}
	for _, need := range []string{"nvim/init.lua", "languages"} {
		if _, err := os.Stat(filepath.Join(checkout, need)); err != nil {
			return fmt.Errorf("%s isn't a bud checkout (no %s)", checkout, need)
		}
	}

	if _, linked := DevLink(dir); linked {
		if _, err := Unlink(dir); err != nil {
			return err
		}
	} else if err := RemoveConfig(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	targets := map[string]string{}
	// the dirs bud.Config() mounts next to nvim/'s files
	for _, mount := range []string{"languages", "layouts", "tools"} {
		if _, err := os.Stat(filepath.Join(checkout, mount)); err == nil {
			targets[mount] = filepath.Join(checkout, mount)
		}
	}
	entries, err := os.ReadDir(filepath.Join(checkout, "nvim"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		targets[e.Name()] = filepath.Join(checkout, "nvim", e.Name())
	}
	for name := range targets {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			return fmt.Errorf("%s is in the way; move it and link again", filepath.Join(dir, name))
		}
	}
	for name, target := range targets {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(devLink{Checkout: checkout}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, devLinkName), append(data, '\n'), 0o644)
}

// Unlink removes the symlinks Link made into the checkout, and its marker,
// returning the checkout. Anything else in dir stays
func Unlink(dir string) (string, error) {
	checkout, ok := DevLink(dir)
	if !ok {
		return "", fmt.Errorf("%s isn't linked to a checkout", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(target, checkout+string(filepath.Separator)) {
			if err := os.Remove(path); err != nil {
				return "", err
			}
		}
	}
	if err := os.Remove(filepath.Join(dir, devLinkName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	return checkout, nil
}
