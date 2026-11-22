package cli

import (
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
	if err := runMemoryReview(&Globals{}, nil, "/run/m.sock"); err != nil {
		t.Fatal(err)
	}
	if got == nil || !strings.HasSuffix(got.Status, "bud/memory-review.json") || got.Review == nil {
		t.Errorf("reviewer = %+v", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if err := runMemoryReview(&Globals{}, nil, ""); err == nil || !strings.Contains(err.Error(), "pass --socket") {
		t.Errorf("no runtime dir and no --socket: %v", err)
	}
}
