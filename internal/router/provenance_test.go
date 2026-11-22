package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"jgarner.dev/bud/internal/registry"
)

func TestFromServerMatchesWithAndWithoutTheRuntimePrefix(t *testing.T) {
	servers := []string{"calendar", "mail", "home-notes"}
	for name, want := range map[string]bool{
		"calendar_list_events":       true,
		"mcp__calendar__list_events": true,
		"mcp_calendar_list_events":   true,
		"MCP__Mail__read_message":    true,
		"mail_send_message":          true,
		"home_notes_read":            true,
		"home-notes_read":            true,
		"calendar":                   true,
		"linear_get_issue":           false,
		"mcp__linear__get_issue":     false,
		"mailbox_tool":               false, // another server whose name starts the same
		"mcpcalendar_x":              false,
		"web_search":                 false,
	} {
		if got := fromServer(name, servers); got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	// a server named mcp matches its unprefixed tools and its prefixed ones
	for name, want := range map[string]bool{
		"mcp_list":       true,
		"mcp__mcp__list": true,
		"mcp_mcp_list":   true,
		"mcpx_list":      false,
	} {
		if got := fromServer(name, []string{"mcp"}); got != want {
			t.Errorf("server mcp, %s: got %v, want %v", name, got, want)
		}
	}
}

func TestTaintedReadsEveryPlaceAToolNameSits(t *testing.T) {
	servers := []string{"calendar", "mail"}
	for body, want := range map[string]string{
		`{"messages":[{"role":"user","content":"hi"}]}`: "",
		`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"1","type":"function","function":{"name":"linear_get_issue","arguments":"{}"}},{"id":"2","type":"function","function":{"name":"mcp__calendar__list_events","arguments":"{}"}}]},{"role":"tool","tool_call_id":"2","content":"dentist at 3"}]}`: "mcp__calendar__list_events",
		`{"messages":[{"role":"assistant","function_call":{"name":"mail_read_message","arguments":"{}"}}]}`: "mail_read_message",
		// a history trimmed of the call still names the tool on its result
		`{"messages":[{"role":"tool","name":"calendar_list_events","tool_call_id":"9","content":"x"}]}`: "calendar_list_events",
		// a user who just says the word isn't a call
		`{"messages":[{"role":"user","name":"calendar_list_events","content":"x"}]}`: "",
		`{"messages":"nope"}`:                 "",
		`{"messages":[7,{"tool_calls":"x"}]}`: "",
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &fields); err != nil {
			t.Fatal(err)
		}
		got, ok := tainted(fields, servers)
		if got != want || ok != (want != "") {
			t.Errorf("%s: got %q %v, want %q", body, got, ok, want)
		}
	}
}

// provRig has a local P0 model and a cloud P1 model, both in one role with
// the cloud first, and a cloud recorder that counts every byte it gets
type provRig struct {
	rt         *Router
	localHits  int
	cloudBytes int
	localFails bool
	mu         sync.Mutex
}

func newProvRig(t *testing.T) *provRig {
	t.Helper()
	rig := &provRig{}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		rig.localHits++
		fails := rig.localFails
		rig.mu.Unlock()
		io.Copy(io.Discard, r.Body)
		if fails {
			// a transport error: the router retries on the next member
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		io.WriteString(w, "{}")
	}))
	t.Cleanup(local.Close)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rig.mu.Lock()
		rig.cloudBytes += len(b)
		rig.mu.Unlock()
		io.WriteString(w, "{}")
	}))
	t.Cleanup(cloud.Close)
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"home":  {API: "openai", Endpoint: local.URL, Local: true},
			"cloud": {API: "openai", Endpoint: cloud.URL},
		},
		Models: map[string]registry.Model{
			"local": {Provider: "home", Name: "local-1", Privacy: []string{"P0", "P1", "P2"}},
			"p1":    {Provider: "cloud", Name: "p1-1", Privacy: []string{"P1", "P2"}},
		},
		Roles: map[string]registry.Role{"manager": {Pool: []string{"p1", "local"}}},
	}
	rt, err := New(reg, env(nil), Options{Logf: quiet, P0Servers: []string{"calendar", "mail"}})
	if err != nil {
		t.Fatal(err)
	}
	rig.rt = rt

	return rig
}

const taintedBody = `{"model":"bud/manager","messages":[
	{"role":"user","content":"what's on today?"},
	{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"mcp__calendar__list_events","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"c1","content":"dentist at 3pm"},
	{"role":"user","content":"thanks, and what's the weather?"}]}`

const cleanBody = `{"model":"bud/manager","messages":[{"role":"user","content":"what's the weather?"},
	{"role":"assistant","content":null,"tool_calls":[{"id":"w1","type":"function","function":{"name":"web_search","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"w1","content":"sunny"}]}`

// Suite D119: a calendar call in the history keeps the conversation local,
// and the cloud never sees a byte of it, whichever path the router takes
func TestACalendarCallInTheHistoryKeepsTheConversationLocal(t *testing.T) {
	rig := newProvRig(t)
	w := do(t, rig.rt, "POST", "/v1/chat/completions", cleanBody, p1)
	if w.Code != 200 || w.Header().Get("X-Bud-Model") != "p1" || w.Header().Get(RaisedHeader) != "" {
		t.Fatalf("a clean chat: code %d, model %q, raised %q", w.Code, w.Header().Get("X-Bud-Model"), w.Header().Get(RaisedHeader))
	}
	rig.cloudBytes = 0

	w = do(t, rig.rt, "POST", "/v1/chat/completions", taintedBody, p1)
	if w.Code != 200 || w.Header().Get("X-Bud-Model") != "local" || w.Header().Get(RaisedHeader) != ProvenanceRule {
		t.Fatalf("tainted: code %d, model %q, raised %q", w.Code, w.Header().Get("X-Bud-Model"), w.Header().Get(RaisedHeader))
	}
	// naming the cloud model outright is refused, not sent
	named := strings.Replace(taintedBody, `"bud/manager"`, `"p1"`, 1)
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", named, p1); w.Code != 403 {
		t.Errorf("naming the cloud model: code %d", w.Code)
	}
	// the retry after a transport error goes to no cloud model
	rig.localFails = true
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", taintedBody, p1); w.Code == 200 {
		t.Errorf("with the local model failing: code %d", w.Code)
	}
	// with the local model down and no approvals wired, the D104 fallback
	// answers 503 rather than asking
	rig.rt.health.markDown("home")
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", taintedBody, p1); w.Code != 503 {
		t.Errorf("with the local model down: code %d", w.Code)
	}
	if rig.cloudBytes != 0 {
		t.Errorf("the cloud got %d bytes of a tainted conversation", rig.cloudBytes)
	}
}

// a request a rule already made P0 keeps the rule's name in the header
func TestARuleRaiseKeepsItsName(t *testing.T) {
	rig := newProvRig(t)
	rig.rt.rules = []registry.CompiledRule{}
	reg := &registry.Registry{PrivacyRules: map[string]registry.PrivacyRule{"marker": {Match: `(?i)^\s*#private\b`, Tier: "P0"}}}
	rules, err := reg.CompileRules()
	if err != nil {
		t.Fatal(err)
	}
	rig.rt.rules = rules
	body := strings.Replace(taintedBody, `"what's on today?"`, `"#private what's on today?"`, 1)
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", body, p1); w.Header().Get(RaisedHeader) != "marker" {
		t.Errorf("raised %q, want marker", w.Header().Get(RaisedHeader))
	}
}

func TestReadP0Servers(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "f.json")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if got, err := ReadP0Servers(write(`{"servers": ["calendar", "mail"]}`)); err != nil || len(got) != 2 {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := ReadP0Servers(write(`{"servers": []}`)); err != nil || got == nil {
		t.Errorf("an empty list is fine: %q, %v", got, err)
	}
	for _, bad := range []string{`{}`, `{"servers": ["Cal endar"]}`, `{"servers": [], "x": 1}`, `nope`, `{"servers": []} garbage`, `{"servers": []}{"servers": ["calendar"]}`} {
		if _, err := ReadP0Servers(write(bad)); err == nil || !strings.Contains(err.Error(), "bud sync") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
	if _, err := ReadP0Servers(filepath.Join(dir, "none.json")); err == nil || !strings.Contains(err.Error(), "run bud sync") {
		t.Errorf("missing: err = %v", err)
	}
}
