package improve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/registry"
)

// Unmeasured is a merged proposal that never had enough numbers on both
// sides to check
const Unmeasured = "unmeasured"

// Label marks every pull request the loop opens. Only Jason merges them
const Label = "bud-proposal"

// Action is the approval action the weekly digest is asked under
const Action = "improve:digest"

// Scope is the fixed approval scope of the digest: the loop runs alone on
// the home server, with no session of its own
var Scope = approval.Scope{Runtime: "improve", Profile: "server", Session: "improve", Surface: "signal", Actor: "improve"}

// Loop is one home server's self-improvement loop. Round does a day's work
type Loop struct {
	// State is the state file (paths.ImproveState)
	State  string
	GitHub *GitHub
	// Public is jgarner9/bud; Overlay the private overlay's owner/name,
	// empty to scan and propose nothing there
	Public, Overlay string
	Leaks           *Leaks
	// Measure takes today's numbers; Day and Month are filled in after
	Measure func(ctx context.Context) Snapshot
	// List fetches a provider's model list
	List     func(ctx context.Context, id string, p registry.Provider) ([]registry.Listing, error)
	Runtimes []RuntimePin
	// Channel reads the signed channel.json
	Channel func(ctx context.Context) (*content.Channel, error)

	Approvals approval.Store
	Review    approval.Review
	// DigestDay and DigestHour are when the digest is asked, local time
	DigestDay  time.Weekday
	DigestHour int

	Logf func(format string, args ...any)
	Now  func() time.Time
}

// Round does one day's work: measure, scan once a week, follow open and
// merged proposals, and ask the weekly digest when it is due. Only the
// digest waits on Jason, for up to an hour
func (l *Loop) Round(ctx context.Context) error {
	now := l.now()
	var ask *digestAsk
	err := l.update(func(st *State) error {
		s := l.Measure(ctx)
		s.Day, s.Month = now.UTC().Format(time.DateOnly), now.UTC().Format("2006-01")
		st.Record(s, now)
		if st.Scan == nil || now.Sub(st.Scan.At) >= 6*24*time.Hour+12*time.Hour {
			if err := l.scan(ctx, st, now); err != nil {
				l.logf("improve: scan: %v", err)
			}
		}
		edge := l.follow(ctx, st, now)
		ask = l.due(st, now, edge)
		return nil
	})
	if err != nil || ask == nil {
		return err
	}
	return l.askDigest(ctx, ask)
}

// update runs fn on the state under an exclusive lock and saves it
func (l *Loop) update(fn func(*State) error) error {
	unlock, err := lockFile(l.State + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	st, err := Load(l.State)
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return st.Save(l.State)
}

// Update is update for callers outside the loop, such as bud improve add
func Update(file string, fn func(*State) error) error {
	return (&Loop{State: file}).update(fn)
}

func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("lock the improve state: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock the improve state: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock the improve state: %w", err)
	}
	return func() { f.Close() }, nil
}

// scan runs the landscape scan and queues what it proposes
func (l *Loop) scan(ctx context.Context, st *State, now time.Time) error {
	pubBranch, err := l.GitHub.DefaultBranch(ctx, l.Public)
	if err != nil {
		return err
	}
	pub, _, ok, err := l.GitHub.File(ctx, l.Public, PublicRegistry, pubBranch)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s has no %s", l.Public, PublicRegistry)
	}
	in := ScanInput{Public: pub, List: l.List, Runtimes: l.Runtimes}
	if l.Overlay != "" {
		ob, err := l.GitHub.DefaultBranch(ctx, l.Overlay)
		if err != nil {
			return fmt.Errorf("the overlay: %w", err)
		}
		if in.Overlay, _, _, err = l.GitHub.File(ctx, l.Overlay, OverlayRegistry, ob); err != nil {
			return fmt.Errorf("the overlay: %w", err)
		}
	}
	sc, props, err := RunScan(ctx, in, now)
	st.Scan = &sc
	if err != nil {
		return err
	}
	for _, p := range props {
		if p.Target == Overlay && l.Overlay == "" {
			continue
		}
		// a fact refresh replaces an older one still waiting, since both
		// patch the same file
		for i := range st.Proposals {
			q := &st.Proposals[i]
			if q.Status == New && q.Source == "scan" && q.Target == p.Target && len(q.Files) == 1 && q.Files[0].Path == p.Files[0].Path {
				q.Status, q.Note = Stale, "a newer scan replaced it"
			}
		}
		if id, added, err := st.Add(p, l.Leaks, now); err != nil {
			l.logf("improve: the scan's proposal was refused: %v", err)
		} else if added {
			l.logf("improve: queued %s: %s", id, p.Title)
		}
	}

	return nil
}

// follow reads each open pull request and each merged change's rollout,
// and checks merged changes whose week after is over. It returns what edge
// offers now, or ""
func (l *Loop) follow(ctx context.Context, st *State, now time.Time) (edgeNow string) {
	var ch *content.Channel
	if l.Channel != nil {
		var err error
		if ch, err = l.Channel(ctx); err != nil {
			l.logf("improve: read the channel: %v", err)
		} else {
			edgeNow = ch.Channels["edge"].Content
		}
	}
	for i := range st.Proposals {
		p := &st.Proposals[i]
		switch p.Status {
		case Open:
			pr, err := l.GitHub.PR(ctx, l.repo(p.Target), p.PR)
			if err != nil {
				l.logf("improve: read %s's pull request: %v", p.ID, err)
				continue
			}
			switch {
			case pr.Merged:
				p.Status, p.MergedAt, p.MergeSHA = Merged, pr.MergedAt, pr.MergeSHA
				l.logf("improve: %s was merged", p.ID)
			case pr.State == "closed":
				p.Status = Closed
				st.drop(p)
				l.logf("improve: %s's pull request was closed unmerged", p.ID)
			}
		}
		if p.Status != Merged && p.Status != Passed && p.Status != Regressed && p.Status != Unmeasured {
			continue
		}
		if p.Target == Public && ch != nil && p.MergeSHA != "" {
			l.rollout(ctx, p, ch)
		}
		if p.Status == Merged {
			l.check(st, p, now)
		}
	}

	return edgeNow
}

// rollout records the first edge and stable versions that hold p's merge
func (l *Loop) rollout(ctx context.Context, p *Proposal, ch *content.Channel) {
	for _, c := range []struct {
		name string
		at   *string
		tag  string
	}{{"edge", &p.Edge, "content-"}, {"stable", &p.Stable, "content-stable-"}} {
		v := ch.Channels[c.name].Content
		if *c.at != "" || v == "" {
			continue
		}
		in, err := l.GitHub.Contains(ctx, l.Public, c.tag+v, p.MergeSHA)
		if err != nil {
			l.logf("improve: find %s on %s: %v", p.ID, c.name, err)
			continue
		}
		if in {
			*c.at = v
			l.logf("improve: %s is on %s in content %s", p.ID, c.name, v)
		}
	}
}

// check compares a merged change's week before and after, once the week
// after is over, and proposes a revert when a number got worse
func (l *Loop) check(st *State, p *Proposal, now time.Time) {
	if len(p.Criteria) == 0 {
		p.Status, p.Note = Unmeasured, "it names no criteria"
		return
	}
	if now.Before(p.MergedAt.Add(Window + 24*time.Hour)) {
		return
	}
	res, ok := st.Compare(p.Criteria, p.MergedAt)
	if !ok {
		if now.After(p.MergedAt.Add(3 * Window)) {
			p.Status, p.Note = Unmeasured, "too few days of numbers on one side of the merge"
		}
		return
	}
	p.Check = res
	var worse []string
	for _, r := range res {
		if r.Worse {
			worse = append(worse, r.String())
		}
	}
	if len(worse) == 0 {
		p.Status = Passed
		l.logf("improve: %s held its numbers", p.ID)
		return
	}
	p.Status = Regressed
	why := fmt.Sprintf("%s (merged %s) made its numbers worse:\n%s", p.ID, p.MergedAt.UTC().Format(time.DateOnly), strings.Join(worse, "\n"))
	if id, _, err := st.Add(Revert(*p, why), l.Leaks, now); err != nil {
		l.logf("improve: couldn't propose reverting %s: %v", p.ID, err)
	} else {
		l.logf("improve: %s regressed; proposed %s to revert it", p.ID, id)
	}
}

func (l *Loop) repo(target string) string {
	if target == Overlay {
		return l.Overlay
	}
	return l.Public
}

// digestAsk is a digest that is due, with what it shows
type digestAsk struct {
	text     string
	shown    []string
	reported []string
}

// due returns the digest to ask now, or nil. It is due from DigestDay at
// DigestHour, a week after the last answer, at most once a day; a week with
// nothing new counts as answered without asking
func (l *Loop) due(st *State, now time.Time, edgeNow string) *digestAsk {
	local := now.In(time.Local)
	last := st.Digest.Answered
	if last.IsZero() {
		last = now.Add(-7 * 24 * time.Hour)
	}
	if now.Before(nextSlot(last, l.DigestDay, l.DigestHour)) {
		return nil
	}
	if a := st.Digest.Asked.In(time.Local); !st.Digest.Asked.IsZero() && a.Year() == local.Year() && a.YearDay() == local.YearDay() {
		return nil
	}
	d := Render(st, edgeNow)
	if len(d.Shown) == 0 && !slices.ContainsFunc(d.Reported, func(k string) bool { return !slices.Contains(st.Digest.Reported, k) }) {
		st.Digest.Answered = now
		return nil
	}
	st.Digest.Asked = now

	return &digestAsk{text: Fit(d.Text, d.Shown), shown: d.Shown, reported: d.Reported}
}

// Fit cuts the digest until its prompt fits approval.MaxSummary, which the
// plugin refuses rather than cut; the cut points to bud improve show
func Fit(text string, shown []string) string {
	const more = "\n...cut; bud improve show has the whole digest."
	fits := func(t string) bool {
		p := approval.Prompt{Invocation: approval.Invocation{Action: Action, Target: "github", Arguments: Arguments(t, shown)}, ExpiresAt: time.Now().Add(time.Hour)}
		return utf8.RuneCountInString(approval.Summary(p)) <= approval.MaxSummary
	}
	t, r := text, []rune(text)
	for !fits(t) && len(r) > 0 {
		r = r[:max(0, len(r)-200)]
		t = string(r) + more
	}
	return t
}

// nextSlot is the first DigestDay at DigestHour, local time, after t
func nextSlot(t time.Time, day time.Weekday, hour int) time.Time {
	lt := t.In(time.Local)
	s := time.Date(lt.Year(), lt.Month(), lt.Day(), hour, 0, 0, 0, time.Local)
	for !s.After(t) || s.Weekday() != day {
		s = s.AddDate(0, 0, 1)
	}
	return s
}

// Arguments is what the digest prompt shows and its approval binds
func Arguments(text string, shown []string) []byte {
	if shown == nil {
		shown = []string{}
	}
	b, _ := json.Marshal(map[string]any{"digest": text, "proposals": shown})
	return b
}

// askDigest asks Jason about the digest and acts on his answer: a yes opens
// each shown proposal as a draft pull request, his no drops them, and no
// answer leaves them for tomorrow's ask
func (l *Loop) askDigest(ctx context.Context, d *digestAsk) error {
	var opened []string
	err := l.Approvals.Dispatch(ctx, Scope, approval.Invocation{Action: Action, Target: "github", Arguments: Arguments(d.text, d.shown)}, time.Time{}, l.review(),
		func(ctx context.Context, _ approval.Invocation) error {
			return l.update(func(st *State) error {
				for _, id := range d.shown {
					p := st.Find(id)
					if p == nil || p.Status != New {
						continue
					}
					if err := l.open(ctx, p); err != nil {
						l.logf("improve: couldn't open %s: %v", id, err)
						continue
					}
					opened = append(opened, id)
				}
				answered(st, d, l.now())
				return nil
			})
		})
	switch {
	case err == nil:
		l.logf("improve: Jason said yes to the digest; opened %d pull request(s)", len(opened))
		return nil
	case errors.Is(err, approval.ErrDeclined):
		return l.update(func(st *State) error {
			for _, id := range d.shown {
				if p := st.Find(id); p != nil && p.Status == New {
					p.Status = Denied
					st.drop(p)
				}
			}
			answered(st, d, l.now())
			l.logf("improve: Jason said no to the digest; dropped %d proposal(s)", len(d.shown))
			return nil
		})
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		l.logf("improve: the digest went unanswered (%v); asking again tomorrow", err)
		return nil
	}
}

func answered(st *State, d *digestAsk, now time.Time) {
	st.Digest.Answered = now
	for _, k := range d.reported {
		if !slices.Contains(st.Digest.Reported, k) {
			st.Digest.Reported = append(st.Digest.Reported, k)
		}
	}
}

// review withdraws the prompt a minute before it expires, so the plugin's
// own deny at the expiry never races an answer
func (l *Loop) review() approval.Review {
	return func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
		ctx, cancel := context.WithDeadline(ctx, p.ExpiresAt.Add(-time.Minute))
		defer cancel()
		return l.Review(ctx, p)
	}
}

// open opens p as a draft pull request, once each file on the default
// branch is still what p was written against; otherwise p goes stale
func (l *Loop) open(ctx context.Context, p *Proposal) error {
	repo := l.repo(p.Target)
	if repo == "" {
		return errors.New("no overlay repo is set")
	}
	base, err := l.GitHub.DefaultBranch(ctx, repo)
	if err != nil {
		return err
	}
	shas := map[string]string{}
	for _, f := range p.Files {
		now, sha, found, err := l.GitHub.File(ctx, repo, f.Path, base)
		if err != nil {
			return err
		}
		if found != (f.Before != nil) || found && now != *f.Before {
			p.Status, p.Note = Stale, f.Path+" changed since the proposal was written"
			return nil
		}
		shas[f.Path] = sha
	}
	pr, err := l.GitHub.OpenPR(ctx, repo, base, "bud-proposal/"+p.ID, p.Title, Body(*p), Label, p.Files, shas)
	if err != nil {
		return err
	}
	p.Status, p.PR, p.PRURL = Open, pr.Number, pr.URL

	return nil
}

func (l *Loop) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Loop) logf(format string, args ...any) {
	if l.Logf != nil {
		l.Logf(format, args...)
	}
}
