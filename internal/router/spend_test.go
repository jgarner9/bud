package router

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/registry"
)

var allTiers = []string{"P0", "P1", "P2"}

func spendRegistry(endpoint string) *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{"go": {API: "openai", Endpoint: endpoint}},
		Models: map[string]registry.Model{
			"inc1":  {Provider: "go", Name: "inc1-up", Privacy: allTiers, Cost: "C1", Allowance: 1, Price: &registry.Price{In: 1, Out: 2}},
			"inc2":  {Provider: "go", Name: "inc2-up", Privacy: allTiers, Cost: "C1", Allowance: 5, Price: &registry.Price{In: 1, Out: 2}},
			"paid":  {Provider: "go", Name: "paid-up", Privacy: allTiers, Cost: "C2", Price: &registry.Price{In: 10, Out: 10}},
			"local": {Provider: "go", Name: "local-up", Privacy: allTiers, Cost: "C0", Price: &registry.Price{In: 10, Out: 10}},
		},
		Roles: map[string]registry.Role{
			"a": {Pool: []string{"inc1", "inc2"}},
			"b": {Pool: []string{"inc1", "paid"}},
		},
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSpendRouter(t *testing.T, endpoint, path string, cap float64, c *clock) (*Router, *Ledger) {
	t.Helper()
	reg := spendRegistry(endpoint)
	l, err := NewLedger(path, reg.Models)
	if err != nil {
		t.Fatal(err)
	}
	l.Now = c.now
	rt, err := New(reg, env(nil), Options{Ledger: l, TopUpCap: cap, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	return rt, l
}

func jsonUpstream(usage string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Bud-Spend", "forged")
		io.WriteString(w, `{"choices":[],"usage":`+usage+`}`)
	}))
}

func chatBody(model string) string { return `{"model":"` + model + `","messages":[]}` }

func TestAJSONResponsesUsageIsCharged(t *testing.T) {
	up := jsonUpstream(`{"prompt_tokens":1000000,"completion_tokens":500000}`)
	defer up.Close()
	c := &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}
	rt, l := newSpendRouter(t, up.URL, "", 10, c)
	w := do(t, rt, "POST", "/v1/chat/completions", chatBody("inc1"), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"prompt_tokens":1000000`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// 1 + 0.5*2 = 2 dollars against a 1 dollar allowance
	if got := l.TopUp(); got < 0.999 || got > 1.001 {
		t.Errorf("topup = %v, want 1", got)
	}
	if l.Left("inc1") != 0 {
		t.Errorf("left = %v", l.Left("inc1"))
	}
}

func TestXBudSpendShowsTheTotalBeforeTheRequestAndReplacesAnUpstreamOne(t *testing.T) {
	up := jsonUpstream(`{"prompt_tokens":1000000,"completion_tokens":0}`)
	defer up.Close()
	rt, _ := newSpendRouter(t, up.URL, "", 20, &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	if got := do(t, rt, "POST", "/v1/chat/completions", chatBody("paid"), nil).Header().Get("X-Bud-Spend"); got != "topup=0.00/20.00" {
		t.Errorf("first = %q", got)
	}
	if got := do(t, rt, "POST", "/v1/chat/completions", chatBody("paid"), nil).Header().Get("X-Bud-Spend"); got != "topup=10.00/20.00" {
		t.Errorf("second = %q", got)
	}
}

func TestAnSSEStreamPassesUnchangedAndItsFinalUsageIsCharged(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":null}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100000,\"completion_tokens\":0}}\n\n" +
		"data: [DONE]\n\n"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, stream)
	}))
	defer up.Close()
	rt, l := newSpendRouter(t, up.URL, "", 10, &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"paid","stream":true,"messages":[]}`, nil)
	if w.Body.String() != stream {
		t.Errorf("stream changed: %q", w.Body)
	}
	if got := l.TopUp(); got < 0.999 || got > 1.001 {
		t.Errorf("topup = %v, want 1", got)
	}
}

func TestIncludeUsageIsAddedOnlyToAStreamWithoutStreamOptions(t *testing.T) {
	var got string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		io.WriteString(w, `{}`)
	}))
	defer up.Close()
	rt, _ := newSpendRouter(t, up.URL, "", 10, &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	for body, want := range map[string]bool{
		`{"model":"local","stream":true}`:  true,
		`{"model":"local","stream":false}`: false,
		`{"model":"local"}`:                false,
		`{"model":"local","stream":true,"stream_options":{"include_usage":false}}`: false,
	} {
		do(t, rt, "POST", "/v1/chat/completions", body, nil)
		if has := strings.Contains(got, `"stream_options":{"include_usage":true}`); has != want {
			t.Errorf("%s: forwarded %s", body, got)
		}
	}
	do(t, rt, "POST", "/v1/chat/completions", `{"model":"local","stream":true,"stream_options":{"include_usage":false}}`, nil)
	if !strings.Contains(got, `"include_usage":false`) {
		t.Errorf("caller's stream_options changed: %s", got)
	}
}

func TestARolePickSkipsAnExhaustedIncludedModel(t *testing.T) {
	up := jsonUpstream(`{"prompt_tokens":2000000,"completion_tokens":0}`)
	defer up.Close()
	rt, _ := newSpendRouter(t, up.URL, "", 10, &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	if got := do(t, rt, "POST", "/v1/chat/completions", chatBody("bud/a"), nil).Header().Get("X-Bud-Model"); got != "inc1" {
		t.Fatalf("first = %q", got)
	}
	if got := do(t, rt, "POST", "/v1/chat/completions", chatBody("bud/a"), nil).Header().Get("X-Bud-Model"); got != "inc2" {
		t.Errorf("second = %q, want inc2", got)
	}
}

func TestARoleFallsToTopUpWhenEveryMemberIsExhaustedAndRefusesAtTheCap(t *testing.T) {
	up := jsonUpstream(`{"prompt_tokens":3000000,"completion_tokens":0}`)
	defer up.Close()
	rt, l := newSpendRouter(t, up.URL, "", 4, &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	do(t, rt, "POST", "/v1/chat/completions", chatBody("bud/b"), nil) // inc1: 3 dollars, 2 of it top-up
	if got := l.TopUp(); got < 1.999 || got > 2.001 {
		t.Fatalf("topup = %v", got)
	}
	w := do(t, rt, "POST", "/v1/chat/completions", chatBody("bud/b"), nil)
	if w.Header().Get("X-Bud-Model") != "inc1" {
		t.Errorf("model = %q, want inc1 spending top-up first", w.Header().Get("X-Bud-Model"))
	}
	w = do(t, rt, "POST", "/v1/chat/completions", chatBody("bud/b"), nil)
	if w.Code != 402 || !strings.Contains(w.Body.String(), "insufficient_quota") || !strings.Contains(w.Body.String(), "$4.00") {
		t.Errorf("role at cap: %d %s", w.Code, w.Body)
	}
	w = do(t, rt, "POST", "/v1/chat/completions", chatBody("paid"), nil)
	if w.Code != 402 {
		t.Errorf("explicit id at cap: %d", w.Code)
	}
	if w = do(t, rt, "POST", "/v1/chat/completions", chatBody("inc2"), nil); w.Code != 200 {
		t.Errorf("in-allowance model refused: %d", w.Code)
	}
	if w = do(t, rt, "GET", "/v1/models", "", nil); !strings.Contains(w.Body.String(), `"paid"`) {
		t.Errorf("models hides paid: %s", w.Body)
	}
}

func TestALocalModelIsNeverCharged(t *testing.T) {
	up := jsonUpstream(`{"prompt_tokens":9000000,"completion_tokens":9000000}`)
	defer up.Close()
	rt, l := newSpendRouter(t, up.URL, "", 0, &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	if w := do(t, rt, "POST", "/v1/chat/completions", chatBody("local"), nil); w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if l.TopUp() != 0 {
		t.Errorf("topup = %v", l.TopUp())
	}
}

func TestANewMonthStartsFromZero(t *testing.T) {
	reg := spendRegistry("http://x.invalid")
	c := &clock{time.Date(2026, 3, 31, 23, 0, 0, 0, time.UTC)}
	l, _ := NewLedger("", reg.Models)
	l.Now = c.now
	l.Charge("paid", 3)
	l.Charge("inc1", 0.5)
	c.t = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if l.TopUp() != 0 || l.Left("inc1") != 1 {
		t.Errorf("topup %v left %v", l.TopUp(), l.Left("inc1"))
	}
}

func TestTheLedgerSurvivesANewLedgerOnTheSameFile(t *testing.T) {
	reg := spendRegistry("http://x.invalid")
	path := filepath.Join(t.TempDir(), "spend.json")
	c := &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}
	l, _ := NewLedger(path, reg.Models)
	l.Now = c.now
	if err := l.Charge("paid", 2.5); err != nil {
		t.Fatal(err)
	}
	if err := l.Charge("inc1", 0.25); err != nil {
		t.Fatal(err)
	}
	l2, err := NewLedger(path, reg.Models)
	if err != nil {
		t.Fatal(err)
	}
	l2.Now = c.now
	if l2.TopUp() != 2.5 || l2.Left("inc1") != 0.75 {
		t.Errorf("topup %v left %v", l2.TopUp(), l2.Left("inc1"))
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
}

func TestACorruptLedgerFileIsAnErrorNamingThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spend.json")
	os.WriteFile(path, []byte("{nope"), 0o600)
	_, err := NewLedger(path, nil)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "move it aside") {
		t.Errorf("err = %v", err)
	}
}

func TestAGzipAskingClientIsStillCharged(t *testing.T) {
	// an upstream that compresses whenever it's asked to, as providers do
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"choices":[],"usage":{"prompt_tokens":1000000,"completion_tokens":500000}}`
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			io.WriteString(zw, body)
			zw.Close()
			return
		}
		io.WriteString(w, body)
	}))
	defer up.Close()
	c := &clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}
	rt, l := newSpendRouter(t, up.URL, "", 10, c)
	w := do(t, rt, "POST", "/v1/chat/completions", chatBody("inc1"), map[string]string{"Accept-Encoding": "gzip"})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := l.TopUp(); got < 0.999 || got > 1.001 {
		t.Errorf("topup = %v, want 1: the meter didn't see the usage", got)
	}
}
