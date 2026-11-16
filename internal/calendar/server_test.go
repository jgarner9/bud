package calendar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*fakeDAV, *Server) {
	f, c := newFake(t)
	s := NewServer(c, chicago)
	s.now = func() time.Time { return time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC) }

	return f, s
}

func post(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8791/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var out map[string]any
	if w.Code == http.StatusOK && !strings.HasPrefix(body, "[") {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("answer %q: %v", w.Body.String(), err)
		}
	}

	return w, out
}

func call(t *testing.T, s *Server, tool string, args string) (map[string]any, bool) {
	t.Helper()
	_, out := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+args+`}}`)
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result in %v", tool, out)
	}
	isErr, _ := res["isError"].(bool)
	if isErr {
		return map[string]any{"error": res["content"].([]any)[0].(map[string]any)["text"]}, true
	}

	return res["structuredContent"].(map[string]any), false
}

func TestServerRefusesBrowsersAndOtherHosts(t *testing.T) {
	_, s := newTestServer(t)
	for _, c := range []struct {
		host, origin, method, path string
		want                       int
	}{
		{"127.0.0.1:8791", "https://evil.example", "POST", "/mcp", 403},
		{"rebound.example:8791", "", "POST", "/mcp", 403},
		{"100.64.0.1:8791", "", "POST", "/mcp", 403},
		{"localhost:8791", "", "GET", "/mcp", 405},
		{"127.0.0.1:8791", "", "POST", "/other", 404},
	} {
		r := httptest.NewRequest(c.method, "http://"+c.host+c.path, strings.NewReader(`{}`))
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%+v: %d", c, w.Code)
		}
	}
}

func TestServerSpeaksMCP(t *testing.T) {
	_, s := newTestServer(t)
	_, out := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || res["capabilities"].(map[string]any)["tools"] == nil {
		t.Errorf("initialize %v", res)
	}
	_, out = post(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if out["result"].(map[string]any)["protocolVersion"] != protocolVersions[0] {
		t.Errorf("an unknown version got %v", out)
	}
	if w, _ := post(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); w.Code != http.StatusAccepted {
		t.Errorf("a notification got %d", w.Code)
	}
	if _, out = post(t, s, `{"jsonrpc":"2.0","id":2,"method":"nope"}`); out["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("an unknown method got %v", out)
	}
	if _, out = post(t, s, `not json`); out["error"] == nil {
		t.Errorf("a bad body got %v", out)
	}

	// reads are marked read-only, so Hermes runs them alone; writes aren't,
	// so it asks
	_, out = post(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	readOnly := map[string]bool{}
	for _, tl := range tools {
		m := tl.(map[string]any)
		readOnly[m["name"].(string)] = m["annotations"].(map[string]any)["readOnlyHint"].(bool)
	}
	want := map[string]bool{"list_calendars": true, "list_events": true, "create_event": false, "move_event": false, "delete_event": false}
	for name, ro := range want {
		if got, ok := readOnly[name]; !ok || got != ro {
			t.Errorf("%s: read-only %v (listed %v)", name, got, ok)
		}
	}
	if len(readOnly) != len(want) {
		t.Errorf("tools %v", readOnly)
	}

	w, _ := post(t, s, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"}]`)
	var batch []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil || len(batch) != 1 {
		t.Errorf("batch answer %s", w.Body.String())
	}
	if _, out = post(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"calendar_nope","arguments":{}}}`); out["error"] == nil {
		t.Errorf("an unknown tool got %v", out)
	}
}

func TestServerToolsWorkTheCalendar(t *testing.T) {
	f, s := newTestServer(t)
	f.put(workPath+"a.ics", event("Dentist", "20261117T150000Z", "20261117T160000Z"))
	f.put(workPath+"late.ics", event("Next month", "20261217T150000Z", "20261217T160000Z"))

	// no arguments is this week, from the start of today in the server's zone
	res, isErr := call(t, s, "list_events", `{}`)
	if isErr {
		t.Fatal(res)
	}
	evs := res["events"].([]any)
	if len(evs) != 1 || evs[0].(map[string]any)["title"] != "Dentist" || res["start"] != "2026-11-16T00:00:00-06:00" || res["end"] != "2026-11-23T00:00:00-06:00" {
		t.Errorf("this week %v", res)
	}

	res, isErr = call(t, s, "create_event", `{"title":"Plumber","start":"2026-11-18T14:30","duration_minutes":45,"alarm_minutes":[30,10,30],"calendar":"Work"}`)
	if isErr {
		t.Fatal(res)
	}
	created := res["created"].(map[string]any)
	if created["start"] != "2026-11-18T14:30:00-06:00" || created["end"] != "2026-11-18T15:15:00-06:00" || len(created["alarm_minutes"].([]any)) != 2 {
		t.Errorf("created %v", created)
	}

	res, isErr = call(t, s, "create_event", `{"title":"Trip","start":"2026-12-01","end":"2026-12-03","calendar":"Work"}`)
	if isErr {
		t.Fatal(res)
	}
	trip := res["created"].(map[string]any)
	if trip["all_day"] != true || trip["end"] != "2026-12-04T00:00:00-06:00" {
		t.Errorf("trip %v", trip)
	}

	id := created["id"].(string)
	res, isErr = call(t, s, "move_event", `{"id":"`+id+`","start":"2026-11-19T09:00:00-06:00"}`)
	if isErr || res["moved"].(map[string]any)["end"] != "2026-11-19T09:45:00-06:00" {
		t.Errorf("moved %v", res)
	}
	if res, isErr = call(t, s, "delete_event", `{"id":"`+id+`"}`); isErr {
		t.Errorf("delete %v", res)
	}

	for _, bad := range []struct{ tool, args, want string }{
		{"list_events", `{"start":"2026-11-20","end":"2026-11-19"}`, "after start"},
		{"list_events", `{"start":"2026-01-01","end":"2027-06-01"}`, "366 days"},
		{"list_events", `{"start":"next tuesday"}`, "isn't a time"},
		{"list_events", `{"when":"today"}`, "don't fit"},
		{"create_event", `{"title":" ","start":"2026-11-18T14:30"}`, "title"},
		{"create_event", `{"title":"x","start":"2026-11-18T14:30","end":"2026-11-18T15:00","duration_minutes":5}`, "not both"},
		{"create_event", `{"title":"x","start":"2026-11-18T14:30","alarm_minutes":[-5],"calendar":"Work"}`, "alarm_minutes"},
		{"create_event", `{"title":"x","start":"2026-11-18T14:30","calendar":"Home"}`, "no calendar is named"},
		{"move_event", `{"start":"2026-11-18T14:30"}`, "id"},
		{"delete_event", `{"id":"/elsewhere/x.ics"}`, "isn't an event id"},
	} {
		res, isErr := call(t, s, bad.tool, bad.args)
		if !isErr || !strings.Contains(res["error"].(string), bad.want) {
			t.Errorf("%s %s: %v", bad.tool, bad.args, res)
		}
	}
}

func TestCheckListen(t *testing.T) {
	for addr, ok := range map[string]bool{"127.0.0.1:8791": true, "[::1]:8791": true, "0.0.0.0:8791": false, ":8791": false, "100.64.0.1:8791": false, "localhost:8791": false, "127.0.0.1": false} {
		if err := CheckListen(addr); (err == nil) != ok {
			t.Errorf("%s: %v", addr, err)
		}
	}
}
