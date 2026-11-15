package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
	"jgarner.dev/bud/internal/registry"
)

// What bud doctor asks of the last sync (build plan task 1.9, Suite D46).
// None of it fetches anything except FetchChannel

// ProfileDirs are where device profiles are read from: the overlay's
// profiles/ first, so it replaces an example of the same name, then the
// examples built into bud
func ProfileDirs(p *paths.Paths) []profile.Dir {
	return []profile.Dir{
		profile.OS(filepath.Join(p.Overlay(), "profiles")),
		{Name: "bud's examples", FS: bud.Profiles()},
	}
}

// OverlayRepo is the overlay's git URL: BUD_OVERLAY when set, else the
// configured overlay.repo
func OverlayRepo(configured string) string {
	if env := os.Getenv("BUD_OVERLAY"); env != "" {
		return env
	}

	return configured
}

// OverlayOnDisk is the overlay clone when repo names an overlay and it's
// been cloned, else empty
func OverlayOnDisk(p *paths.Paths, repo string) string {
	if repo == "" {
		return ""
	}
	if _, err := os.Stat(p.Overlay()); err != nil {
		return ""
	}

	return p.Overlay()
}

// Drift is a file the ledger records whose bytes on disk aren't what bud
// wrote
type Drift struct {
	Path string
	// Missing is true when the file is gone; otherwise it was changed
	Missing bool
}

// Drift checks every file the ledger records against its hash, in the
// ledger's order
func (l *Ledger) Drift() ([]Drift, error) {
	var out []Drift
	for _, f := range l.Files {
		sum, exists, err := hashFile(f.Path)
		if err != nil {
			return nil, err
		}
		switch {
		case !exists:
			out = append(out, Drift{Path: f.Path, Missing: true})
		case sum != f.SHA256:
			out = append(out, Drift{Path: f.Path})
		}
	}

	return out, nil
}

// CurrentLink is what the content/current link points at, or empty when
// there's no link
func CurrentLink(p *paths.Paths) string { return readLink(p.ContentCurrent()) }

// LoadRegistry reads the content dir's env/registry.jsonc, merges the
// overlay's over it when overlayDir has one, and validates the result,
// the same registry a sync renders from
func LoadRegistry(dir, overlayDir string) (*registry.Registry, error) {
	return loadRegistry(dir, overlayDir)
}

// Check works out what a sync would change if the content version in use
// were synced again now, with the overlay as it is on disk: nothing is
// fetched and nothing changes. A change it reports means the files on disk
// aren't what this bud renders, say after an overlay pull or a bud update
// changed the adapter. o.DryRun is ignored, since Check never writes
func Check(o Options) (*Report, error) {
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		return nil, err
	}
	if led.Content == "" {
		return nil, errors.New("bud sync hasn't run on this device, so there's nothing to check; run bud sync")
	}
	if o.Device == "" {
		return nil, ErrNoDevice
	}
	o.DryRun = true
	prof, _, err := profile.Find(o.Device, o.Profiles()...)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(o.Paths.Content(), led.Content)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("content %s isn't on disk at %s; run bud sync to fetch it again", led.Content, dir)
	}
	next := &Ledger{
		Format: LedgerFormat, Device: prof.Name, Channel: prof.Channel, Content: led.Content,
		RolledBack: led.RolledBack, Overlay: led.Overlay,
	}
	rep := &Report{From: led.Content}

	return rep, apply(&o, led, next, prof, dir, OverlayOnDisk(o.Paths, o.Overlay), rep)
}

// FetchChannel downloads channel.json from mirror (GitHub when empty) and
// checks its signature
func FetchChannel(ctx context.Context, mirror string) (*content.Channel, error) {
	base, err := baseURL(mirror)
	if err != nil {
		return nil, err
	}

	return fetchChannel(ctx, base)
}

// HeldBack says whether offered is no newer than a version the ledger says
// a rollback left, so a sync stays where it is
func (l *Ledger) HeldBack(offered string) bool { return heldBack(l.RolledBack, offered) }
