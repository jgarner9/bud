package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
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
		Usage: "Asks Jason on Signal about each staged memory record: a yes approves it, a no deletes it; `retier <id> <P0|P1|P2>` asks to move an approved record to another tier",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&socket, "socket", "", "the memory service's socket (default $XDG_RUNTIME_DIR/bud-memory/memory.sock)")
		},
		Run: func(g *Globals, args []string) error { return runMemoryReview(g, args, socket) },
	}
}

// reviewerToken is the environment variable op run fills with the memory
// service's reviewer token
const reviewerToken = "BUD_MEMORY_TOKEN_REVIEWER"

// retierRecord is swappable so tests don't ask on Signal
var retierRecord = func(ctx context.Context, r *memoryreview.Reviewer, svc memoryreview.Retierer, id int64, to string) error {
	return r.Retier(ctx, svc, id, to)
}

// runReviewer is swappable so tests stop before the loop
var runReviewer = func(ctx context.Context, r *memoryreview.Reviewer) { r.Run(ctx) }

func runMemoryReview(g *Globals, args []string, socket string) error {
	retier := len(args) > 0 && args[0] == "retier"
	if len(args) > 0 && !retier {
		return usageErrorf("memory-review takes no arguments, or `retier <id> <P0|P1|P2>`")
	}
	var id int64
	if retier {
		if len(args) != 3 {
			return usageErrorf("usage: memory-review retier <id> <P0|P1|P2>")
		}
		var err error
		if id, err = strconv.ParseInt(args[1], 10, 64); err != nil || id < 1 {
			return usageErrorf("%q isn't a record id; use the number memory_search shows", args[1])
		}
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

	svc := memory.New(socket, tok)
	r := &memoryreview.Reviewer{
		Records:   svc,
		Approvals: approval.Store{Path: p.ApprovalStore()},
		Review:    approval.SocketReview(p.ReviewSocket(), p.ReviewSecret(), memoryreview.Scope),
		Status:    p.MemoryReviewStatus(),
		Logf:      func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if retier {
		g.Noticef("memory-review: asking on Signal to move record %d to %s", id, args[2])
		if err := retierRecord(ctx, r, svc, id, args[2]); err != nil {
			return err
		}
		fmt.Printf("record %d is now %s\n", id, args[2])
		return nil
	}
	g.Noticef("memory-review: reviewing %s through %s", socket, p.ReviewSocket())
	runReviewer(ctx, r)

	return nil
}
