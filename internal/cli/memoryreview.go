package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/memoryreview"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(MemoryReview())
}

// MemoryReview asks Jason on Signal about each staged memory record (Suite
// D113); see docs/internals/memory-review.md
func MemoryReview() *Command {
	var socket string
	return &Command{
		Name:  "memory-review",
		Usage: "Asks Jason on Signal about each staged memory record: a yes approves it, a no deletes it",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&socket, "socket", "", "the memory service's socket (default $XDG_RUNTIME_DIR/bud-memory/memory.sock)")
		},
		Run: func(g *Globals, args []string) error { return runMemoryReview(g, args, socket) },
	}
}

// reviewerToken is the environment variable op run fills with the memory
// service's reviewer token
const reviewerToken = "BUD_MEMORY_TOKEN_REVIEWER"

// runReviewer is swappable so tests stop before the loop
var runReviewer = func(ctx context.Context, r *memoryreview.Reviewer) { r.Run(ctx) }

func runMemoryReview(g *Globals, args []string, socket string) error {
	if len(args) > 0 {
		return usageErrorf("memory-review takes no arguments")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if socket == "" {
		if socket = p.MemorySocket(); socket == "" {
			return usageErrorf("XDG_RUNTIME_DIR isn't set, so the memory service's socket can't be found; pass --socket")
		}
	}
	if p.ReviewSocket() == "" {
		return fmt.Errorf("memory-review: XDG_RUNTIME_DIR isn't set, so the Signal reviewer's socket can't be found; run it as a user unit")
	}
	tok := os.Getenv(reviewerToken)
	if tok == "" {
		return fmt.Errorf("memory-review: %s is empty; run it under op run with server/memory-review/memory-review.env", reviewerToken)
	}

	r := &memoryreview.Reviewer{
		Records:   memory.New(socket, tok),
		Approvals: approval.Store{Path: p.ApprovalStore()},
		Review:    approval.SocketReview(p.ReviewSocket(), p.ReviewSecret(), memoryreview.Scope),
		Status:    p.MemoryReviewStatus(),
		Logf:      func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("memory-review: reviewing %s through %s", socket, p.ReviewSocket())
	runReviewer(ctx, r)

	return nil
}
