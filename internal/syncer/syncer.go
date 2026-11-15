// Package syncer is bud sync (Suite D46, D57, D59, D60, D61): it brings a
// device's content and runtime config up to its channel. It reads the
// device profile, fetches the channel's newest signed content bundle and
// unpacks it into a dir of its own, layers the private overlay on top,
// points the current link at the version, and renders each runtime's
// config from the model registry and env fragments. A ledger records every
// file it wrote, so a file edited by hand, or never bud's, is skipped and
// named instead of overwritten. The details are in docs/internals/sync.md
package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/adapter/opencode"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/overlay"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
	"jgarner.dev/bud/internal/registry"
)

// Options is what a sync works from
type Options struct {
	Paths *paths.Paths
	// Device names the device profile
	Device string
	// Overlay is the overlay's git URL, or empty for none
	Overlay string
	// Mirror is the base URL content releases come from, or empty for GitHub
	Mirror string
	// Profiles lists where device profiles are read from, the overlay's
	// first; it's called once the overlay is fetched
	Profiles func() []profile.Dir
	// DryRun works out every change and makes none: the overlay isn't
	// fetched, and a new bundle is checked in a temp dir and thrown away
	DryRun bool
	Logf   func(format string, args ...any)
}

func (o *Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Report is what a sync did, or would do in a dry run
type Report struct {
	From, Content string // the content version before and after
	Overlay       string // the overlay commit layered, if any
	Changes       []Change
	// Skipped are files left alone, each with why and what to do
	Skipped []Change
	Notes   []string
}

// Changed says whether the sync changed anything
func (r *Report) Changed() bool { return len(r.Changes) > 0 }

// ErrNoDevice is returned when no device profile is set
var ErrNoDevice = errors.New("no device profile is set, so bud sync doesn't know what this device takes; set one with bud install --device <name> or bud config set device <name> (such as dev-machine)")

// Run syncs the device to its channel's newest content
func Run(ctx context.Context, o Options) (*Report, error) {
	if !o.DryRun {
		unlock, err := lock(o.Paths.SyncLedger())
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		return nil, err
	}
	if o.Device == "" {
		return nil, ErrNoDevice
	}
	base, err := baseURL(o.Mirror)
	if err != nil {
		return nil, err
	}
	rep := &Report{From: led.Content}

	overlayDir, commit, err := fetchOverlay(ctx, &o, led, rep)
	if err != nil {
		return nil, err
	}
	prof, _, err := profile.Find(o.Device, o.Profiles()...)
	if err != nil {
		return nil, err
	}

	o.logf("channel %s from %s", prof.Channel, base)
	ch, err := fetchChannel(ctx, base)
	if err != nil {
		return nil, err
	}
	offered := ch.Channels[prof.Channel].Content
	if offered == "" {
		return nil, fmt.Errorf("channel.json has no content on the %s channel yet; nothing changed", prof.Channel)
	}

	target := offered
	rolledBack := led.RolledBack
	switch {
	case offered == led.Content:
	case heldBack(led.RolledBack, offered) && led.Content != "":
		target = led.Content
		rep.Notes = append(rep.Notes, fmt.Sprintf("content %s isn't newer than a version that was rolled back, so this device stays on %s until a newer one is out", offered, led.Content))
	default:
		if err := content.CheckNewer(led.Content, offered); err != nil {
			return nil, err
		}
		rolledBack = nil
	}

	dir := filepath.Join(o.Paths.Content(), target)
	if _, err := os.Lstat(dir); err != nil {
		o.logf("downloading content %s", target)
		m, err := fetchManifest(ctx, base, target)
		if err != nil {
			return nil, err
		}
		if o.DryRun {
			tmp, err := os.MkdirTemp("", "bud-sync-")
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(tmp)
			dir = filepath.Join(tmp, target)
		}
		if err := fetchBundle(ctx, base, m, dir); err != nil {
			return nil, err
		}
	}

	next := &Ledger{Format: LedgerFormat, Device: prof.Name, Channel: prof.Channel, Content: target, RolledBack: rolledBack}
	if o.Overlay != "" {
		next.Overlay = &OverlayRef{Repo: o.Overlay, Commit: commit}
	}
	return rep, apply(&o, led, next, prof, dir, overlayDir, rep)
}

// Rollback switches the device back to the newest content version on disk
// older than the one in use, with the overlay as it is on disk. A later
// sync won't move to the version it left until a newer one is out
func Rollback(ctx context.Context, o Options) (*Report, error) {
	if !o.DryRun {
		unlock, err := lock(o.Paths.SyncLedger())
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		return nil, err
	}
	if led.Content == "" {
		return nil, errors.New("nothing to roll back: bud sync hasn't run on this device")
	}
	if o.Device == "" {
		return nil, ErrNoDevice
	}
	cur, err := content.ParseVersion(led.Content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", o.Paths.SyncLedger(), err)
	}
	var older *content.Version
	entries, err := os.ReadDir(o.Paths.Content())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		v, err := content.ParseVersion(e.Name())
		if err != nil || !e.IsDir() || v.Compare(cur) >= 0 {
			continue
		}
		if older == nil || v.Compare(*older) > 0 {
			older = &v
		}
	}
	if older == nil {
		return nil, fmt.Errorf("no content version older than %s is on disk to roll back to; nothing changed", led.Content)
	}

	rep := &Report{From: led.Content}
	overlayDir := ""
	if o.Overlay != "" {
		if _, err := os.Stat(o.Paths.Overlay()); err == nil {
			overlayDir = o.Paths.Overlay()
		}
	}
	prof, _, err := profile.Find(o.Device, o.Profiles()...)
	if err != nil {
		return nil, err
	}
	next := &Ledger{
		Format: LedgerFormat, Device: prof.Name, Channel: prof.Channel, Content: older.String(),
		RolledBack: append(slices.Clone(led.RolledBack), led.Content), Overlay: led.Overlay,
	}
	rep.Notes = append(rep.Notes, "the overlay is used as it is on disk; a rollback doesn't fetch it")

	return rep, apply(&o, led, next, prof, filepath.Join(o.Paths.Content(), older.String()), overlayDir, rep)
}

// fetchOverlay fast-forwards the overlay and returns its dir and commit;
// both are empty without one. A dry run uses the clone as it is
func fetchOverlay(ctx context.Context, o *Options, led *Ledger, rep *Report) (string, string, error) {
	if o.Overlay == "" {
		return "", "", nil
	}
	dir := o.Paths.Overlay()
	if o.DryRun {
		if _, err := os.Stat(dir); err != nil {
			rep.Notes = append(rep.Notes, "the overlay isn't cloned yet, and a dry run doesn't clone it, so this shows public content only")
			return "", "", nil
		}
		rep.Notes = append(rep.Notes, "a dry run uses the overlay as it is on disk, without fetching it")
		commit := ""
		if led.Overlay != nil {
			commit = led.Overlay.Commit
		}
		return dir, commit, nil
	}
	o.logf("fetching the overlay from %s", o.Overlay)
	commit, err := overlay.Fetch(ctx, o.Overlay, dir)
	if err != nil {
		return "", "", err
	}

	return dir, commit, nil
}

// apply layers content dir (version next.Content) with the overlay, works
// out the links and files the device should have, and makes them so,
// unless this is a dry run. The ledger is saved even when a step fails, so
// it records each file that was written
func apply(o *Options, led, next *Ledger, prof *profile.Profile, dir, overlayDir string, rep *Report) error {
	p := o.Paths
	rep.Content = next.Content
	if next.Overlay != nil {
		rep.Overlay = next.Overlay.Commit
	}
	versionDir := filepath.Join(p.Content(), next.Content)
	// paths into dir as they are once dir is in place (it's a temp dir in a
	// dry run)
	stable := func(path string) string {
		if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join(versionDir, rel)
		}
		return path
	}

	items, err := overlay.Layer(dir, overlayDir)
	if err != nil {
		return err
	}
	reg, err := loadRegistry(dir, overlayDir)
	if err != nil {
		return err
	}

	links := linkSet{}
	skills := 0
	var wants []want
	in := opencode.Input{Registry: reg}
	for _, it := range items {
		if it.Kind == "env" && it.ID == "registry" {
			continue // merged above, never a fragment
		}
		if it.Kind != "instructions" && !prof.Selects(it.Kind, it.ID) {
			continue
		}
		switch it.Kind {
		case "skills", "plugins":
			links[it.Kind+"/"+it.ID] = stable(it.Path)
			if it.Kind == "skills" {
				skills++
			} else {
				in.Plugins = append(in.Plugins, filepath.Join(p.Active(), "plugins", it.ID))
			}
		case "instructions":
			path := it.Path
			if it.Layer == overlay.Public {
				path = filepath.Join(p.ContentCurrent(), "AGENTS.md")
			}
			in.Instructions = append(in.Instructions, path)
		case "agents", "commands":
			b, err := os.ReadFile(it.Path)
			if err != nil {
				return err
			}
			wants = append(wants, want{filepath.Join(p.OpenCodeConfig(), it.Kind, it.ID+".md"), b, 0o644, it.Kind, it.Layer})
		case "env":
			b, err := os.ReadFile(it.Path)
			if err != nil {
				return err
			}
			in.Env = append(in.Env, opencode.Fragment{Name: it.Layer + "/" + it.ID, Src: b})
		}
	}
	if skills > 0 {
		in.Skills = []string{filepath.Join(p.Active(), "skills")}
	}
	if !prof.Has("harness") && !prof.Has("server") {
		// nothing on this device runs OpenCode, so it takes no config;
		// agents and commands are OpenCode's too
		wants = nil
		rep.Notes = append(rep.Notes, fmt.Sprintf("profile %s runs no OpenCode, so only content was synced", prof.Name))
	} else {
		out, err := opencode.Render(in)
		if err != nil {
			return err
		}
		wants = append(wants, want{filepath.Join(p.OpenCodeConfig(), "opencode.jsonc"), out.Config, 0o644, "config", "rendered"})
		if len(out.Secrets) > 0 {
			wants = append(wants, want{p.OpenCodeEnv(), envFile(out.Secrets), 0o600, "secrets", "rendered"})
		}
	}

	steps, keep, skips, err := planFiles(led.Files, wants)
	if err != nil {
		return err
	}
	rep.Skipped = skips
	if t := readLink(p.ContentCurrent()); t != next.Content {
		from := t
		if from == "" {
			from = "none"
		}
		rep.Changes = append(rep.Changes, Change{Switch, p.ContentCurrent(), "content " + from + " to " + next.Content})
	}
	next.Active = links.name()
	activeTarget := filepath.Join(activeDirs, next.Active)
	if readLink(p.Active()) != activeTarget {
		rep.Changes = append(rep.Changes, Change{Switch, p.Active(), fmt.Sprintf("%d skills and plugins", len(links))})
	}
	for _, s := range steps {
		rep.Changes = append(rep.Changes, s.Change)
	}
	if o.DryRun {
		return nil
	}

	next.Files = keep
	// build the link set before switching anything, and put current back if
	// active can't follow it, so the two never point at different versions
	contentDir := filepath.Join(p.Content(), next.Content)
	if fi, err := os.Stat(contentDir); err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("%s is not a content directory", contentDir)
	}
	if err := links.build(p.Active()); err != nil {
		return err
	}
	currentLink, activeLink := p.ContentCurrent(), p.Active()
	oldCurrent, oldActive := readLink(currentLink), readLink(activeLink)
	if err := switchLink(currentLink, next.Content); err != nil {
		return err
	}
	if err := switchLink(activeLink, activeTarget); err != nil {
		restoreErr := errors.Join(
			restoreLink(activeLink, oldActive, activeTarget),
			restoreLink(currentLink, oldCurrent, next.Content),
		)
		return errors.Join(err, restoreErr)
	}
	var errs []error
	for _, s := range steps {
		if err := s.apply(); err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", s.Action, s.Path, err))
			// the file is as it was, so its old entry, if any, still holds
			if i := slices.IndexFunc(led.Files, func(f File) bool { return f.Path == s.Path }); i >= 0 {
				next.Files = append(next.Files, led.Files[i])
			}
			continue
		}
		if s.Action == Write {
			next.Files = append(next.Files, s.entry)
		}
	}
	errs = append(errs, next.save(p.SyncLedger()))
	if err := prune(p.Active(), next.Active); err != nil {
		rep.Notes = append(rep.Notes, "couldn't clear out old link sets: "+err.Error())
	}

	return errors.Join(errs...)
}

// heldBack says whether offered is no newer than a version a rollback left,
// so a sync stays on the rollback's version: an offer has to be newer than
// every rolled-back version to be taken
func heldBack(rolledBack []string, offered string) bool {
	o, err := content.ParseVersion(offered)
	if err != nil {
		return false
	}
	for _, rb := range rolledBack {
		if v, err := content.ParseVersion(rb); err == nil && o.Compare(v) <= 0 {
			return true
		}
	}

	return false
}

// loadRegistry reads the public registry from content dir, merges the
// overlay's env/registry.jsonc over it when there is one, and validates it
func loadRegistry(dir, overlayDir string) (*registry.Registry, error) {
	reg, err := registry.Read(filepath.Join(dir, "env", "registry.jsonc"))
	if err != nil {
		return nil, err
	}
	name := "the content's env/registry.jsonc"
	if overlayDir != "" {
		path := filepath.Join(overlayDir, "env", "registry.jsonc")
		if _, err := os.Stat(path); err == nil {
			top, err := registry.Read(path)
			if err != nil {
				return nil, err
			}
			if reg, err = registry.Merge(reg, top); err != nil {
				return nil, err
			}
			name += " with the overlay's over it"
		}
	}
	if err := reg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return reg, nil
}

// envFile is the env file op run --env-file reads: each variable and the
// op:// reference op fills it from, sorted
func envFile(secrets map[string]string) []byte {
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("# Written by bud sync. op run --env-file fills these from 1Password; no value is stored here\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, secrets[k])
	}

	return []byte(b.String())
}
