package mcpgateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jgarner.dev/bud/internal/approval"
)

// approvalGateway is newGateway with an approval block: get_* is allowed and
// everything else asks. review stands in for the Signal reviewer
func approvalGateway(t *testing.T, upstreamURL string, review approval.Review) (*Gateway, string, approval.Store) {
	t.Helper()
	c := &Config{Listen: "127.0.0.1:0", Servers: map[string]Server{
		"linear": {
			URL: upstreamURL + "/mcp", Privacy: []string{"P1"},
			Approval: &Approval{Tools: map[string]string{"get_*": "allow", "list_*": "allow"}},
		},
	}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := OpenAudit(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	g, err := New(c, a, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	store := approval.Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	g.SetApprovals(Approvals{Store: store, Timeout: 2 * time.Second, Review: func(s approval.Scope) approval.Review {
		return func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
			if review == nil {
				return approval.Reply{}, approval.ErrReviewUnavailable
			}
			rep, err := review(ctx, p)
			rep.Scope = s
			return rep, err
		}
	}})
	return g, logPath, store
}

func choose(c approval.Choice) approval.Review {
	return func(_ context.Context, p approval.Prompt) (approval.Reply, error) {
		return approval.Reply{ID: p.ID, Choice: c}, nil
	}
}

func noUpstream(t *testing.T, got chan seen) {
	t.Helper()
	select {
	case s := <-got:
		t.Fatalf("upstream was sent %q", s.body)
	default:
	}
}

func wantRefusal(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	b := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(b, `"isError":true`) || !strings.Contains(b, want) {
		t.Errorf("got %d %s, want an isError result with %q", w.Code, b, want)
	}
}

func lastDecision(t *testing.T, logPath string) string {
	t.Helper()
	recs := readAudit(t, logPath)
	if len(recs) == 0 {
		t.Fatal("no audit record")
	}
	return recs[len(recs)-1].Decision
}

func TestReadsAndNonApprovalToolsPassThroughUnchanged(t *testing.T) {
	up, got := fakeUpstream(t)
	g, _, _ := approvalGateway(t, up.URL, nil) // a reviewer that is down must not matter
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue","arguments":{"id":"X-1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`[{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_issues"}}]`,
	} {
		call(g, "P1", body)
		if s := <-got; s.body != body {
			t.Errorf("upstream got %q, want %q", s.body, body)
		}
	}
}

func TestApprovedCallIsHeldAndForwardedByteForByte(t *testing.T) {
	up, got := fakeUpstream(t)
	release := make(chan struct{})
	var prompt approval.Prompt
	g, logPath, _ := approvalGateway(t, up.URL, func(_ context.Context, p approval.Prompt) (approval.Reply, error) {
		prompt = p
		<-release
		return approval.Reply{ID: p.ID, Choice: approval.AllowOnce}, nil
	})
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"save_issue","arguments": {"title":"a",  "n":1}}}`
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- call(g, "P1", body) }()

	time.Sleep(50 * time.Millisecond)
	noUpstream(t, got) // held while the review is open
	close(release)
	w := <-done
	if s := <-got; s.body != body {
		t.Errorf("upstream got %q, want the identical %q", s.body, body)
	}
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "isError") {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
	if got, want := string(prompt.Invocation.Arguments), `{"title":"a",  "n":1}`; got != want {
		t.Errorf("reviewed arguments %q, want %q", got, want)
	}
	if prompt.Invocation.Action != "mcp:linear/save_issue" || !strings.HasPrefix(prompt.Invocation.Target, "linear http://") {
		t.Errorf("binding %+v", prompt.Invocation)
	}
	if d := lastDecision(t, logPath); d != "approved" {
		t.Errorf("decision %q", d)
	}
}

func TestScopeUsesTheSessionHeaderOrTheClientAndTier(t *testing.T) {
	up, _ := fakeUpstream(t)
	var scopes []approval.Scope
	c := &Config{Listen: "127.0.0.1:0", Servers: map[string]Server{"linear": {URL: up.URL + "/mcp", Privacy: []string{"P1"}, Approval: &Approval{}}}}
	a, _ := OpenAudit(filepath.Join(t.TempDir(), "a.jsonl"))
	t.Cleanup(func() { _ = a.Close() })
	g, err := New(c, a, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	g.SetApprovals(Approvals{Store: approval.Store{Path: filepath.Join(t.TempDir(), "s.json")}, Review: func(s approval.Scope) approval.Review {
		scopes = append(scopes, s)
		return func(context.Context, approval.Prompt) (approval.Reply, error) {
			return approval.Reply{}, errors.New("no")
		}
	}})
	call(g, "P1", toolCall, func(r *http.Request) { r.Header.Set("Mcp-Session-Id", "sess-1") })
	call(g, "P1", toolCall)
	want := []approval.Scope{
		{Runtime: "mcp-gateway", Profile: "P1", Surface: "linear", Actor: "100.101.102.103", Session: "sess-1"},
		{Runtime: "mcp-gateway", Profile: "P1", Surface: "linear", Actor: "100.101.102.103", Session: "100.101.102.103/P1"},
	}
	if len(scopes) != 2 || scopes[0] != want[0] || scopes[1] != want[1] {
		t.Errorf("scopes %+v, want %+v", scopes, want)
	}
}

func TestEveryFailureSendsNothingUpstreamAndReturnsIsError(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"save_issue","arguments":{"a":1}}}`
	cases := []struct {
		name     string
		review   approval.Review
		breakDir bool
		want     string
		decision string
	}{
		{"denied", choose(approval.DenyOnce), false, "denied", "denied"},
		{"expired", func(ctx context.Context, _ approval.Prompt) (approval.Reply, error) {
			<-ctx.Done()
			return approval.Reply{}, ctx.Err()
		}, false, "in time", "expired"},
		{"reviewer down", nil, false, "unavailable", "unavailable"},
		{"reviewer error", func(context.Context, approval.Prompt) (approval.Reply, error) {
			return approval.Reply{}, errors.New("boom")
		}, false, "approval store failed", "failed"},
		{"store failure", choose(approval.AllowOnce), true, "approval store failed", "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up, got := fakeUpstream(t)
			g, logPath, _ := approvalGateway(t, up.URL, tc.review)
			// only the expiry case waits out the timeout; the others get room
			// to finish on a slow runner, where 100 ms raced the store's write
			g.appr.Timeout = 5 * time.Second
			if tc.decision == "expired" {
				g.appr.Timeout = 100 * time.Millisecond
			}
			if tc.breakDir {
				// a file where the dir should be, so Create can't succeed
				g.appr.Store = approval.Store{Path: filepath.Join(logPath, "approvals.json")}
			}
			w := call(g, "P1", body)
			wantRefusal(t, w, tc.want)
			noUpstream(t, got)
			if d := lastDecision(t, logPath); d != tc.decision {
				t.Errorf("decision %q, want %q", d, tc.decision)
			}
		})
	}
}

func TestClientDisconnectCancelsAndSendsNothing(t *testing.T) {
	up, got := fakeUpstream(t)
	var canceled atomic.Bool
	started := make(chan struct{})
	g, logPath, _ := approvalGateway(t, up.URL, func(ctx context.Context, _ approval.Prompt) (approval.Reply, error) {
		close(started)
		<-ctx.Done()
		canceled.Store(true)
		return approval.Reply{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/linear/mcp", strings.NewReader(toolCall)).WithContext(ctx)
	r.RemoteAddr = "100.101.102.103:41000"
	r.Host = "100.64.0.1:8790"
	r.Header.Set(PrivacyHeader, "P1")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { g.ServeHTTP(w, r); close(done) }()
	<-started
	cancel()
	<-done
	if !canceled.Load() {
		t.Error("the review wasn't canceled")
	}
	noUpstream(t, got)
	if w.Body.Len() != 0 {
		t.Errorf("answered a client that left: %s", w.Body)
	}
	if d := lastDecision(t, logPath); d != "canceled" {
		t.Errorf("decision %q", d)
	}
}

func TestBatchWithAnApprovalTierCallIsRefusedWhole(t *testing.T) {
	up, got := fakeUpstream(t)
	g, logPath, _ := approvalGateway(t, up.URL, choose(approval.AllowOnce))
	body := `[{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue"}},{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"save_issue"}}]`
	w := call(g, "P1", body)
	noUpstream(t, got)
	if !strings.HasPrefix(w.Body.String(), "[") || strings.Count(w.Body.String(), `"isError":true`) != 2 {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
	if recs := readAudit(t, logPath); len(recs) != 2 || recs[0].Decision != "refused" {
		t.Errorf("audit %+v", recs)
	}
}

func TestServerWithoutAnApprovalBlockIsUnchanged(t *testing.T) {
	up, got := fakeUpstream(t)
	g, _ := newGateway(t, up.URL, "P1")
	call(g, "P1", toolCall)
	if s := <-got; s.body != toolCall {
		t.Errorf("upstream got %q", s.body)
	}
}

func TestApprovalGlobsFailClosedAndAskWins(t *testing.T) {
	a := &Approval{Tools: map[string]string{"get_*": "allow", "get_secret": "ask", "list_*": "allow"}}
	for tool, want := range map[string]bool{"get_issue": false, "get_secret": true, "list_x": false, "save_issue": true, "": true} {
		if got := a.Asks(tool); got != want {
			t.Errorf("Asks(%q) = %v, want %v", tool, got, want)
		}
	}
	c := &Config{Listen: "127.0.0.1:0", Servers: map[string]Server{"a": {URL: "http://127.0.0.1:1/mcp", Privacy: []string{"P1"},
		Approval: &Approval{Tools: map[string]string{"[": "allow", "x": "maybe"}}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "valid tool-name glob") || !strings.Contains(err.Error(), "isn't allow or ask") {
		t.Errorf("Validate: %v", err)
	}
}

func TestToolCallsWithOddArgumentsOrUnparsableMessagesAreRefused(t *testing.T) {
	up, got := fakeUpstream(t)
	g, logPath, _ := approvalGateway(t, up.URL, choose(approval.AllowOnce))
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue","arguments":[1]}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"save_issue","arguments":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"save_issue","arguments":7}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":5}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[]}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call"`,
		`[{"jsonrpc":"2.0","id":1,"method":7}]`,
	} {
		w := call(g, "P1", body)
		wantRefusal(t, w, "not made")
		noUpstream(t, got)
		if d := lastDecision(t, logPath); d != "refused" {
			t.Errorf("%s: decision %q", body, d)
		}
	}
}

func TestReviewTargetExcludesUserinfoQueryAndFragment(t *testing.T) {
	u, err := url.Parse("https://user:pw@example.com/mcp?token=s3cret#frag")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := (&upstream{url: u}).publicURL(), "https://example.com/mcp"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
