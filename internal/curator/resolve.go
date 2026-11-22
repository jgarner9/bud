package curator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"jgarner.dev/bud/internal/kb"
)

// slugRe is a page or section slug, as in a fact key
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Answers to a review item
const (
	// Keep leaves the page as it is and counts as confirming the section
	Keep = "keep"
	// Take writes memory's value over the page's
	Take = "take"
	// Confirm says an evergreen section is still right
	Confirm = "confirm"
)

// Promote marks a record so the next run promotes its facts on its own
// occurrence: Jason's doc-commit (Suite D123)
func (c *Curator) Promote(id int64) error {
	// the clone's lock, so a run in progress can't save over this
	unlock, err := c.Repo.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := LoadState(c.State)
	if err != nil {
		return err
	}
	st.Promote[id] = true

	return st.Save(c.State)
}

// Resolve answers one review item and commits the answer
func (c *Curator) Resolve(ctx context.Context, id, answer string) error {
	unlock, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	q, err := c.Repo.ReadQueue()
	if err != nil {
		return err
	}
	it := q.Find(id)
	if it == nil {
		return fmt.Errorf("no review item %q: the site's home page lists them", id)
	}
	item := *it
	switch {
	case item.Kind == kb.ItemContradiction && (answer == Keep || answer == Take):
	case item.Kind == kb.ItemRefresh && (answer == Confirm || answer == Keep):
	case item.Kind == kb.ItemContradiction:
		return fmt.Errorf("%s is a contradiction: answer keep (the page is right) or take (memory is right)", id)
	default:
		return fmt.Errorf("%s is a refresh check: answer confirm (the section is still right), or edit the page yourself", id)
	}
	// the queue is committed data: its paths must name a page, and a
	// contradiction's page must be its fact's
	var k kb.Key
	if item.Kind == kb.ItemContradiction {
		if k, err = kb.ParseKey(item.Key); err != nil || k.Path() != item.Page || item.Current == nil || item.Proposed == nil {
			return fmt.Errorf("review item %s is malformed (key %q, page %q): fix or remove it in %s by hand", id, item.Key, item.Page, kb.QueuePath)
		}
	} else if dir, slug := pageID(item.Page); kb.PagePath(dir, slug) != item.Page || !slugRe.MatchString(slug) || !slugRe.MatchString(item.Section) {
		return fmt.Errorf("review item %s is malformed (page %q, section %q): fix or remove it in %s by hand", id, item.Page, item.Section, kb.QueuePath)
	}
	p, err := c.Repo.ReadPage(item.Page)
	if err != nil {
		return err
	}
	if item.Kind == kb.ItemContradiction {
		// the page changed since the item was queued: the item is stale
		if f := p.Meta.Facts[k.Local()]; f == nil || f.Value != item.Current.Value {
			return fmt.Errorf("%s is stale: the page no longer says %s; the next run drops it from the queue", id, item.Current.Value)
		}
	}
	today := c.today()
	var subject string
	switch answer {
	case Take:
		title := k.Section
		if s, ok := p.Section(k.Section); ok {
			title = s.Title
		}
		var occ []Occurrence
		for _, s := range item.Proposed.Sources {
			occ = append(occ, Occurrence{Record: s.Record, Source: s.Source, Date: s.Date, Privacy: item.Proposed.Privacy})
		}
		setFact(p, k, item.Proposed.Value, item.Proposed.Statement, title, occ, today)
		subject = fmt.Sprintf("curator: take %s = %s (Jason, review item %s)", item.Key, item.Proposed.Value, id)
	default:
		s := section(p, item.Section)
		s.Referenced, s.Status = today, ""
		subject = fmt.Sprintf("curator: Jason confirmed %s#%s (review item %s)", strings.TrimSuffix(strings.TrimPrefix(item.Page, "content/"), ".md"), item.Section, id)
	}
	q.Remove(id)
	if err := c.Repo.WritePage(item.Page, p); err != nil {
		return err
	}
	if err := c.Repo.WriteQueue(q); err != nil {
		return err
	}
	if err := c.Repo.Commit(ctx, subject, item.Page, kb.QueuePath); err != nil {
		return err
	}

	return c.Repo.Push(ctx)
}

// Restore moves an archived section back onto its page: target is
// <projects|systems>/<page>#<section>
func (c *Curator) Restore(ctx context.Context, target string) error {
	ref, sec, ok := strings.Cut(target, "#")
	dir, slug, ok2 := strings.Cut(ref, "/")
	// the same rules as a fact key, so the paths stay inside content/
	if !ok || !ok2 || kb.Kinds[strings.TrimSuffix(dir, "s")] != dir || !slugRe.MatchString(slug) || !slugRe.MatchString(sec) {
		return fmt.Errorf("%q isn't <projects|systems>/<page>#<section>, such as systems/archie#old-mail", target)
	}
	unlock, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	arel, rel := kb.ArchivePath(dir, slug), kb.PagePath(dir, slug)
	a, err := c.Repo.ReadPage(arel)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s has no archive page (%s)", ref, arel)
	}
	if err != nil {
		return err
	}
	if _, ok := a.Section(sec); !ok {
		return fmt.Errorf("%s has no archived section %q", arel, sec)
	}
	p, err := c.Repo.ReadPage(rel)
	if errors.Is(err, fs.ErrNotExist) {
		p = kb.NewPage(strings.TrimSuffix(a.Meta.Title, " (archived)"), a.Meta.Kind)
	} else if err != nil {
		return err
	}
	if _, ok := p.Section(sec); ok {
		return fmt.Errorf("%s already has a section %q: merge the archived one by hand", rel, sec)
	}
	moveSection(a, p, sec)
	s := section(p, sec)
	s.Referenced, s.Status = c.today(), ""
	if err := c.Repo.WritePage(rel, p); err != nil {
		return err
	}
	if len(a.Sections()) == 0 {
		if err := os.Remove(filepath.Join(c.Repo.Dir, filepath.FromSlash(arel))); err != nil {
			return err
		}
	} else if err := c.Repo.WritePage(arel, a); err != nil {
		return err
	}
	if err := c.Repo.Commit(ctx, fmt.Sprintf("curator: restore %s (Jason)", target), rel, arel); err != nil {
		return err
	}

	return c.Repo.Push(ctx)
}
