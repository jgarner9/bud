// Package overlay fetches the private overlay and layers it over the public
// content (Suite D21, D59). The overlay is a plain git repo, reached with
// the device's own git credentials (Suite D60: it is never signed; trust
// comes from it being a private repo): profiles/<device>.json, env/*.jsonc
// and content/ with personal skills, agents, commands, plugins and an
// AGENTS.md. Without an overlay, Bud runs on the public content alone
package overlay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Format is the overlay.json format this bud reads
const Format = 1

// Fetch makes dir a fast-forwarded clone of url and returns its commit.
// It clones when dir is missing; otherwise dir must already be a clone of
// url, with no local changes, and it only ever fast-forwards: when the
// clone has diverged from its remote it stops and says so, changing nothing
func Fetch(ctx context.Context, url, dir string) (string, error) {
	if url == "" {
		return "", errors.New("no overlay repo is set")
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		if _, err := git(ctx, "", "clone", "--quiet", "--", url, dir); err != nil {
			return "", fmt.Errorf("clone the overlay: %w", err)
		}
		return git(ctx, dir, "rev-parse", "HEAD")
	} else if err != nil {
		return "", err
	}

	origin, err := git(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("%s isn't a git clone of the overlay: %w", dir, err)
	}
	if origin != url {
		return "", fmt.Errorf("%s is a clone of %s, not %s; move it aside to clone the new one, or set the overlay repo back", dir, origin, url)
	}
	if status, err := git(ctx, dir, "status", "--porcelain"); err != nil {
		return "", err
	} else if status != "" {
		return "", fmt.Errorf("the overlay at %s has local changes; commit and push them, or stash them, and sync again (nothing changed)", dir)
	}
	if _, err := git(ctx, dir, "fetch", "--quiet", "origin"); err != nil {
		return "", fmt.Errorf("fetch the overlay: %w", err)
	}
	if _, err := git(ctx, dir, "merge", "--ff-only", "--quiet", "@{upstream}"); err != nil {
		return "", fmt.Errorf("the overlay at %s has diverged from %s, so it can't fast-forward; sort it out with git -C %s status (nothing changed)", dir, url, dir)
	}

	return git(ctx, dir, "rev-parse", "HEAD")
}

// git runs git in dir and returns its trimmed stdout; on failure the error
// carries git's own message
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// never stop to ask for a password: a sync runs unattended
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}

	return strings.TrimSpace(out.String()), nil
}

// CheckFormat reads the overlay's overlay.json, when it has one, and refuses
// a format this bud doesn't read
func CheckFormat(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "overlay.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var meta struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return fmt.Errorf("overlay.json: %w", err)
	}
	if meta.Format != Format {
		return fmt.Errorf("overlay.json is format %d, but this bud reads format %d; update bud, or the overlay", meta.Format, Format)
	}

	return nil
}
