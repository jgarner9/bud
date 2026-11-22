// Package improve is Bud's self-improvement loop (Suite D47, D125 to D129):
// it measures, scans the model landscape, keeps a queue of proposed changes
// with their evidence and success criteria, asks Jason about them in one
// weekly digest, opens the ones he wants as draft pull requests, follows
// them through the edge and stable channels, and proposes a revert when a
// merged change made its numbers worse. Nothing here applies a change: a
// proposal reaches devices only when Jason merges its pull request. See
// docs/internals/improve.md
package improve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
)

// Targets a proposal can change
const (
	// Public is jgarner9/bud's content/, which ships through the content
	// channels
	Public = "public"
	// Overlay is the private overlay, which reaches every device at its
	// next sync
	Overlay = "overlay"
)

// Statuses a proposal moves through
const (
	// New is queued and not yet in a digest Jason answered
	New = "new"
	// Open has a draft pull request waiting on Jason
	Open = "open"
	// Merged is merged and waiting for its check
	Merged = "merged"
	// Passed was merged and its numbers held
	Passed = "passed"
	// Regressed was merged and made a number worse; a revert was proposed
	Regressed = "regressed"
	// Denied is one Jason said no to in a digest
	Denied = "denied"
	// Closed is a pull request closed without merging
	Closed = "closed"
	// Stale changes files that moved on since it was written, so its
	// pull request couldn't be opened
	Stale = "stale"
)

// File is one file a proposal changes: its whole contents before and after.
// A nil Before means the file is new; a nil After deletes it
type File struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// Criterion says how to tell a change worked: over the week after the
// merge, the metric's daily average may be at most the week before's times
// 1+MaxRise, plus Slack
type Criterion struct {
	Metric  string  `json:"metric"`
	MaxRise float64 `json:"max_rise"`
	Slack   float64 `json:"slack"`
}

// Proposal is one change Bud would like Jason to make
type Proposal struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Evidence string      `json:"evidence"`
	Target   string      `json:"target"`
	Files    []File      `json:"files"`
	Criteria []Criterion `json:"criteria"`
	// Source names what wrote it: scan, check or hand
	Source  string    `json:"source"`
	Created time.Time `json:"created"`
	Status  string    `json:"status"`
	// Reverts is the ID of the proposal this one undoes
	Reverts string `json:"reverts,omitempty"`

	PR       int       `json:"pr,omitempty"`
	PRURL    string    `json:"pr_url,omitempty"`
	MergedAt time.Time `json:"merged_at,omitzero"`
	MergeSHA string    `json:"merge_sha,omitempty"`
	// Edge and Stable are the first content versions on each channel that
	// hold the merge
	Edge   string `json:"edge,omitempty"`
	Stable string `json:"stable,omitempty"`
	// Check is what the before and after comparison found
	Check []Result `json:"check,omitempty"`
	// Note says why it went stale or couldn't be checked
	Note string `json:"note,omitempty"`
}

// Fingerprint names the change itself, so the same change is never queued
// twice, whatever its title or evidence
func (p Proposal) Fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", p.Target)
	files := slices.Clone(p.Files)
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	for _, f := range files {
		fmt.Fprintf(h, "%s\n%s\n%s\n", f.Path, side(f.Before), side(f.After))
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

func side(s *string) string {
	if s == nil {
		return "-"
	}
	sum := sha256.Sum256([]byte(*s))
	return hex.EncodeToString(sum[:])
}

// Metrics a criterion may name: spend in dollars a day, doctor counts as
// levels. spend.model:<id> is one registry model's spend
var metricRe = regexp.MustCompile(`^(spend\.total|spend\.topup|spend\.model:[a-z0-9][a-z0-9._-]*|doctor\.fail|doctor\.warn)$`)

// Validate checks a proposal before it is queued: its target, its paths
// (public ones only under content/), that it changes something, its
// criteria, and that a public one holds no personal detail
func (p Proposal) Validate(leaks *Leaks) error {
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Evidence) == "" {
		return errors.New("a proposal needs a title and evidence")
	}
	if p.Target != Public && p.Target != Overlay {
		return fmt.Errorf("target %q: use %q or %q", p.Target, Public, Overlay)
	}
	if len(p.Files) == 0 {
		return errors.New("a proposal changes at least one file")
	}
	seen := map[string]bool{}
	for _, f := range p.Files {
		if f.Path == "" || path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || strings.HasPrefix(f.Path, "../") || f.Path == ".." {
			return fmt.Errorf("file %q: give a clean path relative to the repo", f.Path)
		}
		if f.Path == ".git" || strings.HasPrefix(f.Path, ".git/") || strings.HasPrefix(f.Path, ".github/") {
			return fmt.Errorf("file %q: proposals don't change git or CI files", f.Path)
		}
		if p.Target == Public && !strings.HasPrefix(f.Path, "content/") {
			return fmt.Errorf("file %q: a public proposal changes only content/", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("file %q is listed twice", f.Path)
		}
		seen[f.Path] = true
		if f.Before == nil && f.After == nil || f.Before != nil && f.After != nil && *f.Before == *f.After {
			return fmt.Errorf("file %q doesn't change", f.Path)
		}
	}
	for _, c := range p.Criteria {
		if !metricRe.MatchString(c.Metric) {
			return fmt.Errorf("criterion metric %q: use spend.total, spend.topup, spend.model:<id>, doctor.fail or doctor.warn", c.Metric)
		}
		if c.MaxRise < 0 || c.Slack < 0 {
			return fmt.Errorf("criterion %s: max_rise and slack can't be negative", c.Metric)
		}
	}
	if p.Target == Public {
		texts := []string{p.Title, p.Evidence}
		for _, f := range p.Files {
			texts = append(texts, f.Path)
			if f.After != nil {
				texts = append(texts, *f.After)
			}
		}
		if what := leaks.Find(texts...); what != "" {
			return fmt.Errorf("a public proposal can't hold %s; target the overlay instead", what)
		}
	}

	return nil
}

// State is everything the loop keeps, in one file
type State struct {
	Proposals []Proposal `json:"proposals"`
	// Dropped are the fingerprints of changes Jason said no to, or whose
	// pull request he closed, so they are never proposed again
	Dropped []string `json:"dropped"`
	// Snapshots are the daily measurements, oldest first
	Snapshots []Snapshot `json:"snapshots"`
	Digest    Digest     `json:"digest"`
	// Scan is the last landscape scan
	Scan *Scan `json:"scan,omitempty"`
}

// Digest is when the weekly digest was last asked and answered
type Digest struct {
	Asked    time.Time `json:"asked,omitzero"`
	Answered time.Time `json:"answered,omitzero"`
	// Shown are the proposal IDs the last answered digest showed, and
	// Reported the regressions and promotions it named, so a quiet week
	// asks nothing
	Reported []string `json:"reported,omitempty"`
}

// Load reads the state file; a missing one is an empty state
func Load(file string) (*State, error) {
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the improve state: %w", err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("read %s: %w (move it aside to start over)", file, err)
	}

	return &s, nil
}

// Save writes the state file atomically, mode 0600, since overlay
// proposals hold private file contents
func (s *State) Save(file string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(file, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write the improve state: %w", err)
	}

	return nil
}

// Add validates p and queues it as new, with an ID, unless the same change
// is queued already or was dropped. added is false for a duplicate
func (s *State) Add(p Proposal, leaks *Leaks, now time.Time) (id string, added bool, err error) {
	if err := p.Validate(leaks); err != nil {
		return "", false, err
	}
	fp := p.Fingerprint()
	if slices.Contains(s.Dropped, fp) {
		return "", false, nil
	}
	for _, q := range s.Proposals {
		if q.Fingerprint() == fp && q.Status != Stale {
			return q.ID, false, nil
		}
	}
	day := now.UTC().Format("20060102")
	n := 1
	for _, q := range s.Proposals {
		if strings.HasPrefix(q.ID, "p-"+day+"-") {
			n++
		}
	}
	p.ID = fmt.Sprintf("p-%s-%d", day, n)
	p.Created = now.UTC()
	p.Status = New
	p.PR, p.PRURL, p.MergedAt, p.MergeSHA, p.Edge, p.Stable, p.Check, p.Note = 0, "", time.Time{}, "", "", "", nil, ""
	s.Proposals = append(s.Proposals, p)

	return p.ID, true, nil
}

// Find returns the proposal with id, or nil
func (s *State) Find(id string) *Proposal {
	for i := range s.Proposals {
		if s.Proposals[i].ID == id {
			return &s.Proposals[i]
		}
	}
	return nil
}

// drop remembers a change so it isn't proposed again
func (s *State) drop(p *Proposal) {
	if fp := p.Fingerprint(); !slices.Contains(s.Dropped, fp) {
		s.Dropped = append(s.Dropped, fp)
	}
}

// Revert is the proposal that undoes p: each file's before and after
// swapped
func Revert(p Proposal, why string) Proposal {
	r := Proposal{
		Title:    "Revert: " + p.Title,
		Evidence: why,
		Target:   p.Target,
		Criteria: p.Criteria,
		Source:   "check",
		Reverts:  p.ID,
	}
	for _, f := range p.Files {
		r.Files = append(r.Files, File{Path: f.Path, Before: f.After, After: f.Before})
	}

	return r
}

func strp(s string) *string { return &s }
