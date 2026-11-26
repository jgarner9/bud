package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/memoryreview"
)

// the command needs the reviewer token from op run and a runtime dir for
// the Signal reviewer's socket
func TestMemoryReviewChecksItsSetup(t *testing.T) {
	var got *memoryreview.Reviewer
	orig := runReviewer
	runReviewer = func(_ context.Context, r *memoryreview.Reviewer) { got = r }
	t.Cleanup(func() { runReviewer = orig })
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	t.Setenv(reviewerToken, "")
	if err := runMemoryReview(&Globals{}, nil, "/run/m.sock"); err == nil || !strings.Contains(err.Error(), "BUD_MEMORY_TOKEN_REVIEWER is empty") {
		t.Errorf("no token: %v", err)
	}
	t.Setenv(reviewerToken, "tok")
	if err := runMemoryReview(&Globals{}, []string{"x"}, "/run/m.sock"); err == nil {
		t.Error("an argument was accepted")
	}
	var stderr bytes.Buffer
	if err := runMemoryReview(&Globals{stderr: &stderr}, nil, "/run/m.sock"); err != nil {
		t.Fatal(err)
	}
	// without --verbose too, so the journal shows it started
	if !strings.Contains(stderr.String(), "memory-review: reviewing /run/m.sock") {
		t.Errorf("stderr = %q, want the start line", stderr.String())
	}
	if got == nil || !strings.HasSuffix(got.Status, "bud/memory-review.json") || got.Review == nil {
		t.Errorf("reviewer = %+v", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if err := runMemoryReview(&Globals{}, nil, ""); err == nil || !strings.Contains(err.Error(), "pass --socket") {
		t.Errorf("no runtime dir and no --socket: %v", err)
	}
}

// `memory-review retier <id> <tier>` asks about one record instead of
// running the loop
func TestMemoryReviewRetiersOneRecord(t *testing.T) {
	var gotID int64
	var gotTo string
	orig := retierRecord
	retierRecord = func(_ context.Context, _ *memoryreview.Reviewer, _ memoryreview.Retierer, id int64, to string) error {
		gotID, gotTo = id, to
		return nil
	}
	t.Cleanup(func() { retierRecord = orig })
	origLoop := runReviewer
	runReviewer = func(context.Context, *memoryreview.Reviewer) { t.Error("the loop ran") }
	t.Cleanup(func() { runReviewer = origLoop })
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(reviewerToken, "tok")

	for _, args := range [][]string{{"retier"}, {"retier", "7"}, {"retier", "x", "P0"}, {"retier", "0", "P0"}, {"retier", "7", "P0", "P1"}, {"retier", "7", "P7"}} {
		if err := runMemoryReview(&Globals{}, args, "/run/m.sock"); err == nil || gotID != 0 {
			t.Errorf("%v = %v, id %d", args, err, gotID)
		}
	}
	// a bad tier is a usage error before any setup, even with no token
	t.Setenv(reviewerToken, "")
	if err := runMemoryReview(&Globals{}, []string{"retier", "7", "P7"}, "/run/m.sock"); err == nil || !strings.Contains(err.Error(), "isn't a tier") {
		t.Errorf("a bad tier = %v, want a usage error naming the tiers", err)
	}
	t.Setenv(reviewerToken, "tok")
	var stderr, stdout bytes.Buffer
	if err := runMemoryReview(&Globals{stderr: &stderr, stdout: &stdout}, []string{"retier", "7", "P0"}, "/run/m.sock"); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "record 7 is now P0\n" {
		t.Errorf("stdout = %q, want the result on the injected writer", stdout.String())
	}
	if gotID != 7 || gotTo != "P0" || !strings.Contains(stderr.String(), "asking on Signal to move record 7 to P0") {
		t.Errorf("id %d, to %q, stderr %q", gotID, gotTo, stderr.String())
	}
}
