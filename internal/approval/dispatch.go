package approval

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrReviewUnavailable = errors.New("authenticated approval review unavailable")
var ErrReviewMismatch = errors.New("approval response does not match the pending request")
var ErrDenied = errors.New("approval denied")

// Scope is assigned by the trusted adapter from the authenticated transport.
// Actor and Surface are opaque identifiers, never credentials.
type Scope struct {
	Runtime string
	Profile string
	Session string
	Surface string
	Actor   string
}

// Invocation describes the final call after argument rewrites. Target identifies
// the resolved resource and any context that changes the call's effect.
type Invocation struct {
	Action    string
	Target    string
	Arguments []byte
}

type Prompt struct {
	ID         string
	Invocation Invocation
	ExpiresAt  time.Time
}

type Choice string

const (
	AllowOnce Choice = "once"
	DenyOnce  Choice = "deny"
)

type Reply struct {
	ID     string
	Scope  Scope
	Choice Choice
}

// Review presents a prompt on the turn's owning surface. The adapter must
// derive Reply.Scope from the authenticated channel, independently of the reply
// contents, and stop waiting when ctx ends. Incomplete displays may only deny.
type Review func(ctx context.Context, prompt Prompt) (Reply, error)

// Dispatch requires a matching decision and consumes it before passing the
// frozen invocation to execute. The trusted runtime calls this only after
// native checks and Bud policy select review. Agent tools must not be able to
// reach the store or impersonate the reviewer.
func (s Store) Dispatch(ctx context.Context, scope Scope, call Invocation, deadline time.Time, review Review, execute func(context.Context, Invocation) error) error {
	if scope.Runtime == "" || scope.Profile == "" || scope.Session == "" || scope.Surface == "" || scope.Actor == "" || review == nil {
		return ErrReviewUnavailable
	}
	if execute == nil {
		return errors.New("approval dispatch requires an executor")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	call.Arguments = bytes.Clone(call.Arguments)
	binding, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fmt.Errorf("generate approval request ID: %w", err)
	}
	request, err := New(hex.EncodeToString(id[:]), string(binding), call.Action, call.Target, call.Arguments, time.Now())
	if err != nil {
		return err
	}
	if !deadline.IsZero() && deadline.Before(request.ExpiresAt) {
		request.ExpiresAt = deadline
	}
	if turnDeadline, ok := ctx.Deadline(); ok && turnDeadline.Before(request.ExpiresAt) {
		request.ExpiresAt = turnDeadline
	}
	reviewCtx, cancel := context.WithDeadline(ctx, request.ExpiresAt)
	defer cancel()
	if err := reviewCtx.Err(); err != nil {
		return err
	}
	if err := s.Create(request); err != nil {
		return fmt.Errorf("persist approval request: %w", err)
	}
	pending := true
	defer func() {
		if pending {
			_ = s.Decide(request.ID, Canceled, time.Now())
		}
	}()
	if err := reviewCtx.Err(); err != nil {
		return err
	}
	display := call
	display.Arguments = bytes.Clone(call.Arguments)
	reply, err := review(reviewCtx, Prompt{ID: request.ID, Invocation: display, ExpiresAt: request.ExpiresAt})
	if err := reviewCtx.Err(); err != nil {
		return err
	}
	if err != nil {
		return fmt.Errorf("review approval request: %w", err)
	}
	if reply.ID != request.ID || reply.Scope != scope {
		return ErrReviewMismatch
	}
	switch reply.Choice {
	case DenyOnce:
		if err := s.Decide(request.ID, Denied, time.Now()); err != nil {
			return fmt.Errorf("persist approval denial: %w", err)
		}
		pending = false
		return ErrDenied
	case AllowOnce:
		// Persist approval and consumption atomically: a crash or canceled turn
		// cannot leave a durable Approved request available for a later retry.
		if err := s.approveAndConsume(request.ID, request.Session, call.Action, call.Target, call.Arguments); err != nil {
			return fmt.Errorf("consume approval: %w", err)
		}
	default:
		return ErrReviewMismatch
	}
	pending = false
	// The store write may block across cancellation or expiry. The approval is
	// already spent even when the tool is prevented from starting.
	if err := reviewCtx.Err(); err != nil {
		return err
	}
	if !time.Now().Before(request.ExpiresAt) {
		return context.DeadlineExceeded
	}
	return execute(ctx, call)
}
