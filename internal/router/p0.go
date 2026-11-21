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
