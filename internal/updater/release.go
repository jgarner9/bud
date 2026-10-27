// Package updater keeps bud current (BASE.md 5.7): it asks GitHub for the
// channel's newest release, and downloads, verifies and swaps in the binary.
// net/http only (D1)
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"jgarner.dev/bud/internal/version"
)

// Repo is where releases come from
const Repo = "jgarner9/bud"

// Channels: stable is the latest release; nightly is a rolling prerelease
// rebuilt from main, tagged "nightly", whose name is its version (D9, D42)
var Channels = []string{"stable", "nightly"}

// NightlyTag is the rolling nightly release's tag
const NightlyTag = "nightly"

// swappable in tests
var (
	apiBase = "https://api.github.com"
	client  = &http.Client{Timeout: 20 * time.Second}
)

// ErrNoRelease means the channel has nothing published yet
var ErrNoRelease = errors.New("no release published on this channel yet")

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is one GitHub release
type Release struct {
	Tag    string  `json:"tag_name"`
	Name   string  `json:"name"`
	Assets []Asset `json:"assets"`
	// Version is the release's version without a "v": the tag for stable,
	// the name for the rolling nightly
	Version string `json:"-"`
}

// Asset finds an attached file by name
func (r *Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}

	return Asset{}, false
}

// ArchiveName is the release archive for an OS and architecture, as
// .goreleaser.yaml names it (and install.sh downloads it)
func ArchiveName(goos, goarch string) string {
	return fmt.Sprintf("bud_%s_%s.tar.gz", goos, goarch)
}

// Latest asks GitHub for the channel's newest release
func Latest(ctx context.Context, channel string) (*Release, error) {
	var path string
	switch channel {
	case "stable":
		path = "/repos/" + Repo + "/releases/latest"
	case "nightly":
		path = "/repos/" + Repo + "/releases/tags/" + NightlyTag
	default:
		return nil, fmt.Errorf("unknown channel %q (one of %s)", channel, strings.Join(Channels, ", "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "bud/"+version.String())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNoRelease
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("checking for updates: GitHub said %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("checking for updates: %w", err)
	}
	r.Version = strings.TrimPrefix(r.Tag, "v")
	if channel == "nightly" {
		r.Version = strings.TrimPrefix(r.Name, "v")
	}
	if _, ok := version.Parse(r.Version); !ok {
		return nil, fmt.Errorf("checking for updates: the %s release has no version bud can read (%q)", channel, r.Version)
	}

	return &r, nil
}

// Status is what a check found
type Status struct {
	Channel string
	Current string // the running version
	Release *Release
	// Newer is true when Release is newer than Current. A development build
	// (no release version) is never offered an update
	Newer bool
	Dev   bool
}

// Check compares the running version with the channel's newest release
func Check(ctx context.Context, channel, current string) (*Status, error) {
	r, err := Latest(ctx, channel)
	if err != nil {
		return nil, err
	}

	s := &Status{Channel: channel, Current: current, Release: r}
	cur, ok := version.Parse(current)
	if !ok {
		s.Dev = true
		return s, nil
	}
	latest, _ := version.Parse(r.Version)
	s.Newer = version.Compare(latest, cur) > 0

	return s, nil
}
