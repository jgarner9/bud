package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/registry"
)

const secretPrompt = "my-secret-prompt-text"

// p0Rig is a router whose local provider is down and whose reviewer is a
// function the test controls
type p0Rig struct {
	rt     *Router
	cloud  *upstream
	prompt chan approval.Prompt
}

func newP0Rig(t *testing.T, wait time.Duration, decide func(context.Context, approval.Prompt) (approval.Choice, error)) *p0Rig {
	t.Helper()
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	reg := fbRegistry(local.URL, cloud.URL)
	rig := &p0Rig{cloud: cloud, prompt: make(chan approval.Prompt, 4)}
	var ap *Approvals
	if decide != nil {
		ap = &Approvals{
			Store: approval.Store{Path: filepath.Join(t.TempDir(), "approvals.json")},
			Review: func(scope approval.Scope) approval.Review {
				return func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
					rig.prompt <- p
					c, err := decide(ctx, p)
					if err != nil {
						return approval.Reply{}, err
					}
					return approval.Reply{ID: p.ID, Scope: scope, Choice: c}, nil
				}
			},
		}
	}
	rt, err := New(reg, env(nil), Options{Logf: quiet, Approvals: ap, P0Wait: wait})
	if err != nil {
		t.Fatal(err)
	}
	rt.health.markDown("local")
	rig.rt = rt
	return rig
}

func allow(context.Context, approval.Prompt) (approval.Choice, error) { return approval.AllowOnce, nil }

const p0Body = `{"model":"bud/coding","messages":[{"role":"user","content":"` + secretPrompt + `"}],"temperature":0.2}`

const p0Message = "no local model is available for a sensitive (P0) request; the local model is down"

func TestP0WithNoApprovalsWiredAnswers503AtOnce(t *testing.T) {
	rig := newP0Rig(t, time.Minute, nil)
	w := do(t, rig.rt, "POST", "/v1/chat/completions", p0Body, nil)
	if w.Code != 503 || errMessage(t, w) != p0Message || !strings.Contains(w.Body.String(), `"api_error"`) {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
	if rig.cloud.hits.Load() != 0 {
		t.Error("the request went upstream")
	}
}

func TestAnApprovedP0RequestGoesToTheP1FallbackWithItsBodyUnchangedBesidesModel(t *testing.T) {
	rig := newP0Rig(t, time.Minute, allow)
	w := do(t, rig.rt, "POST", "/v1/chat/completions", p0Body, map[string]string{"X-Bud-Privacy": "P0"})
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if w.Header().Get("X-Bud-Model") != "glm" || w.Header().Get("X-Bud-Fallback") != "p0-approved" {
		t.Errorf("headers %v", w.Header())
	}
	body, hdr := rig.cloud.last()
	var got, want map[string]json.RawMessage
	json.Unmarshal([]byte(body), &got)
	json.Unmarshal([]byte(p0Body), &want)
	if string(got["model"]) != `"glm-up"` {
		t.Errorf("model %s", got["model"])
	}
	delete(got, "model")
	delete(want, "model")
	if len(got) != len(want) {
		t.Fatalf("fields %v, want %v", got, want)
	}
	for k, v := range want {
		if string(got[k]) != string(v) {
			t.Errorf("%s changed: %s", k, got[k])
		}
	}
	if hdr.Get("X-Bud-Privacy") != "" {
		t.Error("the tier header reached the provider")
	}
	p := <-rig.prompt
	if c := p.Invocation; c.Action != "router:p0-fallback" || c.Target != "glm" {
		t.Errorf("invocation %+v", c)
	}
	var args map[string]string
	if err := json.Unmarshal(p.Invocation.Arguments, &args); err != nil || args["role"] != "coding" || args["endpoint"] != "/v1/chat/completions" || len(args["body_sha256"]) != 64 {
		t.Errorf("arguments %s", p.Invocation.Arguments)
	}
}

func TestAP0RequestThatIsDeniedExpiredOrAbandonedAnswers503AndNeverGoesUpstream(t *testing.T) {
	deny := func(context.Context, approval.Prompt) (approval.Choice, error) { return approval.DenyOnce, nil }
	wait := func(ctx context.Context, _ approval.Prompt) (approval.Choice, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	unavailable := func(context.Context, approval.Prompt) (approval.Choice, error) {
		return "", approval.ErrReviewUnavailable
	}
	for name, tc := range map[string]struct {
		decide func(context.Context, approval.Prompt) (approval.Choice, error)
		wait   time.Duration
		cancel bool
	}{
		"denied":      {deny, time.Minute, false},
		"expired":     {wait, 50 * time.Millisecond, false},
		"unavailable": {unavailable, time.Minute, false},
		"client gone": {wait, time.Minute, true},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newP0Rig(t, tc.wait, tc.decide)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				go func() { <-rig.prompt; cancel() }()
			}
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(p0Body)).WithContext(ctx)
			w := httptest.NewRecorder()
			rig.rt.ServeHTTP(w, req)
			if w.Code != 503 || !strings.HasPrefix(errMessage(t, w), p0Message+"; Jason didn't approve sending it to glm") {
				t.Errorf("status %d: %s", w.Code, w.Body)
			}
			if rig.cloud.hits.Load() != 0 {
				t.Error("the request went upstream")
			}
		})
	}
}

func TestTheP0ApprovalSummaryHasNoPromptText(t *testing.T) {
	rig := newP0Rig(t, time.Minute, allow)
	do(t, rig.rt, "POST", "/v1/chat/completions", p0Body, nil)
	p := <-rig.prompt
	if s := p.Invocation.Action + p.Invocation.Target + string(p.Invocation.Arguments); strings.Contains(s, secretPrompt) {
		t.Errorf("the prompt shows %q", s)
	}
}

func TestP0WithNoP1FallbackAnswers503WithoutAPrompt(t *testing.T) {
	rig := newP0Rig(t, time.Minute, allow)
	// only a P2-only model is left up
	rig.rt.reg.Roles["coding"] = registry.Role{Pool: []string{"qwen", "p2"}}
	w := do(t, rig.rt, "POST", "/v1/chat/completions", p0Body, nil)
	if w.Code != 503 || errMessage(t, w) != p0Message {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
	if len(rig.prompt) != 0 || rig.cloud.hits.Load() != 0 {
		t.Error("asked, or went upstream")
	}
}

func TestP0WhoseFallbackTheCapBlocksAnswers402WithoutAPrompt(t *testing.T) {
	rig := newP0Rig(t, time.Minute, allow)
	rig.rt.reg.Models["glm"] = registry.Model{Provider: "cloud", Name: "glm-up", Privacy: []string{"P1", "P2"}, Cost: "C2", Price: &registry.Price{In: 1, Out: 1}}
	// cap 0: every top-up is refused
	w := do(t, rig.rt, "POST", "/v1/chat/completions", p0Body, nil)
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
	if len(rig.prompt) != 0 || rig.cloud.hits.Load() != 0 {
		t.Error("asked, or went upstream")
	}
}

func TestAnExplicitP0ModelWhoseProviderIsDownGetsNoOverride(t *testing.T) {
	rig := newP0Rig(t, time.Minute, allow)
	w := do(t, rig.rt, "POST", "/v1/chat/completions", `{"model":"qwen"}`, nil)
	if w.Code != 503 || !strings.Contains(errMessage(t, w), "provider is down") || len(rig.prompt) != 0 {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
}

func TestP0RequestsPastTheHeldLimitAreRefusedAtOnce(t *testing.T) {
	rig := newP0Rig(t, time.Minute, allow)
	for range maxP0Held {
		rig.rt.p0Held <- struct{}{}
	}
	w := do(t, rig.rt, "POST", "/v1/chat/completions", p0Body, nil)
	if w.Code != 503 || !strings.Contains(errMessage(t, w), "already waiting") {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
	if len(rig.prompt) != 0 || rig.cloud.hits.Load() != 0 {
		t.Error("asked, or went upstream")
	}
}
