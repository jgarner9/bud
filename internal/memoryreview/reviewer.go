// Package memoryreview asks Jason on Signal about each memory record an
// agent staged (Suite D113): a yes approves it, so memory_search can find
// it, and his own no deletes it. It holds the memory service's reviewer
// token, which no agent-facing process does, and asks through the
// bud-signal plugin's reviewer socket like the gateway and ops (Suite D101,
// D103). See docs/internals/memory-review.md
package memoryreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/memory"
)

// Records is the memory service as the reviewer token sees it
type Records interface {
	Staged(ctx context.Context, after int64, limit int) ([]memory.Record, error)
	Approve(ctx context.Context, id int64) error
	Discard(ctx context.Context, id int64) error
}

// Action is the approval action a memory record is asked under
const Action = "memory:approve"

// RetierAction is the approval action a re-tier is asked under (Suite D136)
const RetierAction = "memory:retier"

// Retierer is the memory service as the reviewer token sees an approved
// record: it can list them, which is how a re-tier shows what it moves, and
// move one
type Retierer interface {
	Export(ctx context.Context) (*memory.Export, error)
	Retier(ctx context.Context, id int64, from, to, bodyHash string) error
}

// Scope is the fixed approval scope every memory prompt shares: the
// reviewer runs alone on the home server, with no session of its own
var Scope = approval.Scope{Runtime: "memory-review", Profile: "server", Session: "memory-review", Surface: "signal", Actor: "memory-review"}

// pageSize is how many staged records one listing asks for
const pageSize = 100

// Reviewer polls the staged records and asks about each
type Reviewer struct {
	Records   Records
	Approvals approval.Store
	Review    approval.Review
	// Status is the file the counts go to for the ops reviews tool; empty
	// writes none
	Status string
	// Interval is the wait between listings; a minute when 0
	Interval time.Duration
	// Retry is how long an unanswered record waits before it is asked
	// again; a day when 0
	Retry time.Duration
	// MaxAsking bounds the prompts waiting on Jason at once; 4 when 0
	MaxAsking int
	// Logf reports what was decided and what failed
	Logf func(format string, args ...any)
	// Now is the clock, swappable in tests
	Now func() time.Time

	mu      sync.Mutex
	asking  map[int64]bool
	quiet   map[int64]time.Time // not asked again before this time
	tooLong map[int64]bool
	wg      sync.WaitGroup
}

// Status is what the reviewer last saw, counts only: no ids or bodies
type Status struct {
	UpdatedAt time.Time      `json:"updated_at"`
	Pending   int            `json:"pending"`
	ByPrivacy map[string]int `json:"by_privacy"`
	// Asking is how many are on Jason's phone now
	Asking int `json:"asking"`
	// TooLong is how many can't be shown whole in a Signal prompt, so wait
	// to be approved by hand
	TooLong int `json:"too_long"`
	// Oldest is when the oldest waiting record was staged
	Oldest string `json:"oldest,omitempty"`
}

// Run lists and asks until ctx ends, then waits for the prompts it opened
// to be withdrawn
func (r *Reviewer) Run(ctx context.Context) {
	defer r.wg.Wait()
	for {
		if err := r.Round(ctx); err != nil && ctx.Err() == nil {
			r.logf("memory-review: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.interval()):
		}
	}
}

// Round lists every staged record, writes the counts and asks about each
// record that isn't waiting, resting or too long, up to MaxAsking at once.
// The asks run on after it returns
func (r *Reviewer) Round(ctx context.Context) error {
	var all []memory.Record
	var after int64
	for {
		page, err := r.Records.Staged(ctx, after, pageSize)
		if err != nil {
			return fmt.Errorf("list staged records: %w", err)
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
		after = page[len(page)-1].ID
	}

	now := r.now()
	r.mu.Lock()
	if r.asking == nil {
		r.asking, r.quiet, r.tooLong = map[int64]bool{}, map[int64]time.Time{}, map[int64]bool{}
	}
	waiting := map[int64]bool{}
	var ask []memory.Record
	for _, rec := range all {
		waiting[rec.ID] = true
		if r.asking[rec.ID] || r.tooLong[rec.ID] || now.Before(r.quiet[rec.ID]) {
			continue
		}
		args := Arguments(rec)
		if utf8.RuneCountInString(approval.Summary(approval.Prompt{Invocation: approval.Invocation{Action: Action, Target: "memory", Arguments: args}, ExpiresAt: now.Add(time.Hour)})) > approval.MaxSummary {
			r.tooLong[rec.ID] = true
			r.logf("memory-review: record %d is too long to show whole on Signal; approve it by hand", rec.ID)
			continue
		}
		if len(r.asking)+len(ask) < r.maxAsking() {
			ask = append(ask, rec)
		}
	}
	// forget records that were decided some other way
	for id := range r.quiet {
		if !waiting[id] {
			delete(r.quiet, id)
		}
	}
	for id := range r.tooLong {
		if !waiting[id] {
			delete(r.tooLong, id)
		}
	}
	for _, rec := range ask {
		r.asking[rec.ID] = true
	}
	st := Status{UpdatedAt: now.UTC(), Pending: len(all), ByPrivacy: map[string]int{}, Asking: len(r.asking), TooLong: len(r.tooLong)}
	r.mu.Unlock()

	for _, rec := range all {
		st.ByPrivacy[rec.Privacy]++
		if st.Oldest == "" || rec.CreatedAt < st.Oldest {
			st.Oldest = rec.CreatedAt
		}
	}
	for _, rec := range ask {
		r.wg.Add(1)
		go r.ask(ctx, rec)
	}
	if r.Status == "" {
		return nil
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(r.Status, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write the review status: %w", err)
	}

	return nil
}

// Arguments is what a prompt shows and the approval binds: the whole record
func Arguments(rec memory.Record) []byte {
	b, _ := json.Marshal(map[string]any{
		"id": rec.ID, "source": rec.Source, "kind": rec.Kind, "privacy": rec.Privacy,
		"created_at": rec.CreatedAt, "body": rec.Body,
	})
	return b
}

// ask asks about one record and acts on the answer
func (r *Reviewer) ask(ctx context.Context, rec memory.Record) {
	defer r.wg.Done()
	err := r.Approvals.Dispatch(ctx, Scope, approval.Invocation{Action: Action, Target: "memory", Arguments: Arguments(rec)}, time.Time{}, r.review(), func(ctx context.Context, _ approval.Invocation) error {
		return r.Records.Approve(ctx, rec.ID)
	})
	rest := time.Duration(0)
	switch {
	case err == nil:
		r.logf("memory-review: Jason approved record %d", rec.ID)
	case errors.Is(err, approval.ErrDeclined):
		// only his own no deletes; a timeout or a stopping plugin is a
		// plain denial and leaves the record staged
		switch derr := r.Records.Discard(context.WithoutCancel(ctx), rec.ID); {
		case derr == nil:
			r.logf("memory-review: Jason turned down record %d; deleted", rec.ID)
		case errors.Is(derr, memory.ErrApproved):
			r.logf("memory-review: Jason turned down record %d, but it was approved meanwhile, so it stays", rec.ID)
		default:
			r.logf("memory-review: Jason turned down record %d, but it couldn't be deleted: %v", rec.ID, derr)
			rest = r.retry()
		}
	case ctx.Err() != nil:
		// stopping: the record is asked again after a restart
	case errors.Is(err, context.DeadlineExceeded):
		r.logf("memory-review: nobody answered about record %d; asking again in %s", rec.ID, r.retry())
		rest = r.retry()
	case errors.Is(err, approval.ErrDenied):
		// the plugin stopped (Hermes restarting) before Jason answered
		rest = 10 * r.interval()
		r.logf("memory-review: the prompt about record %d ended unanswered; asking again in %s", rec.ID, rest)
	default:
		rest = 10 * r.interval()
		r.logf("memory-review: couldn't ask about record %d (asking again in %s): %v", rec.ID, rest, err)
	}
	r.mu.Lock()
	delete(r.asking, rec.ID)
	if rest > 0 {
		r.quiet[rec.ID] = r.now().Add(rest)
	}
	r.mu.Unlock()
}

// Retier asks Jason on Signal to move approved record id to tier to (Suite
// D136), showing the whole record and its tiers, and moves it only on his
// yes. Anything else, a timeout included, leaves the record where it is
func (r *Reviewer) Retier(ctx context.Context, src Retierer, id int64, to string) error {
	if to != "P0" && to != "P1" && to != "P2" {
		return fmt.Errorf("re-tier record %d: %q isn't a tier; use P0, P1 or P2", id, to)
	}
	export, err := src.Export(ctx)
	if err != nil {
		return fmt.Errorf("re-tier record %d: read the approved records: %w", id, err)
	}
	var found *memory.ExportedRecord
	for i := range export.Records {
		if export.Records[i].ID == id {
			found = &export.Records[i]
		}
	}
	if found == nil {
		return fmt.Errorf("re-tier record %d: no approved record with that id in the reviewer's tiers; a staged one is approved first", id)
	}
	if found.Privacy == to {
		return fmt.Errorf("re-tier record %d: it is already %s", id, to)
	}
	args, _ := json.Marshal(map[string]any{
		"id": id, "source": found.Source, "kind": found.Kind, "from": found.Privacy, "to": to,
		"created_at": found.CreatedAt, "body": found.Body,
	})
	call := approval.Invocation{Action: RetierAction, Target: to, Arguments: args}
	if utf8.RuneCountInString(approval.Summary(approval.Prompt{Invocation: call, ExpiresAt: r.now().Add(time.Hour)})) > approval.MaxSummary {
		return fmt.Errorf("re-tier record %d: it is too long to show whole on Signal, so move it by hand with the service's retier action", id)
	}
	err = r.Approvals.Dispatch(ctx, Scope, call, time.Time{}, r.review(), func(ctx context.Context, _ approval.Invocation) error {
		return src.Retier(ctx, id, found.Privacy, to, found.BodyHash)
	})
	switch {
	case err == nil:
		r.logf("memory-review: Jason moved record %d from %s to %s", id, found.Privacy, to)
		return nil
	case errors.Is(err, approval.ErrDeclined):
		return fmt.Errorf("re-tier record %d: Jason said no, so it stays %s", id, found.Privacy)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("re-tier record %d: nobody answered, so it stays %s; run it again to ask again", id, found.Privacy)
	case errors.Is(err, memory.ErrStale):
		return fmt.Errorf("re-tier record %d: it changed while the prompt was open, so nothing moved; run it again", id)
	default:
		return fmt.Errorf("re-tier record %d: %w", id, err)
	}
}

// review withdraws each prompt a minute before it expires, so the
// plugin's own deny at the expiry never races an answer
func (r *Reviewer) review() approval.Review {
	return func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
		ctx, cancel := context.WithDeadline(ctx, p.ExpiresAt.Add(-time.Minute))
		defer cancel()
		return r.Review(ctx, p)
	}
}

// ReadStatus reads the counts bud memory-review last wrote; ok is false
// when it never wrote any
func ReadStatus(path string) (st Status, ok bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, false, nil
	}
	if err != nil {
		return Status{}, false, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return Status{}, false, fmt.Errorf("read %s: %w", path, err)
	}

	return st, true, nil
}

func (r *Reviewer) interval() time.Duration {
	if r.Interval > 0 {
		return r.Interval
	}
	return time.Minute
}

func (r *Reviewer) retry() time.Duration {
	if r.Retry > 0 {
		return r.Retry
	}
	return 24 * time.Hour
}

func (r *Reviewer) maxAsking() int {
	if r.MaxAsking > 0 {
		return r.MaxAsking
	}
	return 4
}

func (r *Reviewer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reviewer) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}
