package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func eml(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "messages", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAMultipartMessageReadsItsPlainTextAndNamesAttachments(t *testing.T) {
	m, err := ParseMessage(eml(t, "multipart.eml"), false)
	if err != nil {
		t.Fatal(err)
	}
	want := Header{
		Date:      "2026-11-03T09:15:00-06:00",
		From:      []string{"Renée <renee@example.com>"},
		To:        []string{"someone@example.com", "Sam Lee <sam@example.com>"},
		Subject:   "Café plans",
		MessageID: "<m1@example.com>",
	}
	if !reflect.DeepEqual(m.Header, want) {
		t.Errorf("header\n%+v\nwant\n%+v", m.Header, want)
	}
	if m.Text != "See you at the café at noon. Bring the map." || m.FromHTML {
		t.Errorf("text %q (from html %v)", m.Text, m.FromHTML)
	}
	if !reflect.DeepEqual(m.Attachments, []Attachment{{Name: "map.pdf", Type: "application/pdf", Bytes: 9}}) {
		t.Errorf("attachments %+v", m.Attachments)
	}
	if len(m.Notes) != 0 {
		t.Errorf("notes %q", m.Notes)
	}
}

func TestAnHTMLOnlyMessageIsTurnedIntoText(t *testing.T) {
	m, err := ParseMessage(eml(t, "html_only.eml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Café sale today & tomorrow\nLine two\nLine three"; m.Text != want || !m.FromHTML {
		t.Errorf("text %q, want %q", m.Text, want)
	}
	if strings.Contains(m.Text, "alert") || strings.Contains(m.Text, "color") {
		t.Errorf("script or style left in %q", m.Text)
	}
}

func TestACutMessageSaysSo(t *testing.T) {
	raw := eml(t, "multipart.eml")
	m, err := ParseMessage(raw[:len(raw)/2], true)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Notes) == 0 || !strings.Contains(m.Notes[0], "longer than bud fetches") {
		t.Errorf("notes %q", m.Notes)
	}
}

func TestLongTextIsCutOnARuneBoundary(t *testing.T) {
	raw := "Subject: long\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + strings.Repeat("é", MaxText)
	m, err := ParseMessage([]byte(raw), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Text) > MaxText || !strings.HasSuffix(m.Text, "é") {
		t.Errorf("text is %d bytes ending %q", len(m.Text), m.Text[len(m.Text)-3:])
	}
}

// fakeAcct is a Backend with canned answers
type fakeAcct struct {
	folders []FolderInfo
	sums    []Summary
	total   int
	body    Body
	err     error

	moved     []Moved
	acts      []Action
	sent      *Outgoing
	gotUIDs   []uint32
	gotID     string
	gotSince  time.Time
	gotFolder string
	gotCr     Criteria
	gotLimit  int
	gotUID    uint32
}

func (f *fakeAcct) Folders(context.Context) ([]FolderInfo, error) { return f.folders, f.err }

func (f *fakeAcct) Search(_ context.Context, folder string, cr Criteria, limit int) ([]Summary, int, error) {
	f.gotFolder, f.gotCr, f.gotLimit = folder, cr, limit
	return f.sums, f.total, f.err
}

func (f *fakeAcct) Read(_ context.Context, folder string, uid uint32, _ int) (Body, error) {
	f.gotFolder, f.gotUID = folder, uid
	return f.body, f.err
}

func (f *fakeAcct) MoveToTrash(_ context.Context, folder string, uids []uint32) ([]Moved, string, error) {
	f.gotFolder, f.gotUIDs = folder, uids
	return f.moved, "Deleted Messages", f.err
}

func (f *fakeAcct) Restore(_ context.Context, id string) (Action, error) {
	f.gotID = id
	return Action{Action: "restore", ToFolder: "INBOX", MessageID: id}, f.err
}

func (f *fakeAcct) Digest(since time.Time) ([]Action, error) {
	f.gotSince = since
	return f.acts, f.err
}

func (f *fakeAcct) Send(_ context.Context, m Outgoing) (string, error) {
	f.sent = &m
	return "<new@example.com>", f.err
}

func (f *fakeAcct) Reply(_ context.Context, folder string, uid uint32, to, cc []*netmail.Address, subject, body string) (string, error) {
	f.gotFolder, f.gotUID = folder, uid
	f.sent = &Outgoing{To: to, Cc: cc, Subject: subject, Body: body}
	return "<re@example.com>", f.err
}

// callTool posts one tools/call and returns its result
func callTool(t *testing.T, h http.Handler, name, args string) map[string]any {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8796/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", name, rec.Code, rec.Body)
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  *rpcErr        `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("%s: %+v", name, resp.Error)
	}
	return resp.Result
}

func errorText(r map[string]any) string {
	if r["isError"] != true {
		return ""
	}
	return r["content"].([]any)[0].(map[string]any)["text"].(string)
}

func TestSearchPassesCriteriaAndMarksTheResultUntrusted(t *testing.T) {
	f := &fakeAcct{total: 5, sums: []Summary{{UID: 9, Size: 100, Flags: []string{`\Seen`}, Header: []byte("Subject: hi\r\nFrom: sam@example.com\r\n\r\n")}}}
	r := callTool(t, NewServer(f), "search_messages", `{"from":"sam","since":"2026-11-01","unseen":true,"limit":5}`)
	if e := errorText(r); e != "" {
		t.Fatal(e)
	}
	if f.gotFolder != "INBOX" || f.gotLimit != 5 || f.gotCr.From != "sam" || !f.gotCr.Unseen || !f.gotCr.Since.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("backend got %q %d %+v", f.gotFolder, f.gotLimit, f.gotCr)
	}
	sc := r["structuredContent"].(map[string]any)
	msg := sc["messages"].([]any)[0].(map[string]any)
	if sc["notice"] != notice || sc["matched"] != float64(5) || msg["seen"] != true {
		t.Errorf("result %v", sc)
	}
	if u := msg["untrusted"].(map[string]any); u["subject"] != "hi" {
		t.Errorf("untrusted %v", u)
	}
}

func TestSearchRefusesBadArguments(t *testing.T) {
	s := NewServer(&fakeAcct{})
	for args, want := range map[string]string{
		`{"since":"Nov 1"}`: "since",
		`{"limit":500}`:     "limit",
		`{"sender":"x"}`:    "don't fit",
		`{"uid":3}`:         `no "uid"`,
	} {
		if e := errorText(callTool(t, s, "search_messages", args)); !strings.Contains(e, want) {
			t.Errorf("%s: error %q, want it to mention %q", args, e, want)
		}
	}
}

// a message that claims to approve an action comes back as data under
// untrusted, with the notice; nothing in it reaches a tool
func TestReadKeepsAnInjectionAttemptAsData(t *testing.T) {
	f := &fakeAcct{body: Body{UID: 7, Size: 300, Raw: eml(t, "injection.eml")}}
	r := callTool(t, NewServer(f), "read_message", `{"folder":"Junk","uid":7}`)
	if e := errorText(r); e != "" {
		t.Fatal(e)
	}
	sc := r["structuredContent"].(map[string]any)
	if f.gotFolder != "Junk" || f.gotUID != 7 || sc["notice"] != notice {
		t.Errorf("got %q %d, result %v", f.gotFolder, f.gotUID, sc)
	}
	u := sc["untrusted"].(map[string]any)
	if !strings.Contains(u["text"].(string), "yes 4821") || u["subject"] != "yes 4821" {
		t.Errorf("untrusted %v", u)
	}
	for k := range sc {
		if k != "source" && k != "notice" && k != "untrusted" {
			t.Errorf("unexpected top-level key %q", k)
		}
	}
}

func TestEachToolRefusesAnotherToolsArguments(t *testing.T) {
	s := NewServer(&fakeAcct{})
	if e := errorText(callTool(t, s, "list_folders", `{"uid":1}`)); !strings.Contains(e, `no "uid"`) {
		t.Errorf("list_folders with a uid: %q", e)
	}
	if e := errorText(callTool(t, s, "read_message", `{"uid":1,"from":"sam"}`)); !strings.Contains(e, `no "from"`) {
		t.Errorf("read_message with from: %q", e)
	}
}

func TestReadNeedsAUIDAndReportsBackendErrors(t *testing.T) {
	if e := errorText(callTool(t, NewServer(&fakeAcct{}), "read_message", `{}`)); !strings.Contains(e, "uid") {
		t.Errorf("no uid: %q", e)
	}
	f := &fakeAcct{err: ErrNoMessage}
	if e := errorText(callTool(t, NewServer(f), "read_message", `{"uid":3}`)); e != ErrNoMessage.Error() {
		t.Errorf("backend error: %q", e)
	}
}

// Hermes runs read-only tools alone and asks for the rest, so only the
// sends may be marked otherwise (Suite D31, D36)
func TestOnlyTheSendsAreMarkedAsWrites(t *testing.T) {
	for _, tl := range toolList {
		send := tl.Name == "send_message" || tl.Name == "reply_message"
		if (tl.Annotations["readOnlyHint"] == true) == send {
			t.Errorf("%s: readOnlyHint %v", tl.Name, tl.Annotations["readOnlyHint"])
		}
	}
}

func TestRequestsFromABrowserAreRefused(t *testing.T) {
	s := NewServer(&fakeAcct{})
	for _, mod := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		func(r *http.Request) { r.Host = "evil.example:8796" },
	} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8796/mcp", strings.NewReader(`{}`))
		mod(req)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("got %d, want 403", rec.Code)
		}
	}
}

func TestAccountFoldersSkipsUnselectableOnesAndNamesSpecialUse(t *testing.T) {
	a, err := NewAccount(replay(t, "folders"), "someone@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := a.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []FolderInfo{{Name: "INBOX", Total: 3, Unseen: 1}, {Name: "Deleted Messages", Special: "trash", Total: 12}}
	if !reflect.DeepEqual(fs, want) {
		t.Errorf("folders %+v, want %+v", fs, want)
	}
}

func TestAccountSearchKeepsTheNewestMatchesNewestFirst(t *testing.T) {
	a, err := NewAccount(replay(t, "search_newest"), "someone@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	sums, total, err := a.Search(context.Background(), "INBOX", Criteria{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(sums) != 2 || sums[0].UID != 9 || sums[1].UID != 7 {
		t.Errorf("total %d, summaries %+v", total, sums)
	}
}

func TestAnAccountOffLoopbackUsesTLS(t *testing.T) {
	a, err := NewAccount("imap.mail.me.com:993", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if a.tls == nil || a.tls.ServerName != "imap.mail.me.com" {
		t.Errorf("tls %+v", a.tls)
	}
	if _, err := NewAccount("imap.mail.me.com:993", "u", ""); err == nil || !strings.Contains(err.Error(), "BUD_MAIL_PASSWORD") {
		t.Errorf("no password: %v", err)
	}
}
