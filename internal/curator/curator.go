// Package curator promotes approved working-memory records into bud-kb's
// pages, queues contradictions and refresh checks for Jason, and flags and
// archives sections nothing has referenced for months (Suite D9, D123,
// D124). It reads memory through the service's export action, asks a local
// model only, and commits each change with its evidence. See
// docs/internals/kb.md
package curator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"time"

	"jgarner.dev/bud/internal/kb"
	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/registry"
)

// Fact is one fact the model read in a record
type Fact struct {
	// Key is <projects|systems>/<page>#<section>/<name>
	Key       string `json:"key"`
	Value     string `json:"value"`
	Statement string `json:"statement"`
	// Page and Section are titles for a page or section that doesn't exist yet
	Page    string `json:"page"`
	Section string `json:"section"`
}

// ErrNoModel means no local model answered; extraction waits for the next run
var ErrNoModel = errors.New("no local curator model answered")

// Extractor reads the facts in one record. known lists the KB's facts as
// "key = value" lines, so the model reuses keys
type Extractor func(ctx context.Context, rec memory.ExportedRecord, known []string) ([]Fact, error)

// Memory is the working-memory service, as the curator's token sees it
type Memory interface {
	Export(ctx context.Context) (*memory.Export, error)
}

// Curator runs one night's work over one KB clone
type Curator struct {
	Repo    kb.Repo
	Memory  Memory
	Extract Extractor
	// State is the state file's path
	State string
	// Tiers are the privacy tiers it promotes: P1 and P2 while bud-kb's
	// remote is GitHub, so P0 stays in the house (Suite D123)
	Tiers []string
	Now   func() time.Time
	Logf  func(string, ...any)
}

// Staleness thresholds (Suite D124)
const (
	reviewAfter  = 6  // months with nothing: the section is flagged for review
	archiveAfter = 12 // months: archived, or a refresh check when evergreen
)

// maxFailures is how often a record may fail extraction before it's skipped
const maxFailures = 3

func (c *Curator) today() string { return c.Now().UTC().Format(time.DateOnly) }

func (c *Curator) logf(f string, a ...any) {
	if c.Logf != nil {
		c.Logf(f, a...)
	}
}

// Summary is what one run did
type Summary struct {
	Read, Promoted, Confirmed, Changed, Queued, Flagged, Archived int
	// Waiting says why extraction stopped early, when it did
	Waiting string
}

func (s Summary) String() string {
	out := fmt.Sprintf("read %d records; %d promoted, %d confirmed, %d changed, %d queued for review, %d flagged, %d archived",
		s.Read, s.Promoted, s.Confirmed, s.Changed, s.Queued, s.Flagged, s.Archived)
	if s.Waiting != "" {
		out += "; " + s.Waiting
	}

	return out
}

// open takes the clone's lock, checks it's clean and pulls
func (c *Curator) open(ctx context.Context) (func(), error) {
	unlock, err := c.Repo.Lock()
	if err != nil {
		return nil, err
	}
	clean, err := c.Repo.Clean(ctx)
	if err == nil && !clean {
		err = fmt.Errorf("%s has uncommitted changes: commit or discard them, then run the curator again; nothing was changed", c.Repo.Dir)
	}
	if err == nil {
		err = c.Repo.Pull(ctx)
	}
	if err != nil {
		unlock()
		return nil, err
	}

	return unlock, nil
}

// Run reads new memory, promotes what has enough evidence, and does the
// staleness pass. Each change is its own commit; the clone is pushed at the end
func (c *Curator) Run(ctx context.Context) (Summary, error) {
	var sum Summary
	unlock, err := c.open(ctx)
	if err != nil {
		return sum, err
	}
	defer unlock()
	st, err := LoadState(c.State)
	if err != nil {
		return sum, err
	}
	exp, err := c.Memory.Export(ctx)
	if err != nil {
		return sum, fmt.Errorf("read working memory: %w", err)
	}
	records, refs := c.visible(exp)

	if err := c.extract(ctx, st, records, &sum); err != nil {
		return sum, err
	}
	if err := c.promote(ctx, st, &sum); err != nil {
		return sum, err
	}
	if err := c.staleness(ctx, refs, &sum); err != nil {
		return sum, err
	}
	if err := c.Repo.Push(ctx); err != nil {
		return sum, fmt.Errorf("%w: the commits stay in the clone and the next run pushes them", err)
	}

	return sum, nil
}

// visible keeps the approved records in the curator's tiers, and the days
// each record was last referenced
func (c *Curator) visible(exp *memory.Export) ([]memory.ExportedRecord, map[int64]string) {
	var recs []memory.ExportedRecord
	ok := map[int64]bool{}
	for _, r := range exp.Records {
		// the token's policy already limits tiers; this holds even when it's too wide
		if (r.State == "" || r.State == "approved") && slices.Contains(c.Tiers, r.Privacy) {
			recs = append(recs, r)
			ok[r.ID] = true
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	refs := map[int64]string{}
	for _, ref := range exp.References {
		if d := day(ref.ReferencedAt); ok[ref.RecordID] && d > refs[ref.RecordID] {
			refs[ref.RecordID] = d
		}
	}

	return recs, refs
}

// day is the YYYY-MM-DD of a service timestamp
func day(ts string) string {
	if len(ts) < 10 {
		return ""
	}

	return ts[:10]
}

// extract asks the model about each record it hasn't read yet
func (c *Curator) extract(ctx context.Context, st *State, records []memory.ExportedRecord, sum *Summary) error {
	known, err := c.known()
	if err != nil {
		return err
	}
	for _, r := range records {
		if st.Seen[r.ID] || st.Failed[r.ID] >= maxFailures {
			continue
		}
		facts, err := c.Extract(ctx, r, known)
		if errors.Is(err, ErrNoModel) {
			sum.Waiting = fmt.Sprintf("%v, so the other records wait for the next run", err)
			c.logf("curator: %s", sum.Waiting)
			break
		}
		if err != nil {
			st.Failed[r.ID]++
			c.logf("curator: record %d: %v (try %d of %d)", r.ID, err, st.Failed[r.ID], maxFailures)
			if err := st.Save(c.State); err != nil {
				return err
			}
			continue
		}
		occ := Occurrence{Record: r.ID, Source: r.Source, Date: day(r.CreatedAt), Privacy: r.Privacy}
		for _, f := range facts {
			k, err := kb.ParseKey(f.Key)
			if err != nil {
				c.logf("curator: record %d: %v; skipped", r.ID, err)
				continue
			}
			st.add(k.String(), f, occ)
		}
		st.Seen[r.ID] = true
		delete(st.Failed, r.ID)
		sum.Read++
		if err := st.Save(c.State); err != nil {
			return err
		}
	}

	return nil
}

// known lists the KB's facts for the model, as "key = value"
func (c *Curator) known() ([]string, error) {
	pages, err := c.Repo.Pages()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range pages {
		p, err := c.Repo.ReadPage(rel)
		if err != nil {
			return nil, err
		}
		dir, slug := pageID(rel)
		out = append(out, fmt.Sprintf("%s/%s is the page %q", dir, slug, p.Meta.Title))
		locals := make([]string, 0, len(p.Meta.Facts))
		for k := range p.Meta.Facts {
			locals = append(locals, k)
		}
		sort.Strings(locals)
		for _, l := range locals {
			sec, name, _ := strings.Cut(l, "/")
			out = append(out, fmt.Sprintf("%s/%s#%s/%s = %s", dir, slug, sec, name, p.Meta.Facts[l].Value))
		}
	}

	return out, nil
}

// pageID splits content/<dir>/<slug>.md
func pageID(rel string) (dir, slug string) {
	rest := strings.TrimSuffix(strings.TrimPrefix(rel, "content/"), ".md")
	dir, slug, _ = strings.Cut(rest, "/")

	return dir, slug
}

// promote acts on every candidate with enough fresh evidence, in key order
func (c *Curator) promote(ctx context.Context, st *State, sum *Summary) error {
	keys := make([]string, 0, len(st.Candidates))
	for k := range st.Candidates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, cand := range st.Candidates[key] {
			fresh := st.fresh(key, cand)
			if len(fresh) == 0 {
				continue
			}
			manual := slices.ContainsFunc(cand.Occurrences, func(o Occurrence) bool { return st.Promote[o.Record] })
			if independent(cand.Occurrences) < 2 && !manual {
				continue
			}
			if err := c.act(ctx, st, key, cand, sum); err != nil {
				return err
			}
		}
	}

	return nil
}

// act writes a promoted candidate: a new fact, a confirmation, a supported
// change, or a contradiction for the queue
func (c *Curator) act(ctx context.Context, st *State, key string, cand *Candidate, sum *Summary) error {
	k, _ := kb.ParseKey(key)
	rel := k.Path()
	p, err := c.Repo.ReadPage(rel)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if p == nil {
		p = kb.NewPage(orDefault(cand.Page, titleOf(k.Page)), k.Kind())
	}
	today := c.today()
	existing := p.Meta.Facts[k.Local()]
	if existing != nil {
		if _, ok := p.FactLine(k.Local()); !ok {
			// Jason deleted the bullet by hand: the fact starts over
			delete(p.Meta.Facts, k.Local())
			existing = nil
		}
	}

	var msg string
	paths := []string{rel}
	switch {
	case existing == nil:
		setFact(p, k, cand.Value, cand.Statement, cand.Section, cand.Occurrences, today)
		msg = fmt.Sprintf("curator: add %s = %s", key, cand.Value)
		sum.Promoted++
	case existing.Value == cand.Value:
		f := p.Meta.Facts[k.Local()]
		f.Sources = mergeSources(f.Sources, sources(cand.Occurrences))
		f.Updated = today
		f.Privacy = mostPrivate(f.Privacy, cand.Occurrences)
		section(p, k.Section).Referenced = today
		msg = fmt.Sprintf("curator: confirm %s = %s", key, cand.Value)
		sum.Confirmed++
	default:
		newer, err := c.supported(ctx, rel, existing, cand)
		if err != nil {
			return err
		}
		if newer {
			setFact(p, k, cand.Value, cand.Statement, cand.Section, cand.Occurrences, today)
			msg = fmt.Sprintf("curator: change %s from %s to %s", key, existing.Value, cand.Value)
			sum.Changed++
			break
		}
		q, err := c.Repo.ReadQueue()
		if err != nil {
			return err
		}
		id := kb.ItemID(kb.ItemContradiction, key, cand.Value)
		if q.Find(id) == nil {
			line, _ := p.FactLine(k.Local())
			q.Items = append(q.Items, kb.Item{
				ID: id, Kind: kb.ItemContradiction, Page: rel, Section: k.Section, Key: key,
				Current:  &kb.Claim{Value: existing.Value, Statement: line, Sources: existing.Sources},
				Proposed: &kb.Claim{Value: cand.Value, Statement: oneLine(cand.Statement), Sources: sources(cand.Occurrences)},
				Opened:   today,
			})
			if err := c.Repo.WriteQueue(q); err != nil {
				return err
			}
			if err := c.commit(ctx, fmt.Sprintf("curator: queue %s, memory says %s where the page says %s", key, cand.Value, existing.Value), cand.Occurrences, kb.QueuePath); err != nil {
				return err
			}
			sum.Queued++
		}
		st.use(key, cand.Value, cand.Occurrences)
		return st.Save(c.State)
	}

	if err := c.Repo.WritePage(rel, p); err != nil {
		return err
	}
	if err := c.commit(ctx, msg, cand.Occurrences, paths...); err != nil {
		return err
	}
	st.use(key, cand.Value, cand.Occurrences)

	return st.Save(c.State)
}

// supported says whether a new value may replace the page's: two
// independent occurrences, every one newer than the page's evidence
func (c *Curator) supported(ctx context.Context, rel string, existing *kb.Fact, cand *Candidate) (bool, error) {
	if independent(cand.Occurrences) < 2 {
		return false, nil
	}
	evidence := existing.Updated
	for _, s := range existing.Sources {
		evidence = max(evidence, s.Date)
	}
	human, err := c.Repo.LastHumanEdit(ctx, rel)
	if err != nil {
		return false, err
	}
	if !human.IsZero() {
		evidence = max(evidence, human.UTC().Format(time.DateOnly))
	}
	for _, o := range cand.Occurrences {
		if o.Date <= evidence {
			return false, nil
		}
	}

	return true, nil
}

// commit commits paths with the evidence in the message
func (c *Curator) commit(ctx context.Context, subject string, occ []Occurrence, paths ...string) error {
	var b strings.Builder
	b.WriteString(subject)
	if len(occ) > 0 {
		b.WriteString("\n\nEvidence:\n")
		for _, o := range occ {
			fmt.Fprintf(&b, "- memory record %d (%s, %s, %s)\n", o.Record, o.Source, o.Date, o.Privacy)
		}
	}

	return c.Repo.Commit(ctx, b.String(), paths...)
}

// setFact writes a fact's bullet and its front matter
func setFact(p *kb.Page, k kb.Key, value, statement, sectionTitle string, occ []Occurrence, today string) {
	if kb.Slug(sectionTitle) != k.Section {
		sectionTitle = titleOf(k.Section)
	}
	if strings.TrimSpace(statement) == "" {
		statement = value
	}
	p.SetFact(k.Local(), statement, k.Section, sectionTitle)
	p.Meta.Facts[k.Local()] = &kb.Fact{Section: k.Section, Value: value, Privacy: mostPrivate("", occ), Updated: today, Sources: sources(occ)}
	s := section(p, k.Section)
	s.Referenced, s.Status = today, ""
}

// section returns the front matter of a section, adding it when missing
func section(p *kb.Page, slug string) *kb.Section {
	s := p.Meta.Sections[slug]
	if s == nil {
		s = &kb.Section{}
		p.Meta.Sections[slug] = s
	}

	return s
}

func sources(occ []Occurrence) []kb.Source {
	out := make([]kb.Source, 0, len(occ))
	for _, o := range occ {
		out = append(out, kb.Source{Record: o.Record, Source: o.Source, Date: o.Date})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Record < out[j].Record })

	return out
}

func mergeSources(a, b []kb.Source) []kb.Source {
	out := slices.Clone(a)
	for _, s := range b {
		if !slices.ContainsFunc(out, func(x kb.Source) bool { return x.Record == s.Record }) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Record < out[j].Record })

	return out
}

// mostPrivate is the most private of tier and the occurrences' tiers
func mostPrivate(tier string, occ []Occurrence) string {
	for _, o := range occ {
		if tier == "" || registry.MorePrivate(o.Privacy, tier) {
			tier = o.Privacy
		}
	}

	return tier
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}

	return oneLine(s)
}

// titleOf makes a title from a slug: router-port is "Router port"
func titleOf(slug string) string {
	t := strings.ReplaceAll(slug, "-", " ")
	if t == "" {
		return t
	}

	return strings.ToUpper(t[:1]) + t[1:]
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
