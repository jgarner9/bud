package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"jgarner.dev/bud/internal/registry"
)

func TestTextsReadsEveryStringTheRulesShouldSee(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"string content", `{"messages":[{"role":"user","content":"hi"}]}`, []string{"hi"}},
		{"array content", `{"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"x"}},{"type":"text","text":"b"}]}]}`, []string{"a", "b"}},
		{"tool message", `{"messages":[{"role":"system","content":"s"},{"role":"tool","content":"result"}]}`, []string{"s", "result"}},
		{"prompt string", `{"prompt":"p"}`, []string{"p"}},
		{"prompt array", `{"prompt":["p","q"]}`, []string{"p", "q"}},
		{"input string", `{"input":"i"}`, []string{"i"}},
		{"input array", `{"input":["i","j"]}`, []string{"i", "j"}},
		{"token array skipped", `{"input":[1,2,3]}`, nil},
		{"token arrays skipped", `{"prompt":[[1,2],"q"]}`, []string{"q"}},
		{"malformed messages skipped", `{"messages":"nope"}`, nil},
		{"malformed message skipped", `{"messages":[7,{"content":5},{"content":"ok"}]}`, []string{"ok"}},
		{"null content", `{"messages":[{"role":"assistant","content":null}]}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(c.body), &fields); err != nil {
				t.Fatal(err)
			}
			if got := texts(fields); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("texts = %q, want %q", got, c.want)
			}
		})
	}
}

// rulesServer has a local P0 model and a cloud P1 model on separate fakes, and
// the shipped marker rule
func rulesServer(t *testing.T, cloudHeader string) (rt *Router, localHits, cloudHits *int, logs *[]string) {
	t.Helper()
	var mu sync.Mutex
	localHits, cloudHits, logs = new(int), new(int), new([]string)
	count := func(n *int, hdr string) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*n++
			mu.Unlock()
			io.Copy(io.Discard, r.Body)
			if hdr != "" {
				w.Header().Set(RaisedHeader, hdr)
			}
			io.WriteString(w, "{}")
		}))
		t.Cleanup(s.Close)
		return s
	}
	local, cloud := count(localHits, ""), count(cloudHits, cloudHeader)
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"home":  {API: "openai", Endpoint: local.URL, Local: true},
			"cloud": {API: "openai", Endpoint: cloud.URL},
		},
		Models: map[string]registry.Model{
			"local": {Provider: "home", Name: "local-1", Privacy: []string{"P0", "P1", "P2"}},
			"p1":    {Provider: "cloud", Name: "p1-1", Privacy: []string{"P1", "P2"}},
		},
		Roles:        map[string]registry.Role{"work": {Pool: []string{"p1", "local"}}},
		PrivacyRules: map[string]registry.PrivacyRule{"marker": {Match: `(?i)^\s*#private\b`, Tier: "P0"}},
	}
	rt, err := New(reg, env(nil), Options{Logf: func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		*logs = append(*logs, strings.TrimSpace(fmt.Sprintf(f, a...)))
	}})
	if err != nil {
		t.Fatal(err)
	}
	return rt, localHits, cloudHits, logs
}

var p1 = map[string]string{"X-Bud-Privacy": "P1"}

func TestAMarkedP1RoleRequestStaysOnTheLocalModel(t *testing.T) {
	rt, local, cloud, _ := rulesServer(t, "")
	body := `{"model":"bud/work","messages":[{"role":"user","content":"#private my balance"}]}`
	w := do(t, rt, "POST", "/v1/chat/completions", body, p1)
	if w.Code != 200 || w.Header().Get(RaisedHeader) != "marker" || w.Header().Get("X-Bud-Model") != "local" {
		t.Fatalf("code %d, raised %q, model %q", w.Code, w.Header().Get(RaisedHeader), w.Header().Get("X-Bud-Model"))
	}
	if *local != 1 || *cloud != 0 {
		t.Fatalf("local %d, cloud %d hits", *local, *cloud)
	}
}

func TestAMarkedRequestNamingTheCloudModelIs403WithTheHeader(t *testing.T) {
	rt, _, cloud, _ := rulesServer(t, "")
	body := `{"model":"p1","messages":[{"role":"user","content":"#private x"}]}`
	w := do(t, rt, "POST", "/v1/chat/completions", body, p1)
	if w.Code != 403 || w.Header().Get(RaisedHeader) != "marker" || *cloud != 0 {
		t.Fatalf("code %d, raised %q, cloud hits %d", w.Code, w.Header().Get(RaisedHeader), *cloud)
	}
}

func TestAnUnmatchedRequestGoesToTheCloudWithoutTheHeaderEvenIfUpstreamSendsOne(t *testing.T) {
	rt, local, cloud, _ := rulesServer(t, "forged")
	body := `{"model":"p1","messages":[{"role":"user","content":"my #private note"}]}`
	w := do(t, rt, "POST", "/v1/chat/completions", body, p1)
	if w.Code != 200 || *cloud != 1 || *local != 0 {
		t.Fatalf("code %d, local %d, cloud %d", w.Code, *local, *cloud)
	}
	if got := w.Result().Header.Values(RaisedHeader); len(got) != 0 {
		t.Fatalf("%s = %v, want none", RaisedHeader, got)
	}
}

func TestTheRaiseLogLineNamesTheRuleAndTiersButNotTheText(t *testing.T) {
	rt, _, _, logs := rulesServer(t, "")
	body := `{"model":"bud/work","messages":[{"role":"user","content":"#private secret-sauce"}]}`
	do(t, rt, "POST", "/v1/chat/completions", body, p1)
	all := strings.Join(*logs, "\n")
	if !strings.Contains(all, "router: rule marker raised a P1 request to P0") {
		t.Fatalf("log = %q", all)
	}
	if strings.Contains(all, "secret-sauce") || strings.Contains(all, "#private") {
		t.Fatalf("log leaks the message: %q", all)
	}
}

func TestNewFailsWhenAPrivacyRuleDoesntCompile(t *testing.T) {
	reg := tierRegistry("http://127.0.0.1:1")
	reg.PrivacyRules = map[string]registry.PrivacyRule{"broken": {Match: "(", Tier: "P0"}}
	_, err := New(reg, env(nil), Options{})
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err = %v, want one naming the rule", err)
	}
}
