package mcpgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/registry"
)

// the shipped rules (content/env/registry.jsonc), compiled
func shippedRules(t *testing.T) []registry.CompiledRule {
	t.Helper()
	reg := &registry.Registry{PrivacyRules: map[string]registry.PrivacyRule{
		"marker":      {Match: `(?i)^\s*#private\b`, Tier: "P0"},
		"private-key": {Match: `-----BEGIN [A-Z ]*PRIVATE KEY-----`, Tier: "P0"},
	}}
	rules, err := reg.CompileRules()
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

const keyCall = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create_issue","arguments":{"title":"deploy","body":{"lines":["see","-----BEGIN OPENSSH PRIVATE KEY-----"]}}}}`

func TestTextsReadsEveryStringInParams(t *testing.T) {
	for _, tt := range []struct {
		body string
		want []string
	}{
		{keyCall, []string{"-----BEGIN OPENSSH PRIVATE KEY-----", "create_issue", "deploy", "see"}},
		// a batch, and a resource URI; method, jsonrpc and keys aren't read
		{`[{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///a"}},{"jsonrpc":"2.0","method":"x","params":{"k":["",3,true,null]}}]`, []string{"file:///a"}},
		{`not json`, nil},
		{``, nil},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil},
	} {
		got := texts([]byte(tt.body))
		sort.Strings(got)
		if !slices.Equal(got, tt.want) {
			t.Errorf("texts(%s) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func TestARuleRaisesACallPastAServersTiers(t *testing.T) {
	up, got := fakeUpstream(t)
	g, logPath := newGateway(t, up.URL, "P1", "P2")
	g.SetRules(shippedRules(t))

	w := call(g, "P1", keyCall)
	if w.Code != http.StatusForbidden || w.Header().Get(RaisedHeader) != "private-key" {
		t.Fatalf("status %d, raised %q: %s", w.Code, w.Header().Get(RaisedHeader), w.Body)
	}
	if !strings.Contains(w.Body.String(), "privacy rule private-key made this a P0 request") {
		t.Errorf("the refusal doesn't name the rule: %s", w.Body)
	}
	select {
	case s := <-got:
		t.Fatalf("a raised call went upstream: %+v", s)
	default:
	}
	recs := readAudit(t, logPath)
	if len(recs) != 1 || recs[0].Tier != "P0" || recs[0].Raised != "private-key" || recs[0].Decision != "refused" {
		t.Errorf("audit %+v", recs)
	}
	b, _ := json.Marshal(recs)
	if strings.Contains(string(b), "BEGIN") {
		t.Error("the audit log holds the matched text")
	}

	// the same call without the key isn't raised
	w = call(g, "P1", toolCall)
	if w.Code != http.StatusOK || w.Header().Get(RaisedHeader) != "" {
		t.Fatalf("status %d, raised %q", w.Code, w.Header().Get(RaisedHeader))
	}
	if s := <-got; s.privacy != "P1" {
		t.Errorf("upstream told %q, want P1", s.privacy)
	}
}

func TestARaisedCallReachesAPrivateServerAtTheRaisedTier(t *testing.T) {
	var privacy string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privacy = r.Header.Get(PrivacyHeader)
		w.Header().Set(RaisedHeader, "upstream-says")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`))
	}))
	t.Cleanup(up.Close)
	g, logPath := newGateway(t, up.URL, "P0", "P1", "P2")
	g.SetRules(shippedRules(t))

	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search","arguments":{"query":"  #PRIVATE what did the doctor say"}}}`
	w := call(g, "P2", body)
	if w.Code != http.StatusOK || privacy != "P0" {
		t.Fatalf("status %d, upstream told %q: %s", w.Code, privacy, w.Body)
	}
	if got := w.Header().Values(RaisedHeader); !slices.Equal(got, []string{"marker"}) {
		t.Errorf("raised header %q, want only the gateway's marker", got)
	}
	if recs := readAudit(t, logPath); len(recs) != 1 || recs[0].Tier != "P0" || recs[0].Raised != "marker" || recs[0].Decision != "allowed" {
		t.Errorf("audit %+v", recs)
	}
}

func TestRulesNeverLowerATier(t *testing.T) {
	up, got := fakeUpstream(t)
	g, _ := newGateway(t, up.URL, "P0", "P1")
	rules, err := (&registry.Registry{PrivacyRules: map[string]registry.PrivacyRule{
		"public": {Match: `.`, Tier: "P1"},
	}}).CompileRules()
	if err != nil {
		t.Fatal(err)
	}
	g.SetRules(rules)
	if w := call(g, "P0", toolCall); w.Code != http.StatusOK || w.Header().Get(RaisedHeader) != "" {
		t.Fatalf("status %d, raised %q", w.Code, w.Header().Get(RaisedHeader))
	}
	if s := <-got; s.privacy != "P0" {
		t.Errorf("a P0 call reached the upstream as %q", s.privacy)
	}
}
