package memoryreview

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/memory"
)

// fakeRecords is the memory service: staged records, and what was decided
type fakeRecords struct {
	mu        sync.Mutex
	staged    []memory.Record
	approved  []int64
	discarded []int64
}

func (f *fakeRecords) Staged(_ context.Context, after int64, limit int) ([]memory.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []memory.Record
	for _, r := range f.staged {
		if r.ID > after && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRecords) remove(id int64) {
	for i, r := range f.staged {
		if r.ID == id {
			f.staged = append(f.staged[:i], f.staged[i+1:]...)
			return
		}
	}
}

func (f *fakeRecords) Approve(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved = append(f.approved, id)
	f.remove(id)
	return nil
}

func (f *fakeRecords) Discard(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discarded = append(f.discarded, id)
	f.remove(id)
	return nil
}

func rec(id int64, privacy, body string) memory.Record {
	return memory.Record{ID: id, Source: "hermes", Kind: "fact", Privacy: privacy, Body: body, CreatedAt: "2026-11-22 08:0" + string(rune('0'+id%10)) + ":00"}
}

// answer is a Review that answers every prompt the same way
func answer(choice approval.Choice, answered bool) approval.Review {
	return func(_ context.Context, p approval.Prompt) (approval.Reply, error) {
		return approval.Reply{ID: p.ID, Scope: Scope, Choice: choice, Answered: answered}, nil
	}
}

func newReviewer(t *testing.T, f *fakeRecords, review approval.Review) *Reviewer {
	dir := t.TempDir()
	return &Reviewer{
		Records:   f,
		Approvals: approval.Store{Path: filepath.Join(dir, "approvals.json")},
		Review:    review,
		Status:    filepath.Join(dir, "memory-review.json"),
		Logf:      t.Logf,
	}
}

func round(t *testing.T, r *Reviewer) {
	t.Helper()
	if err := r.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.wg.Wait()
}

func TestAYesApprovesAndHisNoDeletes(t *testing.T) {
	for _, tc := range []struct {
		name               string
		review             approval.Review
		approved, discards []int64
	}{
		{"yes", answer(approval.AllowOnce, true), []int64{1}, nil},
		{"his no", answer(approval.DenyOnce, true), nil, []int64{1}},
		// a deny with no answer is the plugin stopping: nothing is deleted
		{"the plugin stopping", answer(approval.DenyOnce, false), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRecords{staged: []memory.Record{rec(1, "P1", "the dentist is on Tuesdays")}}
			r := newReviewer(t, f, tc.review)
			round(t, r)
			if !reflect.DeepEqual(f.approved, tc.approved) || !reflect.DeepEqual(f.discarded, tc.discards) {
				t.Errorf("approved %v, discarded %v; want %v, %v", f.approved, f.discarded, tc.approved, tc.discards)
			}
		})
	}
}

func TestThePromptShowsTheWholeRecordAndEndsBeforeThePluginsDeny(t *testing.T) {
	f := &fakeRecords{staged: []memory.Record{rec(3, "P0", "line one\nline two")}}
	var got approval.Prompt
	var left time.Duration
	r := newReviewer(t, f, func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
		got = p
		d, _ := ctx.Deadline()
		left = p.ExpiresAt.Sub(d)
		return approval.Reply{ID: p.ID, Scope: Scope, Choice: approval.AllowOnce, Answered: true}, nil
	})
	round(t, r)
	if got.Invocation.Action != "memory:approve" || got.Invocation.Target != "memory" {
		t.Errorf("invocation = %+v", got.Invocation)
	}
	var args map[string]any
	if err := json.Unmarshal(got.Invocation.Arguments, &args); err != nil || args["body"] != "line one\nline two" || args["privacy"] != "P0" || args["id"] != 3.0 {
		t.Errorf("arguments = %s", got.Invocation.Arguments)
	}
	if s := approval.Summary(got); !strings.Contains(s, "Save this to Bud memory?") || !strings.Contains(s, "│ line two") {
		t.Errorf("summary = %q", s)
	}
	if left < time.Minute {
		t.Errorf("the review ends %s before the expiry, want a minute, so the plugin's own deny can't race an answer", left)
	}
}

func TestAnUnansweredRecordRestsBeforeItIsAskedAgain(t *testing.T) {
	f := &fakeRecords{staged: []memory.Record{rec(1, "P1", "a")}}
	asked := 0
	r := newReviewer(t, f, func(context.Context, approval.Prompt) (approval.Reply, error) {
		asked++
		return approval.Reply{}, context.DeadlineExceeded
	})
	now := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	r.Now = func() time.Time { return now }
	round(t, r)
	round(t, r)
	if asked != 1 {
		t.Fatalf("asked %d times in a row, want once", asked)
	}
	now = now.Add(25 * time.Hour)
	round(t, r)
	if asked != 2 {
		t.Errorf("asked %d times after a day, want twice", asked)
	}
	if len(f.approved)+len(f.discarded) != 0 {
		t.Errorf("an unanswered prompt decided something: %v %v", f.approved, f.discarded)
	}
}

func TestATooLongRecordIsLeftForHandApproval(t *testing.T) {
	f := &fakeRecords{staged: []memory.Record{rec(1, "P1", strings.Repeat("x", approval.MaxSummary))}}
	r := newReviewer(t, f, func(context.Context, approval.Prompt) (approval.Reply, error) {
		t.Error("a record too long to show whole was asked about")
		return approval.Reply{}, nil
	})
	round(t, r)
	st, ok, err := ReadStatus(r.Status)
	if err != nil || !ok || st.TooLong != 1 || st.Pending != 1 {
		t.Errorf("status = %+v, %v, %v; want one pending, too long", st, ok, err)
	}
}

func TestAtMostMaxAskingPromptsWaitAndTheStatusCountsThem(t *testing.T) {
	f := &fakeRecords{}
	for i := int64(1); i <= 5; i++ {
		f.staged = append(f.staged, rec(i, []string{"P0", "P1", "P2"}[i%3], "body"))
	}
	release := make(chan struct{})
	var mu sync.Mutex
	var seen []string
	r := newReviewer(t, f, func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
		mu.Lock()
		seen = append(seen, p.ID)
		mu.Unlock()
		<-release
		return approval.Reply{ID: p.ID, Scope: Scope, Choice: approval.AllowOnce, Answered: true}, nil
	})
	r.MaxAsking = 2
	if err := r.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	// a second round while both wait opens no more
	if err := r.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _, _ := ReadStatus(r.Status)
	want := Status{UpdatedAt: st.UpdatedAt, Pending: 5, ByPrivacy: map[string]int{"P0": 1, "P1": 2, "P2": 2}, Asking: 2, Oldest: "2026-11-22 08:01:00"}
	if !reflect.DeepEqual(st, want) {
		t.Errorf("status = %+v, want %+v", st, want)
	}
	close(release)
	r.wg.Wait()
	if len(seen) != 2 || len(f.approved) != 2 {
		t.Errorf("asked %d, approved %v; want 2 and 2", len(seen), f.approved)
	}
	round(t, r)
	round(t, r)
	if len(f.approved) != 5 || len(f.staged) != 0 {
		t.Errorf("approved %v, left %v; want all five approved", f.approved, f.staged)
	}
}

func TestReadStatusWithNoFile(t *testing.T) {
	if _, ok, err := ReadStatus(filepath.Join(t.TempDir(), "none.json")); ok || err != nil {
		t.Errorf("ReadStatus = %v, %v; want not ok and no error", ok, err)
	}
}

// fakeApproved is the memory service's approved records, which a re-tier
// lists and moves
type fakeApproved struct {
	records []memory.ExportedRecord
	moved   map[int64]string
}

func (f *fakeApproved) Export(context.Context) (*memory.Export, error) {
	return &memory.Export{Records: f.records}, nil
}

func (f *fakeApproved) Retier(_ context.Context, id int64, privacy string) error {
	if f.moved == nil {
		f.moved = map[int64]string{}
	}
	f.moved[id] = privacy
	return nil
}

func approvedP1(id int64, body string) memory.ExportedRecord {
	return memory.ExportedRecord{ID: id, Source: "hermes", Kind: "fact", Privacy: "P1", Body: body, CreatedAt: "2026-11-22 08:00:00", State: "approved"}
}

// Suite D136: a yes moves the record, and the prompt shows it whole with
// where it goes; his no, a plugin that stops and an unanswered prompt leave it
func TestARetierMovesTheRecordOnlyOnHisYes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		review approval.Review
		moved  map[int64]string
		errHas string
	}{
		{"yes", answer(approval.AllowOnce, true), map[int64]string{2: "P0"}, ""},
		{"his no", answer(approval.DenyOnce, true), nil, "Jason said no"},
		{"the plugin stopping", answer(approval.DenyOnce, false), nil, "denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeApproved{records: []memory.ExportedRecord{approvedP1(1, "other"), approvedP1(2, "the dentist is on Tuesdays")}}
			var got approval.Prompt
			r := newReviewer(t, &fakeRecords{}, func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
				got = p
				return tc.review(ctx, p)
			})
			err := r.Retier(context.Background(), f, 2, "P0")
			if tc.errHas == "" && err != nil || tc.errHas != "" && (err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.errHas))) {
				t.Fatalf("err = %v, want one with %q", err, tc.errHas)
			}
			if !reflect.DeepEqual(f.moved, tc.moved) {
				t.Errorf("moved %v, want %v", f.moved, tc.moved)
			}
			if got.Invocation.Action != "memory:retier" || got.Invocation.Target != "P0" {
				t.Errorf("invocation = %+v", got.Invocation)
			}
			if s := approval.Summary(got); !strings.Contains(s, "Move this Bud memory record to P0?") || !strings.Contains(s, "body: the dentist is on Tuesdays") || !strings.Contains(s, "from: P1") {
				t.Errorf("summary = %q", s)
			}
		})
	}
}

func TestARetierAsksNothingForARecordItCannotMove(t *testing.T) {
	long := approvedP1(3, strings.Repeat("a long record line ", 400))
	f := &fakeApproved{records: []memory.ExportedRecord{approvedP1(1, "same tier"), long}}
	asked := false
	r := newReviewer(t, &fakeRecords{}, func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
		asked = true
		return answer(approval.AllowOnce, true)(ctx, p)
	})
	for _, tc := range []struct {
		id     int64
		to     string
		errHas string
	}{
		{9, "P0", "no approved record"},
		{1, "P1", "already P1"},
		{1, "P7", "isn't a tier"},
		{3, "P0", "too long"},
	} {
		if err := r.Retier(context.Background(), f, tc.id, tc.to); err == nil || !strings.Contains(err.Error(), tc.errHas) {
			t.Errorf("Retier(%d, %s) = %v, want %q", tc.id, tc.to, err, tc.errHas)
		}
	}
	if asked || len(f.moved) != 0 {
		t.Errorf("asked %v, moved %v", asked, f.moved)
	}
}
