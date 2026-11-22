package improve

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"jgarner.dev/bud/internal/registry"
)

// PublicRegistry and OverlayRegistry are the registry files a scan reads
// and proposes changes to, relative to each repo
const (
	PublicRegistry  = "content/env/registry.jsonc"
	OverlayRegistry = "env/registry.jsonc"
)

// Scan is what the weekly landscape scan found (Suite D129)
type Scan struct {
	At time.Time `json:"at"`
	// Candidates are listed models the registry doesn't have
	Candidates []Candidate `json:"candidates,omitempty"`
	// Gone are registry models their provider no longer lists
	Gone []string `json:"gone,omitempty"`
	// Failed are the providers whose lists couldn't be read, with why
	Failed   []string  `json:"failed,omitempty"`
	Runtimes []Runtime `json:"runtimes,omitempty"`
}

// Candidate is a model a provider lists that the registry lacks, with the
// roles whose needs it meets
type Candidate struct {
	Provider string          `json:"provider"`
	Name     string          `json:"name"`
	Context  int             `json:"context,omitempty"`
	Price    *registry.Price `json:"price,omitempty"`
	// Roles is nil when the provider doesn't report capabilities
	Roles   []string `json:"roles,omitempty"`
	Unknown bool     `json:"unknown_caps,omitempty"`
}

// Runtime is one pinned runtime against its newest release
type Runtime struct {
	Name   string `json:"name"`
	Pinned string `json:"pinned"`
	Latest string `json:"latest"`
	// Newer is true when the newest release isn't the pin
	Newer bool   `json:"newer"`
	Error string `json:"error,omitempty"`
}

// ScanInput is what a scan reads
type ScanInput struct {
	// Public and Overlay are the registry files' text; Overlay is empty
	// when the overlay has none
	Public, Overlay string
	// List fetches a provider's model list
	List func(ctx context.Context, id string, p registry.Provider) ([]registry.Listing, error)
	// Runtimes are the pins to check, each with how to read its newest
	// release
	Runtimes []RuntimePin
}

// RuntimePin is a runtime bud pins and where its releases are
type RuntimePin struct {
	Name, Pinned string
	Latest       func(ctx context.Context) (tag, name string, err error)
}

// RunScan rechecks every provider's models and the runtime pins. Facts that
// drifted become one registry proposal per file they live in; the rest goes
// in the Scan for the digest
func RunScan(ctx context.Context, in ScanInput, now time.Time) (Scan, []Proposal, error) {
	pub, err := registry.Parse([]byte(in.Public))
	if err != nil {
		return Scan{}, nil, fmt.Errorf("read %s: %w", PublicRegistry, err)
	}
	var top *registry.Registry
	if in.Overlay != "" {
		if top, err = registry.Parse([]byte(in.Overlay)); err != nil {
			return Scan{}, nil, fmt.Errorf("read the overlay's %s: %w", OverlayRegistry, err)
		}
	}
	reg, err := registry.Merge(pub, top)
	if err != nil {
		return Scan{}, nil, err
	}

	sc := Scan{At: now.UTC()}
	changed := map[string]map[string]registry.Model{Public: {}, Overlay: {}}
	var drift []string
	ids := make([]string, 0, len(reg.Providers))
	for id := range reg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		list, err := in.List(ctx, id, reg.Providers[id])
		if err != nil {
			sc.Failed = append(sc.Failed, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		rep := registry.Compare(reg, id, list)
		sc.Gone = append(sc.Gone, rep.Gone...)
		byName := map[string]registry.Listing{}
		for _, l := range list {
			byName[l.ID] = l
		}
		for _, name := range rep.New {
			sc.Candidates = append(sc.Candidates, candidate(reg, id, byName[name]))
		}
		for _, d := range rep.Drift {
			m := reg.Models[d.Model]
			where := Public
			if top != nil {
				if _, ok := top.Models[d.Model]; ok {
					where = Overlay
				}
			}
			changed[where][d.Model] = registry.ApplyFacts(m, *byName[m.Name].Facts)
			was := d.Was
			if was == "" {
				was = "unset"
			}
			drift = append(drift, fmt.Sprintf("%s %s.%s: %s -> %s", where, d.Model, d.Field, was, d.Now))
		}
	}
	for _, rp := range in.Runtimes {
		r := Runtime{Name: rp.Name, Pinned: rp.Pinned}
		tag, name, err := rp.Latest(ctx)
		if err != nil {
			r.Error = err.Error()
		} else {
			r.Latest = strings.TrimSpace(tag + " " + name)
			r.Newer = !strings.Contains(" "+tag+" "+name+" ", rp.Pinned)
		}
		sc.Runtimes = append(sc.Runtimes, r)
	}
	sort.Strings(sc.Gone)

	var out []Proposal
	for _, where := range []string{Public, Overlay} {
		if len(changed[where]) == 0 {
			continue
		}
		src, file := in.Public, PublicRegistry
		if where == Overlay {
			src, file = in.Overlay, OverlayRegistry
		}
		after, err := registry.PatchFacts([]byte(src), changed[where])
		if err != nil {
			return sc, nil, err
		}
		var lines []string
		for _, l := range drift {
			if rest, ok := strings.CutPrefix(l, where+" "); ok {
				lines = append(lines, rest)
			}
		}
		ms := make([]string, 0, len(changed[where]))
		for id := range changed[where] {
			ms = append(ms, id)
		}
		sort.Strings(ms)
		out = append(out, Proposal{
			Title:    "Refresh registry facts for " + strings.Join(ms, ", "),
			Evidence: "The providers' model lists now report:\n" + strings.Join(lines, "\n"),
			Target:   where,
			Files:    []File{{Path: file, Before: strp(src), After: strp(string(after))}},
			Criteria: FactCriteria,
			Source:   "scan",
		})
	}

	return sc, out, nil
}

// FactCriteria are what a registry facts refresh must keep: top-up spend
// within a quarter (plus a dime a day) of the week before, and no new
// doctor failure on the server
var FactCriteria = []Criterion{
	{Metric: "spend.topup", MaxRise: 0.25, Slack: 0.10},
	{Metric: "doctor.fail"},
}

// candidate describes a listed model the registry lacks
func candidate(reg *registry.Registry, provider string, l registry.Listing) Candidate {
	c := Candidate{Provider: provider, Name: l.ID}
	if l.Facts == nil {
		c.Unknown = true
		return c
	}
	c.Context, c.Price = l.Facts.Context, l.Facts.Price
	m := registry.Model{Inputs: l.Facts.Inputs, Caps: l.Facts.Caps}
	for name, role := range reg.Roles {
		if !slices.ContainsFunc(role.Needs, func(n string) bool { return !m.Has(n) }) {
			c.Roles = append(c.Roles, name)
		}
	}
	sort.Strings(c.Roles)

	return c
}
