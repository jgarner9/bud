package mcpops

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/doctor"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/syncer"
)

// post sends body to s as a loopback client and decodes the answer
func post(t *testing.T, s http.Handler, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8792/mcp", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var out map[string]any
	if w.Body.Len() > 0 && strings.HasPrefix(strings.TrimSpace(w.Body.String()), "{") {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("answer %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, out
}

func fakeSources(t *testing.T, results []doctor.Result, got *doctor.Options) Sources {
	t.Helper()
	return Sources{
		Doctor: func(o doctor.Options) []doctor.Result { *got = o; return results },
		Paths:  paths.Resolve,
	}
}

func call(name, args string) string {
	return `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

// toolText is a tool result's text and whether it's an error
func toolText(t *testing.T, out map[string]any) (string, bool) {
	t.Helper()
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", out)
	}
	content := res["content"].([]any)
	return content[0].(map[string]any)["text"].(string), res["isError"].(bool)
}

func errCode(out map[string]any) float64 {
	e, _ := out["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestInitializeAgreesOnAVersion(t *testing.T) {
	s := New(Tools(Defaults()))
	for asked, want := range map[string]string{"2025-03-26": "2025-03-26", "2025-11-25": "2025-11-25", "1999-01-01": ProtocolVersion, "": ProtocolVersion} {
		_, out := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+asked+`"}}`)
		res := out["result"].(map[string]any)
		if res["protocolVersion"] != want {
			t.Errorf("asked %q: got %v, want %s", asked, res["protocolVersion"], want)
		}
		if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
			t.Errorf("no tools capability: %v", res)
		}
	}
	if code, out := post(t, s, `{"jsonrpc":"2.0","id":2,"method":"ping"}`); code != 200 || out["result"] == nil {
		t.Errorf("ping: %d %v", code, out)
	}
}

// every tool is listed, in order, marked read-only
func TestToolsListIsReadOnly(t *testing.T) {
	_, out := post(t, New(Tools(Defaults())), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, tl := range tools {
		m := tl.(map[string]any)
		names = append(names, m["name"].(string))
		if m["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Errorf("%s isn't read-only", m["name"])
		}
		if m["inputSchema"].(map[string]any)["type"] != "object" {
			t.Errorf("%s schema: %v", m["name"], m["inputSchema"])
		}
	}
	if got := strings.Join(names, " "); got != "doctor sync_status spend approvals reviews" {
		t.Errorf("tools: %s", got)
	}
}

func TestDoctorReportsCountsAndResults(t *testing.T) {
	var got doctor.Options
	s := New(Tools(fakeSources(t, []doctor.Result{
		{Name: "neovim", Status: doctor.OK, Detail: "0.12.1"},
		{Name: "sync ledger", Status: doctor.Warn, Detail: "1 file changed", Fix: "bud sync"},
		{Name: "registry", Status: doctor.Fail, Detail: "bad"},
	}, &got)))

	_, out := post(t, s, call("doctor", `{}`))
	text, isErr := toolText(t, out)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	// quick, and never the network, since P0 sessions may call it
	if !got.Quick || got.Net {
		t.Errorf("options %+v, want Quick and not Net", got)
	}
	var rep doctorOut
	if err := json.Unmarshal([]byte(text), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Warnings != 1 || len(rep.Results) != 3 || rep.Results[1] != (check{"sync ledger", "warn", "1 file changed", "bud sync"}) || rep.Results[2].Status != "fail" {
		t.Errorf("report %+v", rep)
	}
	for _, bad := range []string{`{"net":true}`, `{"x":1}`} {
		if _, out := post(t, s, call("doctor", bad)); errCode(out) != -32602 {
			t.Errorf("%s: %v", bad, out)
		}
	}
}

// the queues that aren't built say so as a tool error, never an empty list
func TestUnbuiltToolsSaySo(t *testing.T) {
	s := New(Tools(Defaults()))
	for _, name := range []string{"spend", "reviews"} {
		_, out := post(t, s, call(name, `{}`))
		text, isErr := toolText(t, out)
		if !isErr || !strings.Contains(text, "isn't built yet") {
			t.Errorf("%s: %q %v", name, text, isErr)
		}
	}
}

func TestProtocolErrors(t *testing.T) {
	s := New(Tools(Defaults()))
	if _, out := post(t, s, call("nope", `{}`)); errCode(out) != -32602 {
		t.Errorf("unknown tool: %v", out)
	}
	if _, out := post(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`); errCode(out) != -32601 {
		t.Errorf("unknown method: %v", out)
	}
	if _, out := post(t, s, `{"id":1,"method":"ping"}`); errCode(out) != -32600 {
		t.Errorf("not 2.0: %v", out)
	}
	if code, out := post(t, s, `not json`); code != 400 || errCode(out) != -32700 {
		t.Errorf("parse error: %d %v", code, out)
	}
	if code, _ := post(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != 202 {
		t.Errorf("notification: %d", code)
	}
	if code, _ := post(t, s, `{"jsonrpc":"2.0","id":9,"result":{}}`); code != 202 {
		t.Errorf("response: %d", code)
	}
	// malformed messages without an id aren't notifications
	for _, bad := range []string{`{}`, `null`, `{"method":"ping"}`, `{"jsonrpc":"2.0"}`} {
		code, out := post(t, s, bad)
		if code != 200 || errCode(out) != -32600 || out["id"] != nil {
			t.Errorf("%s: %d %v", bad, code, out)
		}
	}

	// a batch is answered as a batch, leaving the notification out
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8792/mcp", strings.NewReader(
		`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`))
	r.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var batch []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil || len(batch) != 2 {
		t.Errorf("batch: %s %v", w.Body.String(), err)
	}
}

// only loopback clients that name it by IP, with no Origin, and only POST
func TestRefusesAnythingButLoopbackPosts(t *testing.T) {
	s := New(Tools(Defaults()))
	cases := []struct {
		name, method, host, remote, origin string
		want                               int
	}{
		{"tailnet peer", "POST", "127.0.0.1:8792", "100.64.0.9:5000", "", 403},
		{"rebound host", "POST", "evil.example:8792", "127.0.0.1:5000", "", 403},
		{"origin", "POST", "127.0.0.1:8792", "127.0.0.1:5000", "http://127.0.0.1", 403},
		{"get", "GET", "127.0.0.1:8792", "127.0.0.1:5000", "", 405},
		{"delete", "DELETE", "localhost:8792", "[::1]:5000", "", 405},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://127.0.0.1:8792/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		r.Host, r.RemoteAddr = c.host, c.remote
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.want)
		}
	}
}

func TestCheckListen(t *testing.T) {
	for listen, ok := range map[string]bool{
		"127.0.0.1:8792": true, "[::1]:8792": true,
		"0.0.0.0:8792": false, ":8792": false, "localhost:8792": false, "100.64.0.1:8792": false, "127.0.0.1": false,
	} {
		if err := CheckListen(listen); (err == nil) != ok {
			t.Errorf("%s: %v", listen, err)
		}
	}
}

// syncHome points bud's paths at temp dirs and returns them
func syncHome(t *testing.T) *paths.Paths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func syncCall(t *testing.T) (string, bool) {
	t.Helper()
	_, out := post(t, New(Tools(Defaults())), call("sync_status", `{}`))
	return toolText(t, out)
}

func TestSyncStatusBeforeAnySync(t *testing.T) {
	syncHome(t)
	text, isErr := syncCall(t)
	var got syncOut
	if isErr || json.Unmarshal([]byte(text), &got) != nil || got.Synced || got.Note == "" {
		t.Errorf("%v %s", isErr, text)
	}
}

func TestSyncStatusReportsTheLedgerAndDrift(t *testing.T) {
	p := syncHome(t)
	dir := t.TempDir()
	same, changed, gone := filepath.Join(dir, "same"), filepath.Join(dir, "changed"), filepath.Join(dir, "gone")
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	for path, body := range map[string]string{same: "a", changed: "edited by hand"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	led := syncer.Ledger{
		Format: syncer.LedgerFormat, Device: "server", Channel: "stable", Content: "2026.11.15.1",
		RolledBack: []string{"2026.11.16.1"},
		Overlay:    &syncer.OverlayRef{Repo: "git@example.com:o/overlay.git", Commit: "abc123"},
		Files: []syncer.File{
			{Path: same, SHA256: sum("a"), Kind: "config", Layer: "rendered"},
			{Path: changed, SHA256: sum("b"), Kind: "config", Layer: "rendered"},
			{Path: gone, SHA256: sum("c"), Kind: "agents", Layer: "public"},
		},
	}
	b, _ := json.Marshal(led)
	if err := os.MkdirAll(p.State, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.SyncLedger(), b, 0o644); err != nil {
		t.Fatal(err)
	}

	text, isErr := syncCall(t)
	if isErr {
		t.Fatal(text)
	}
	var got syncOut
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	want := []drift{{changed, "changed"}, {gone, "missing"}}
	if !got.Synced || got.Device != "server" || got.Channel != "stable" || got.Content != "2026.11.15.1" ||
		got.Overlay == nil || got.Overlay.Commit != "abc123" || len(got.RolledBack) != 1 ||
		len(got.Drift) != 2 || got.Drift[0] != want[0] || got.Drift[1] != want[1] {
		t.Errorf("status %s", text)
	}

	// while something else holds the sync lock, it says to ask again
	unlock, err := syncer.TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if text, isErr := syncCall(t); !isErr || !strings.Contains(text, "ask again") {
		t.Errorf("busy: %v %s", isErr, text)
	}
}

func TestApprovalsToolCountsPendingRequestsWithoutIds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals.json")
	st := approval.Store{Path: path}
	now := time.Now()
	for i, action := range []string{"mcp:linear/save_issue", "mcp:linear/save_issue", "mcp:calendar/create_event"} {
		r, err := approval.New(fmt.Sprintf("secret-id-%d", i), "s", action, "t", []byte("{}"), now)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(path)
	src := Defaults()
	src.Paths = func() (*paths.Paths, error) { return &paths.Paths{State: filepath.Dir(path)}, nil }
	s := New(Tools(src))
	_, out := post(t, s, call("approvals", `{}`))
	text, isErr := toolText(t, out)
	if isErr || strings.Contains(text, "secret-id") {
		t.Fatalf("%q %v", text, isErr)
	}
	var got struct {
		Pending  int            `json:"pending"`
		ByAction map[string]int `json:"by_action"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pending != 3 || got.ByAction["mcp:linear/save_issue"] != 2 || got.ByAction["mcp:calendar/create_event"] != 1 {
		t.Errorf("got %+v", got)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("the tool wrote the store")
	}
	// no store yet is an empty queue, and creates nothing
	src.Paths = func() (*paths.Paths, error) { return &paths.Paths{State: t.TempDir()}, nil }
	empty := t.TempDir()
	src.Paths = func() (*paths.Paths, error) { return &paths.Paths{State: empty}, nil }
	_, out = post(t, New(Tools(src)), call("approvals", `{}`))
	if text, isErr := toolText(t, out); isErr || !strings.Contains(text, `"pending": 0`) {
		t.Errorf("empty: %q %v", text, isErr)
	}
	if entries, _ := os.ReadDir(empty); len(entries) != 0 {
		t.Errorf("the tool created %v", entries)
	}
}
