package mcpops

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"jgarner.dev/bud/internal/approval"
)

// Tier says whether a tool's effect needs Jason's yes before it runs
type Tier int

const (
	// ReadOnly tools run at once, alone under the server's mutex
	ReadOnly Tier = iota
	// ApprovalTier tools go through approval.Store.Dispatch first (Suite D97,
	// D103); the zero Tier is ReadOnly, so a tool must opt in
	ApprovalTier
)

// Approvals is what the server needs to review approval-tier tools. Without
// it an approval-tier tool is refused, never run
type Approvals struct {
	Store approval.Store
	// Review builds the reviewer for a scope; in production it is
	// approval.SocketReview over the reviewer's socket and secret
	Review func(approval.Scope) approval.Review
	// Timeout caps the wait for Jason; zero leaves the store's one hour
	Timeout time.Duration
}

// SetApprovals sets the approval wiring; call it before serving
func (s *Server) SetApprovals(a Approvals) { s.appr = a }

// opsScope is fixed. Ops can't learn the calling device's session or privacy
// tier (the gateway strips them; r6b design, open question 3) and its peer is
// always loopback, so every ops call shares one scope. Nothing in a request
// chooses it. The binding that matters is the action and the exact argument
// bytes
func opsScope() approval.Scope {
	return approval.Scope{Runtime: "mcp-ops", Profile: "unknown", Surface: "ops", Actor: "loopback", Session: "loopback"}
}

// run executes t. A read-only tool runs under the mutex. An approval-tier
// tool is reviewed first with no lock held, so a call waiting on Jason doesn't
// block doctor for up to an hour, and takes the mutex only for its effect
func (s *Server) run(ctx context.Context, t Tool, args json.RawMessage) (any, error) {
	if t.Tier != ApprovalTier {
		s.mu.Lock()
		defer s.mu.Unlock()
		return t.Run(ctx, args)
	}
	if s.appr.Store.Path == "" || s.appr.Review == nil {
		return nil, errors.New("mcp-ops: approval reviews are unavailable, so the call was not made")
	}
	scope := opsScope()
	call := approval.Invocation{Action: "mcp:ops/" + t.Name, Target: s.name, Arguments: args}
	var deadline time.Time
	if s.appr.Timeout > 0 {
		deadline = time.Now().Add(s.appr.Timeout)
	}
	var v any
	var runErr error
	ran := false
	err := s.appr.Store.Dispatch(ctx, scope, call, deadline, s.appr.Review(scope), func(ctx context.Context, _ approval.Invocation) error {
		ran = true
		s.mu.Lock()
		defer s.mu.Unlock()
		v, runErr = t.Run(ctx, args)
		return nil
	})
	if ran {
		return v, runErr
	}
	reason := "the approval store failed, so the call was not made"
	switch {
	case errors.Is(err, approval.ErrDenied):
		reason = "the call was denied by the reviewer and was not made"
	case errors.Is(err, context.Canceled):
		reason = "the call was canceled before it was approved, so it was not made"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "no one answered the approval request in time, so the call was not made"
	case errors.Is(err, approval.ErrReviewUnavailable):
		reason = "approval reviews are unavailable, so the call was not made"
	case errors.Is(err, approval.ErrReviewMismatch):
		reason = "the approval reply didn't match the request, so the call was not made"
	}
	return nil, errors.New("mcp-ops: " + reason)
}
