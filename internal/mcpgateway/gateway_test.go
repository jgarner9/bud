package mcpgateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/tailnet"
)

// seen is what the fake upstream was sent
type seen struct {
	path, auth, privacy, cookie, session string
	body                                 string
}

// fakeUpstream answers every POST with a JSON-RPC result and records the
// request; hits counts the requests
func fakeUpstream(t *testing.T) (*httptest.Server, chan seen) {
	t.Helper()
	got := make(chan seen, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- seen{
			path: r.URL.Path, auth: r.Header.Get("Authorization"), privacy: r.Header.Get(PrivacyHeader),
			cookie: r.Header.Get("Cookie"), session: r.Header.Get("Mcp-Session-Id"), body: string(b),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "s-up")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// newGateway serves one "linear" server at upstream, with privacy tiers and
// a key in LINEAR_API_KEY, and returns it with its audit log's path
func newGateway(t *testing.T, upstream string, privacy ...string) (*Gateway, string) {
	t.Helper()
	c := &Config{Listen: "127.0.0.1:0", Servers: map[string]Server{
		"linear": {URL: upstream + "/mcp", Privacy: privacy, ReadOnly: true, Key: &Key{Header: "Authorization", Prefix: "Bearer ", Env: "LINEAR_API_KEY"}},
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
	env := map[string]string{"LINEAR_API_KEY": "lin_secret"}
	g, err := New(c, a, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	return g, logPath
}

func call(g http.Handler, tier, body string, edit ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/linear/mcp", strings.NewReader(body))
	r.RemoteAddr = "100.101.102.103:41000"
	r.Host = "100.64.0.1:8790"
	r.Header.Set("Content-Type", "application/json")
	if tier != "" {
		r.Header.Set(PrivacyHeader, tier)
	}
	for _, e := range edit {
		e(r)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}

func readAudit(t *testing.T, path string) []Record {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

const toolCall = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create_issue","arguments":{"title":"private words","team":"x"}}}`

func TestAllowedCallGetsTheKeyAndIsLogged(t *testing.T) {
	up, got := fakeUpstream(t)
	g, logPath := newGateway(t, up.URL, "P1", "P2")

	w := call(g, "P1", toolCall, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer client-token")
		r.Header.Set("Cookie", "a=b")
		r.Header.Set("Mcp-Session-Id", "s-client")
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if w.Header().Get("Mcp-Session-Id") != "s-up" {
		t.Errorf("session header not passed back: %q", w.Header().Get("Mcp-Session-Id"))
	}
	s := <-got
	// the fake upstream is on loopback, so it is told the checked tier (D108)
	if s.path != "/mcp" || s.auth != "Bearer lin_secret" || s.privacy != "P1" || s.cookie != "" || s.session != "s-client" || s.body != toolCall {
		t.Errorf("upstream saw %+v", s)
	}

	recs := readAudit(t, logPath)
	if len(recs) != 1 {
		t.Fatalf("want 1 audit record, got %+v", recs)
	}
	r := recs[0]
	if r.Server != "linear" || r.Tier != "P1" || r.Tool != "create_issue" || r.Decision != "allowed" || r.Status != 200 ||
		r.Client != "100.101.102.103" || strings.Join(r.Args, ",") != "team,title" {
		t.Errorf("audit record %+v", r)
	}
	if b, _ := os.ReadFile(logPath); strings.Contains(string(b), "private words") || strings.Contains(string(b), "lin_secret") {
		t.Errorf("audit log holds an argument value or the key: %s", b)
	}
}

func TestOnlyToolCallsAreLoggedWhenAllowed(t *testing.T) {
	up, got := fakeUpstream(t)
	g, logPath := newGateway(t, up.URL, "P1")
	call(g, "P1", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	<-got
	if recs := readAudit(t, logPath); len(recs) != 0 {
		t.Errorf("tools/list logged: %+v", recs)
	}
}

func TestP0IsRefusedAndNothingGoesUpstream(t *testing.T) {
	up, got := fakeUpstream(t)
	g, logPath := newGateway(t, up.URL, "P1", "P2")

	for _, tier := range []string{"P0", ""} { // no header counts as P0
		w := call(g, tier, toolCall)
		if w.Code != http.StatusForbidden {
			t.Fatalf("tier %q: status %d", tier, w.Code)
		}
		var reply struct {
			ID    int `json:"id"`
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatalf("reply %s: %v", w.Body, err)
		}
		if reply.ID != 7 || reply.Error.Code != refusedCode || !strings.Contains(reply.Error.Message, "D81") {
			t.Errorf("reply %+v", reply)
		}
	}
	select {
	case s := <-got:
		t.Fatalf("a P0 request reached the upstream: %+v", s)
	default:
	}
	recs := readAudit(t, logPath)
	if len(recs) != 2 || recs[0].Decision != "refused" || recs[0].Tier != "P0" || recs[0].Tool != "create_issue" || recs[1].Tier != "P0" {
		t.Errorf("audit %+v", recs)
	}
}

func TestRefusedBatchAnswersEachRequest(t *testing.T) {
	up, _ := fakeUpstream(t)
	g, logPath := newGateway(t, up.URL, "P2")
	body := `[{"jsonrpc":"2.0","id":1,"method":"tools/list"},{"jsonrpc":"2.0","method":"notifications/initialized"},` + toolCall + `]`
	w := call(g, "P1", body)
	var replies []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &replies); err != nil {
		t.Fatalf("reply %s: %v", w.Body, err)
	}
	if len(replies) != 2 {
		t.Errorf("want a reply per request (not the notification), got %s", w.Body)
	}
	if recs := readAudit(t, logPath); len(recs) != 3 {
		t.Errorf("want every refused message logged, got %+v", recs)
	}
}

func TestRequestsTheGatewayTurnsAway(t *testing.T) {
	up, got := fakeUpstream(t)
	g, _ := newGateway(t, up.URL, "P1")
	tests := []struct {
		name string
		tier string
		edit func(*http.Request)
		want int
	}{
		{"unknown tier", "P3", nil, http.StatusBadRequest},
		{"LAN client", "P1", func(r *http.Request) { r.RemoteAddr = "192.168.1.20:5000" }, http.StatusForbidden},
		{"public client", "P1", func(r *http.Request) { r.RemoteAddr = "[2001:db8::1]:5000" }, http.StatusForbidden},
		{"web page", "P1", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
		{"rebound domain", "P1", func(r *http.Request) { r.Host = "evil.example:8790" }, http.StatusForbidden},
		{"public IP host", "P1", func(r *http.Request) { r.Host = "203.0.113.5" }, http.StatusForbidden},
		{"unknown server", "P1", func(r *http.Request) { r.URL.Path = "/github/mcp" }, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := []func(*http.Request){}
			if tt.edit != nil {
				edits = append(edits, tt.edit)
			}
			if w := call(g, tt.tier, toolCall, edits...); w.Code != tt.want {
				t.Errorf("status %d, want %d: %s", w.Code, tt.want, w.Body)
			}
		})
	}
	select {
	case s := <-got:
		t.Fatalf("a turned-away request reached the upstream: %+v", s)
	default:
	}
}

func TestLoopbackClientsAreServed(t *testing.T) {
	up, got := fakeUpstream(t)
	g, _ := newGateway(t, up.URL, "P1")
	for _, addr := range []string{"127.0.0.1:5000", "[::1]:5000", "[fd7a:115c:a1e0::5]:5000"} {
		host := func(r *http.Request) { r.RemoteAddr = addr; r.Host = addr }
		if w := call(g, "P1", toolCall, host); w.Code != http.StatusOK {
			t.Errorf("%s: status %d", addr, w.Code)
		}
		<-got
	}
}

func TestEventsStreamAsTheyArrive(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	defer up.Close()
	defer close(release)
	g, _ := newGateway(t, up.URL, "P1")
	front := httptest.NewServer(g) // a real listener on loopback
	defer front.Close()

	req, _ := http.NewRequest(http.MethodPost, front.URL+"/linear/mcp", strings.NewReader(toolCall))
	req.Header.Set(PrivacyHeader, "P1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- s
	}()
	select {
	case s := <-line:
		if s != "event: message\n" {
			t.Errorf("first line %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event was held back until the stream ended")
	}
}

func TestMissingKeyStopsTheGateway(t *testing.T) {
	c := &Config{Listen: "127.0.0.1:0", Servers: map[string]Server{
		"exa": {URL: "https://mcp.exa.ai/mcp", Privacy: []string{"P2"}, ReadOnly: true, Key: &Key{Header: "x-api-key", Env: "EXA_API_KEY"}},
	}}
	_, err := New(c, nil, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "EXA_API_KEY") || !strings.Contains(err.Error(), "op run") {
		t.Errorf("err = %v", err)
	}
}

func TestACallIsLoggedWhenTheClientLeavesMidStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // the stream stays open until the gateway gives up on it
	}))
	defer up.Close()
	g, logPath := newGateway(t, up.URL, "P1")
	front := httptest.NewServer(g)
	defer front.Close()

	req, _ := http.NewRequest(http.MethodPost, front.URL+"/linear/mcp", strings.NewReader(toolCall))
	req.Header.Set(PrivacyHeader, "P1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if recs := readAudit(t, logPath); len(recs) == 1 && recs[0].Tool == "create_issue" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the call the upstream ran was never logged")
}

func TestInterimStatusesAreNotTheAuditedOne(t *testing.T) {
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	sw.WriteHeader(http.StatusEarlyHints)
	sw.WriteHeader(http.StatusBadGateway)
	if sw.code() != http.StatusBadGateway {
		t.Errorf("code = %d", sw.code())
	}
}

func TestAnExistingAuditLogIsMadePrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := OpenAudit(p)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport failure with canary lin_secret")
}

func TestUpstreamTransportErrorDoesNotExposeCredentials(t *testing.T) {
	g, auditPath := newGateway(t, "http://127.0.0.1:1234", "P1")
	g.servers["linear"].proxy.Transport = failingTransport{}
	w := call(g, "P1", toolCall)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "linear didn't answer") {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "lin_secret") {
		t.Fatal("transport error exposed the upstream credential")
	}
	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "lin_secret") {
		t.Fatal("audit log exposed the upstream credential")
	}
}

func TestTierReachesOnlyUpstreamsInsideTheHouse(t *testing.T) {
	up, got := fakeUpstream(t)
	g, _ := newGateway(t, up.URL, "P0", "P1")
	// no header is P0, and the upstream is told so rather than nothing
	if w := call(g, "", toolCall); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if s := <-got; s.privacy != "P0" {
		t.Errorf("a request with no tier reached a loopback upstream as %q, want P0", s.privacy)
	}

	for _, tt := range []struct {
		url  string
		want string
	}{
		{"http://127.0.0.1:8793/mcp", "P2"},
		{"http://100.70.1.2:8793/mcp", "P2"},
		{"https://mcp.linear.app/mcp", ""},
	} {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		in := httptest.NewRequest(http.MethodPost, "/x/mcp", nil)
		in.Header.Set(PrivacyHeader, "P2")
		pr := &httputil.ProxyRequest{In: in, Out: in.Clone(in.Context())}
		(&upstream{url: u, inside: tailnet.Inside(u.Hostname())}).rewrite(pr)
		if got := pr.Out.Header.Get(PrivacyHeader); got != tt.want {
			t.Errorf("%s was sent tier %q, want %q", tt.url, got, tt.want)
		}
	}
}
