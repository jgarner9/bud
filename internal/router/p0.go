package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/registry"
)

// Approvals is what the router needs to ask Jason before a P0 request goes to
// a model that isn't local (Suite D104)
type Approvals struct {
	Store approval.Store
	// Review builds the reviewer for a scope; in production it is
	// approval.SocketReview over the reviewer's socket and secret
	Review func(approval.Scope) approval.Review
}

// p0Scope is fixed, as ops' is. The router can't learn the calling device's
// session or profile (the header that names the tier is the only thing a
// client sends, and the tailnet is the authentication, Suite D72), so every
// held request shares one scope and nothing in a request chooses it. The
// binding that matters is the action, the fallback model and the hash of the
// exact body
func p0Scope() approval.Scope {
	return approval.Scope{Runtime: "router", Profile: "unknown", Surface: "router", Actor: "tailnet", Session: "request"}
}

const p0Fallback = "p0-approved"

// p0TurnTTL is how long an approval covers the rest of its turn: an agent's
// tool loop sends several requests for one message, each a new body, and
// Jason approved the message, not one request of it
const p0TurnTTL = 10 * time.Minute

// turnKey names the turn a P0 request belongs to: the client's conversation
// id (x-opencode-session, which Hermes sends), the latest user message, the
// role and the fallback model. Every request of one turn repeats the same
// latest user message; the next message is a new turn and asks again. A
// request without a conversation id, or without a user message, has no key,
// so it always asks
func turnKey(r *http.Request, raw []byte, role, fb string) string {
	conv := r.Header.Get(SessionHeader)
	if conv == "" {
		return ""
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return ""
	}
	var last json.RawMessage
	for _, m := range req.Messages {
		if m.Role == "user" {
			last = m.Content
		}
	}
	if last == nil {
		return ""
	}
	h := sha256.New()
	for _, part := range [][]byte{[]byte(conv), []byte(role), []byte(fb), last} {
		h.Write(part)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// turnApproved says whether Jason already approved key's turn, forgetting
// turns that have expired
func (rt *Router) turnApproved(key string, now time.Time) bool {
	rt.p0Mu.Lock()
	defer rt.p0Mu.Unlock()
	for k, until := range rt.p0Turns {
		if !now.Before(until) {
			delete(rt.p0Turns, k)
		}
	}
	_, ok := rt.p0Turns[key]
	return key != "" && ok
}

func (rt *Router) approveTurn(key string, now time.Time) {
	if key == "" {
		return
	}
	rt.p0Mu.Lock()
	rt.p0Turns[key] = now.Add(p0TurnTTL)
	rt.p0Mu.Unlock()
}

// maxP0Held is how many P0 requests may wait for Jason at once; Signal shows
// one prompt each, so more than a few would be noise anyway
const maxP0Held = 4

// p0Down answers a P0 role request whose every eligible member is down. The
// router never falls back on its own: with approvals wired it holds the
// request and asks Jason once, naming no prompt text; the request goes
// upstream only after AllowOnce
func (rt *Router) p0Down(w http.ResponseWriter, r *http.Request, raw []byte, fields map[string]json.RawMessage, role string, ro registry.Role) {
	refuse := func(extra string) {
		msg := "no local model is available for a sensitive (P0) request; the local model is down"
		writeError(w, http.StatusServiceUnavailable, "api_error", msg+extra)
	}
	if rt.approvals == nil || rt.approvals.Review == nil || rt.approvals.Store.Path == "" {
		refuse("")
		return
	}
	if !rt.anyUp(ro, "P1", nil) {
		refuse("")
		return
	}
	fb := rt.pickSpend(ro, "P1", nil)
	if fb == "" {
		rt.capReached(w)
		return
	}
	key := turnKey(r, raw, role, fb)
	if rt.turnApproved(key, time.Now()) {
		// the rest of a turn Jason approved: same conversation, same
		// message, same fallback
		if rt.forward(w, r, fields, fb, p0Fallback) {
			rt.badGateway(w)
		}
		return
	}
	select {
	case rt.p0Held <- struct{}{}:
		defer func() { <-rt.p0Held }()
	default:
		refuse(fmt.Sprintf("; %d sensitive requests are already waiting for Jason's approval, so this one wasn't held", maxP0Held))
		return
	}
	sum := sha256.Sum256(raw)
	args, _ := json.Marshal(map[string]string{"role": role, "endpoint": r.URL.Path, "body_sha256": hex.EncodeToString(sum[:])})
	scope := p0Scope()
	call := approval.Invocation{Action: "router:p0-fallback", Target: fb, Arguments: args}
	ctx, cancel := context.WithTimeout(r.Context(), rt.p0Wait)
	defer cancel()

	var failed, ran bool
	err := rt.approvals.Store.Dispatch(ctx, scope, call, time.Now().Add(rt.p0Wait), rt.approvals.Review(scope), func(ctx context.Context, _ approval.Invocation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		ran = true
		// the request's own context carries the forward: the review's ends
		// with the wait for Jason, which a long stream would outlive
		failed = rt.forward(w, r, fields, fb, p0Fallback)
		return nil
	})
	if ran {
		rt.approveTurn(key, time.Now())
		if failed {
			rt.badGateway(w)
		}
		return
	}
	reason := "the approval store failed"
	switch {
	case errors.Is(err, approval.ErrDenied):
		reason = "denied"
	case errors.Is(err, context.Canceled):
		reason = "the request ended first"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "no one answered in time"
	case errors.Is(err, approval.ErrReviewUnavailable):
		reason = "approval reviews are unavailable"
	case errors.Is(err, approval.ErrReviewMismatch):
		reason = "the approval reply didn't match the request"
	}
	refuse(fmt.Sprintf("; Jason didn't approve sending it to %s (%s)", fb, reason))
}
