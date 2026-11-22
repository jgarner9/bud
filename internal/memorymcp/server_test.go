package memorymcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/registry"
)

// fakeMemory is one memory-service identity
type fakeMemory struct {
	recs   []memory.Record
	query  memory.Query
	staged []memory.Draft
	err    error
}

func (f *fakeMemory) Retrieve(_ context.Context, q memory.Query) ([]memory.Record, error) {
	f.query = q
	return f.recs, f.err
}

func (f *fakeMemory) Stage(_ context.Context, d memory.Draft) (int64, error) {
	f.staged = append(f.staged, d)
	return int64(len(f.staged)), f.err
}

type fakeEmbedder struct {
	err   error
	texts [][]string
}

func (e *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.texts = append(e.texts, texts)
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{float64(i), 1}
	}
	return out, nil
}

func newServer() (*Server, map[string]*fakeMemory, *fakeMemory, *fakeEmbedder) {
	tiers := map[string]*fakeMemory{"P0": {}, "P1": {}, "P2": {}}
	stager, emb := &fakeMemory{}, &fakeEmbedder{}
	s := &Server{Search: map[string]Searcher{}, Stage: stager, Embed: emb, Model: "qwen3-embedding-0.6b"}
	for t, m := range tiers {
		s.Search[t] = m
	}
	return s, tiers, stager, emb
}

// callTool posts one tools/call as a session of tier ("" sends no header)
// and returns the result's structured content, or its error text
func callTool(t *testing.T, s *Server, tier, name, args string) (map[string]any, string) {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Host = "127.0.0.1:8798"
	if tier != "" {
		r.Header.Set("X-Bud-Privacy", tier)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			Structured map[string]any `json:"structuredContent"`
			IsError    bool           `json:"isError"`
		} `json:"result"`
		Error *rpcErr `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		return nil, resp.Error.Message
	}
	if resp.Result.IsError {
		return nil, resp.Result.Content[0].Text
	}
	return resp.Result.Structured, ""
}

func TestSearchUsesTheSessionTiersIdentity(t *testing.T) {
	s, tiers, _, emb := newServer()
	tiers["P1"].recs = []memory.Record{{ID: 4, Source: "hermes", Kind: "fact", Privacy: "P1", Body: "b", CreatedAt: "2026-11-21 09:00:00", Matches: []string{"text", "vector"}}}

	got, errText := callTool(t, s, "P1", "memory_search", `{"query":" router fallback ","since":"2026-11-01","limit":3}`)
	if errText != "" {
		t.Fatal(errText)
	}
	q := tiers["P1"].query
	if q.Query != "router fallback" || q.Model != "qwen3-embedding-0.6b" || q.Since != "2026-11-01" || q.Limit != 3 || len(q.Values) != 2 {
		t.Errorf("retrieve got %+v", q)
	}
	if tiers["P0"].query.Query != "" || tiers["P2"].query.Query != "" {
		t.Error("only the P1 identity should be asked for a P1 session")
	}
	res := got["results"].([]any)
	if len(res) != 1 || res[0].(map[string]any)["source"] != "hermes" || got["note"] != nil {
		t.Errorf("results = %v", got)
	}
	if len(emb.texts) != 1 || emb.texts[0][0] != "router fallback" {
		t.Errorf("embedded %v", emb.texts)
	}

	// no header is P0, the most private identity, never a wider one
	if _, errText := callTool(t, s, "", "memory_search", `{"query":"x"}`); errText != "" || tiers["P0"].query.Query != "x" || tiers["P0"].query.Limit != defLimit {
		t.Errorf("a session with no tier: %q, P0 asked %+v", errText, tiers["P0"].query)
	}
}

func TestSearchFallsBackToWordsWhenTheEmbedderIsDown(t *testing.T) {
	s, tiers, _, emb := newServer()
	emb.err = errors.New("connection refused")
	tiers["P0"].recs = []memory.Record{
		{ID: 1, Source: "hermes", CreatedAt: "2026-11-20 08:00:00"},
		{ID: 2, Source: "opencode", CreatedAt: "2026-11-21 08:00:00"},
		{ID: 3, Source: "hermes", CreatedAt: "2026-11-21 23:59:59"},
	}
	got, errText := callTool(t, s, "P0", "memory_search", `{"query":"q","source":"hermes","since":"2026-11-21","until":"2026-11-21"}`)
	if errText != "" {
		t.Fatal(errText)
	}
	// the service filters the words-only search, so no vectors are sent
	// and the filters go with it
	q := tiers["P0"].query
	if q.Model != "" || q.Values != nil || q.Query != "q" || q.Source != "hermes" || q.Since != "2026-11-21" || q.Until != "2026-11-21" || q.Limit != defLimit {
		t.Errorf("a words-only retrieve got %+v", q)
	}
	if res := got["results"].([]any); len(res) != 3 {
		t.Errorf("results = %v", res)
	}
	if note, _ := got["note"].(string); !strings.Contains(note, "words only") {
		t.Errorf("note = %q", note)
	}
}

func TestSearchArguments(t *testing.T) {
	s, _, _, _ := newServer()
	for args, want := range map[string]string{
		`{"query":"  "}`:                                          "query: say what to look for",
		`{"query":"q","limit":21}`:                                "limit: give 1 to 20",
		`{"query":"q","since":"21 Nov"}`:                          `since: "21 Nov" isn't a date`,
		`{"query":"q","since":"2026-11-22","until":"2026-11-21"}`: "since comes after until",
		`{"query":"q","privacy":"P0"}`:                            "unknown field",
	} {
		if _, errText := callTool(t, s, "P2", "memory_search", args); !strings.Contains(errText, want) {
			t.Errorf("%s: got %q, want %q", args, errText, want)
		}
	}
}

func TestStage(t *testing.T) {
	s, _, stager, _ := newServer()
	got, errText := callTool(t, s, "P1", "memory_stage", `{"source":"hermes","kind":"fact","body":" Jason uses Signal "}`)
	if errText != "" {
		t.Fatal(errText)
	}
	if got["privacy"] != "P1" || got["id"] != 1.0 {
		t.Errorf("stage = %v; the privacy should default to the session's", got)
	}
	d := stager.staged[0]
	if d.Body != "Jason uses Signal" || d.Privacy != "P1" || len(d.DedupeKey) != 64 {
		t.Errorf("staged %+v", d)
	}

	// the same record gets the same key, so the service dedupes a retry
	callTool(t, s, "P1", "memory_stage", `{"source":"hermes","kind":"fact","body":"Jason uses Signal"}`)
	if stager.staged[1].DedupeKey != d.DedupeKey {
		t.Error("a retry should reuse the dedupe key")
	}

	// more private than the session is fine, less private isn't
	if _, errText := callTool(t, s, "P1", "memory_stage", `{"source":"hermes","kind":"fact","body":"b","privacy":"P0"}`); errText != "" {
		t.Errorf("a P1 session staging P0: %s", errText)
	}
	if _, errText := callTool(t, s, "", "memory_stage", `{"source":"hermes","kind":"fact","body":"b","privacy":"P2"}`); !strings.Contains(errText, "a P0 session can't save a P2 record") {
		t.Errorf("a P0 session staging P2: %q", errText)
	}
	for args, want := range map[string]string{
		`{"source":"Hermes","kind":"fact","body":"b"}`:           "source:",
		`{"source":"h","kind":"note","body":"b"}`:                `kind: "note"`,
		`{"source":"h","kind":"fact","body":" "}`:                "body: give the record",
		`{"source":"h","kind":"fact","body":"b","privacy":"P3"}`: `privacy: "P3"`,
	} {
		if _, errText := callTool(t, s, "P2", "memory_stage", args); !strings.Contains(errText, want) {
			t.Errorf("%s: got %q, want %q", args, errText, want)
		}
	}
}

func TestDisconnectedMemoryIsReportedPlainly(t *testing.T) {
	s, tiers, stager, _ := newServer()
	stager.err = fmt.Errorf("memory stage: %w (dial unix: no such file)", memory.ErrUnavailable)
	if _, errText := callTool(t, s, "P0", "memory_stage", `{"source":"h","kind":"fact","body":"b"}`); !strings.Contains(errText, "Bud memory is unavailable") || !strings.Contains(errText, "nothing was saved") {
		t.Errorf("stage while memory is down: %q", errText)
	}
	tiers["P2"].err = &memory.StatusError{Action: "retrieve", Status: 403, Reason: "forbidden"}
	if _, errText := callTool(t, s, "P2", "memory_search", `{"query":"q"}`); !strings.Contains(errText, "refused the request (403 forbidden)") {
		t.Errorf("search refused: %q", errText)
	}
}

func TestRequestsAreChecked(t *testing.T) {
	s, _, _, _ := newServer()
	post := func(host, tier, origin, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		r.Host = host
		if tier != "" {
			r.Header.Set("X-Bud-Privacy", tier)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if c := post("127.0.0.1:8798", "P1", "", list); c != http.StatusOK {
		t.Errorf("tools/list = %d", c)
	}
	if c := post("evil.example:8798", "P1", "", list); c != http.StatusForbidden {
		t.Errorf("a rebound Host = %d, want 403", c)
	}
	if c := post("127.0.0.1:8798", "P1", "http://evil.example", list); c != http.StatusForbidden {
		t.Errorf("an Origin = %d, want 403", c)
	}
	if c := post("127.0.0.1:8798", "P9", "", list); c != http.StatusBadRequest {
		t.Errorf("a bad tier = %d, want 400", c)
	}
	if c := post("127.0.0.1:8798", "P1", "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); c != http.StatusAccepted {
		t.Errorf("a notification = %d, want 202", c)
	}
	if _, errText := callTool(t, s, "P1", "memory_delete", `{}`); !strings.Contains(errText, `no tool is named "memory_delete"`) {
		t.Errorf("an unknown tool: %q", errText)
	}
}

// JGA-389: each P0 record starts with the marker the router raises on, in
// the text a runtime quotes into its history; other records carry none
func TestAPrivateRecordCarriesTheMarkerFirst(t *testing.T) {
	s, tiers, _, _ := newServer()
	tiers["P0"].recs = []memory.Record{
		{ID: 4, Source: "hermes", Kind: "fact", Privacy: "P0", Body: "dentist on Tuesdays"},
		{ID: 5, Source: "hermes", Kind: "fact", Privacy: "P1", Body: "likes plain text"},
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_search","arguments":{"query":"x"}}}`
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Host = "127.0.0.1:8798"
	r.Header.Set("X-Bud-Privacy", "P0")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.Result.Content) != 1 {
		t.Fatalf("%v: %s", err, w.Body)
	}
	text := resp.Result.Content[0].Text
	if strings.Count(text, registry.P0Marker) != 1 {
		t.Fatalf("want the marker once, on the P0 record:\n%s", text)
	}
	if m, id := strings.Index(text, registry.P0Marker), strings.Index(text, `"id": 4`); m < 0 || id < m {
		t.Errorf("the marker should come before the record's other fields:\n%s", text)
	}
}

// Copilot on #116: only a P0 record may carry the marker, so a P1 record
// can't hold a chat local by quoting it
func TestOnlyAP0RecordMayCarryTheMarker(t *testing.T) {
	s, _, stager, _ := newServer()
	if _, errText := callTool(t, s, "P1", "memory_stage", `{"source":"hermes","kind":"fact","body":"see `+registry.P0Marker+`"}`); !strings.Contains(errText, "marker") {
		t.Fatalf("a P1 record with the marker: %q", errText)
	}
	if _, errText := callTool(t, s, "P0", "memory_stage", `{"source":"hermes","kind":"fact","body":"see `+registry.P0Marker+`"}`); errText != "" {
		t.Fatalf("a P0 record with the marker: %q", errText)
	}
	_ = stager
}
