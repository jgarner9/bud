package curator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"jgarner.dev/bud/internal/kb"
)

// staleness works out each section's clock and flags, refreshes or
// archives the ones nothing has touched (Suite D124). It also drops queue
// items Jason settled by editing the page himself
func (c *Curator) staleness(ctx context.Context, refs map[int64]string, sum *Summary) error {
	if err := c.pruneQueue(ctx); err != nil {
		return err
	}
	pages, err := c.Repo.Pages()
	if err != nil {
		return err
	}
	now := c.Now().UTC()
	review := now.AddDate(0, -reviewAfter, 0).Format(time.DateOnly)
	archive := now.AddDate(0, -archiveAfter, 0).Format(time.DateOnly)
	for _, rel := range pages {
		p, err := c.Repo.ReadPage(rel)
		if err != nil {
			return err
		}
		human, err := c.lastDay(ctx, rel, c.Repo.LastHumanEdit)
		if err != nil {
			return err
		}
		created, err := c.lastDay(ctx, rel, c.Repo.LastCommit)
		if err != nil {
			return err
		}
		q, err := c.Repo.ReadQueue()
		if err != nil {
			return err
		}
		var notes []string
		queued := false
		for _, s := range p.Sections() {
			clock := sectionClock(p, s.Slug, refs, human)
			if clock == "" {
				clock = created
			}
			meta := p.Meta.Sections[s.Slug]
			status := ""
			if meta != nil {
				status = meta.Status
			}
			evergreen := p.Meta.Evergreen || meta != nil && meta.Evergreen
			switch {
			case clock < archive && evergreen:
				if status != kb.StatusRefresh {
					section(p, s.Slug).Status = kb.StatusRefresh
					notes = append(notes, fmt.Sprintf("%s is evergreen and due its yearly check (nothing since %s)", s.Slug, clock))
					sum.Flagged++
				}
				id := kb.ItemID(kb.ItemRefresh, rel, s.Slug)
				if q.Find(id) == nil {
					q.Items = append(q.Items, kb.Item{ID: id, Kind: kb.ItemRefresh, Page: rel, Section: s.Slug, Opened: c.today(),
						Note: fmt.Sprintf("Nothing has confirmed this section since %s. Is it still right?", clock)})
					queued = true
					sum.Queued++
				}
			case clock < archive:
				if err := c.archive(ctx, rel, p, s, clock); err != nil {
					return err
				}
				sum.Archived++
				// the page changed under us: read it again for the next section
				return c.staleness(ctx, refs, sum)
			case clock < review:
				if status != kb.StatusReview {
					section(p, s.Slug).Status = kb.StatusReview
					notes = append(notes, fmt.Sprintf("%s for review (nothing since %s)", s.Slug, clock))
					sum.Flagged++
				}
			case status != "":
				meta.Status = ""
				notes = append(notes, fmt.Sprintf("%s is current again (%s)", s.Slug, clock))
				if status == kb.StatusRefresh {
					if q.Find(kb.ItemID(kb.ItemRefresh, rel, s.Slug)) != nil {
						q.Remove(kb.ItemID(kb.ItemRefresh, rel, s.Slug))
						queued = true
					}
				}
			}
		}
		if len(notes) == 0 && !queued {
			continue
		}
		paths := []string{rel}
		if err := c.Repo.WritePage(rel, p); err != nil {
			return err
		}
		if queued {
			if err := c.Repo.WriteQueue(q); err != nil {
				return err
			}
			paths = append(paths, kb.QueuePath)
		}
		dir, slug := pageID(rel)
		subject := fmt.Sprintf("curator: staleness on %s/%s", dir, slug)
		if len(notes) > 0 {
			subject += "\n\n- " + strings.Join(notes, "\n- ")
		}
		if err := c.Repo.Commit(ctx, subject, paths...); err != nil {
			return err
		}
	}

	return nil
}

// lastDay is the day of a git date lookup for rel, or "" for none
func (c *Curator) lastDay(ctx context.Context, rel string, f func(context.Context, string) (time.Time, error)) (string, error) {
	t, err := f(ctx, rel)
	if err != nil || t.IsZero() {
		return "", err
	}

	return t.UTC().Format(time.DateOnly), nil
}

// sectionClock is the newest of: the section's referenced day, its facts'
// updated days, references to the records behind its facts, and Jason's
// last commit to the page
func sectionClock(p *kb.Page, slug string, refs map[int64]string, human string) string {
	clock := human
	if s := p.Meta.Sections[slug]; s != nil {
		clock = max(clock, s.Referenced)
	}
	for _, f := range p.Meta.Facts {
		if f.Section != slug {
			continue
		}
		clock = max(clock, f.Updated)
		for _, src := range f.Sources {
			clock = max(clock, refs[src.Record])
		}
	}

	return clock
}

// archive moves one section to the page's archive page, in its own commit,
// so git revert, or bud curator restore, brings it back
func (c *Curator) archive(ctx context.Context, rel string, p *kb.Page, s kb.BodySection, clock string) error {
	dir, slug := pageID(rel)
	arel := kb.ArchivePath(dir, slug)
	a, err := c.Repo.ReadPage(arel)
	if errors.Is(err, fs.ErrNotExist) {
		a = kb.NewPage(p.Meta.Title+" (archived)", p.Meta.Kind)
		a.Meta.Archived = true
	} else if err != nil {
		return err
	}
	moveSection(p, a, s.Slug)
	if err := c.Repo.WritePage(rel, p); err != nil {
		return err
	}
	if err := c.Repo.WritePage(arel, a); err != nil {
		return err
	}
	q, err := c.Repo.ReadQueue()
	if err != nil {
		return err
	}
	paths := []string{rel, arel}
	if dropItems(q, rel, s.Slug) {
		if err := c.Repo.WriteQueue(q); err != nil {
			return err
		}
		paths = append(paths, kb.QueuePath)
	}
	msg := fmt.Sprintf("curator: archive %s/%s#%s\n\nNothing has referenced it since %s. Bring it back with git revert of this commit, or bud curator restore %s/%s#%s.",
		dir, slug, s.Slug, clock, dir, slug, s.Slug)

	return c.Repo.Commit(ctx, msg, paths...)
}

// moveSection moves a section's text, front matter and facts from one page to another
func moveSection(from, to *kb.Page, slug string) {
	text, _ := from.CutSection(slug)
	to.AppendSection(text)
	if m := from.Meta.Sections[slug]; m != nil {
		m.Status = ""
		to.Meta.Sections[slug] = m
		delete(from.Meta.Sections, slug)
	}
	for k, f := range from.Meta.Facts {
		if f.Section == slug {
			to.Meta.Facts[k] = f
			delete(from.Meta.Facts, k)
		}
	}
}

// dropItems removes the queue's items about one section, reporting whether any went
func dropItems(q *kb.Queue, rel, slug string) bool {
	n := len(q.Items)
	out := q.Items[:0]
	for _, it := range q.Items {
		if it.Page != rel || it.Section != slug {
			out = append(out, it)
		}
	}
	q.Items = out

	return len(out) != n
}

// pruneQueue drops contradictions Jason settled by editing the page: the
// fact is gone, or no longer holds the value the item quoted
func (c *Curator) pruneQueue(ctx context.Context) error {
	q, err := c.Repo.ReadQueue()
	if err != nil {
		return err
	}
	var gone []string
	for _, it := range q.Items {
		if it.Kind != kb.ItemContradiction {
			continue
		}
		k, err := kb.ParseKey(it.Key)
		if err != nil || k.Path() != it.Page {
			gone = append(gone, it.ID)
			continue
		}
		p, err := c.Repo.ReadPage(it.Page)
		if errors.Is(err, fs.ErrNotExist) {
			gone = append(gone, it.ID)
			continue
		}
		if err != nil {
			return err
		}
		f := p.Meta.Facts[k.Local()]
		if f == nil || it.Current == nil || f.Value != it.Current.Value {
			gone = append(gone, it.ID)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	for _, id := range gone {
		q.Remove(id)
	}
	if err := c.Repo.WriteQueue(q); err != nil {
		return err
	}

	return c.Repo.Commit(ctx, "curator: drop "+strings.Join(gone, ", ")+" from the review queue\n\nThe page was edited since, which settles them.", kb.QueuePath)
}
