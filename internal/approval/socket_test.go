package approval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fakeReviewer struct {
	socket, secret string
	got            chan reviewRequest
	closed         chan struct{}
}

// newFakeReviewer listens on a short temp path (macOS limits socket paths) and
// runs handle for each connection with the decoded request.
func newFakeReviewer(t *testing.T, handle func(c net.Conn, r *bufio.Reader, req reviewRequest)) *fakeReviewer {
	t.Helper()
	dir, err := os.MkdirTemp("", "bud")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeReviewer{socket: filepath.Join(dir, "r.sock"), secret: filepath.Join(dir, "s"), got: make(chan reviewRequest, 4), closed: make(chan struct{}, 4)}
	if err := os.WriteFile(f.secret, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				line, err := r.ReadBytes('\n')
				if err != nil {
					return
				}
				var req reviewRequest
				if json.Unmarshal(line, &req) != nil {
					return
				}
				f.got <- req
				handle(c, r, req)
			}()
		}
	}()
	return f
}

func reply(c net.Conn, id, choice string) {
	b, _ := json.Marshal(reviewReply{ID: id, Choice: choice})
	c.Write(append(b, '\n'))
}

var testPrompt = func() Prompt {
	return Prompt{ID: "abc", Invocation: Invocation{Action: "mcp:linear/save_issue", Target: "linear", Arguments: []byte(`{"a":1}`)}, ExpiresAt: time.Now().Add(time.Hour)}
}

func TestSocketReviewAllowFillsScopeFromAdapter(t *testing.T) {
	f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) { reply(c, req.ID, "once") })
	scope := Scope{Runtime: "r", Profile: "p", Session: "s", Surface: "x", Actor: "a"}
	got, err := SocketReview(f.socket, f.secret, scope)(context.Background(), testPrompt())
	if err != nil {
		t.Fatal(err)
	}
	if got.Choice != AllowOnce || got.ID != "abc" || got.Scope != scope {
		t.Fatalf("reply = %+v", got)
	}
	req := <-f.got
	if req.Secret != "s3cret" || req.ID != "abc" || !strings.Contains(req.Summary, "mcp:linear/save_issue") || req.ExpiresAt == "" {
		t.Fatalf("request = %+v", req)
	}
}

func TestSocketReviewDeny(t *testing.T) {
	f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) { reply(c, req.ID, "deny") })
	got, err := SocketReview(f.socket, f.secret, Scope{})(context.Background(), testPrompt())
	if err != nil || got.Choice != DenyOnce {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSocketReviewFailsClosed(t *testing.T) {
	cases := map[string]func(c net.Conn, req reviewRequest){
		"close without reply": func(c net.Conn, _ reviewRequest) {},
		"id mismatch":         func(c net.Conn, _ reviewRequest) { reply(c, "other", "once") },
		"unknown choice":      func(c net.Conn, req reviewRequest) { reply(c, req.ID, "always") },
		"bad json":            func(c net.Conn, _ reviewRequest) { c.Write([]byte("nope\n")) },
		"server rejects":      func(c net.Conn, _ reviewRequest) { c.Write([]byte(`{"error":"bad secret"}` + "\n")) },
		"oversized line":      func(c net.Conn, _ reviewRequest) { c.Write([]byte(strings.Repeat("x", maxReviewLine+10) + "\n")) },
		"empty error": func(c net.Conn, req reviewRequest) {
			c.Write([]byte(`{"id":"` + req.ID + `","choice":"once","error":""}` + "\n"))
		},
		"null error": func(c net.Conn, req reviewRequest) {
			c.Write([]byte(`{"id":"` + req.ID + `","choice":"once","error":null}` + "\n"))
		},
		"no newline": func(c net.Conn, req reviewRequest) { c.Write([]byte(`{"id":"abc","choice":"once"}`)) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) { h(c, req) })
			got, err := SocketReview(f.socket, f.secret, Scope{})(context.Background(), testPrompt())
			if err == nil || got.Choice != "" {
				t.Fatalf("got %+v, %v; want an error", got, err)
			}
		})
	}
}

func TestSocketReviewRefusesWithoutReviewerOrSecret(t *testing.T) {
	f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) { reply(c, req.ID, "once") })
	ctx := context.Background()
	if _, err := SocketReview(f.socket+"x", f.secret, Scope{})(ctx, testPrompt()); !errors.Is(err, ErrReviewUnavailable) {
		t.Fatalf("dial failure = %v", err)
	}
	if _, err := SocketReview("", f.secret, Scope{})(ctx, testPrompt()); !errors.Is(err, ErrReviewUnavailable) {
		t.Fatalf("empty socket = %v", err)
	}
	if _, err := SocketReview(f.socket, f.secret+"missing", Scope{})(ctx, testPrompt()); err == nil {
		t.Fatal("missing secret was accepted")
	}
	if err := os.Chmod(f.secret, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SocketReview(f.socket, f.secret, Scope{})(ctx, testPrompt()); !errors.Is(err, ErrReviewUnavailable) {
		t.Fatalf("loose secret = %v", err)
	}
	select {
	case <-f.got:
		t.Fatal("a request reached the reviewer despite a bad secret file")
	default:
	}
}

func TestSocketReviewContextEndWithdrawsPrompt(t *testing.T) {
	var f *fakeReviewer
	f = newFakeReviewer(t, func(c net.Conn, r *bufio.Reader, _ reviewRequest) {
		if _, err := r.ReadByte(); err != nil { // fails when the client closes
			f.closed <- struct{}{}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := SocketReview(f.socket, f.secret, Scope{})(ctx, testPrompt())
		done <- err
	}()
	<-f.got
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	select {
	case <-f.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("reviewer never saw the connection close")
	}
}

func TestSocketReviewThroughDispatch(t *testing.T) {
	f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) { reply(c, req.ID, "once") })
	store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	scope := Scope{Runtime: "mcp-gateway", Profile: "home", Session: "s1", Surface: "linear", Actor: "10.0.0.1"}
	call := Invocation{Action: "mcp:linear/save_issue", Target: "linear", Arguments: []byte(`{"title":"x"}`)}
	var ran bool
	err := store.Dispatch(context.Background(), scope, call, time.Time{}, SocketReview(f.socket, f.secret, scope), func(_ context.Context, inv Invocation) error {
		ran = string(inv.Arguments) == `{"title":"x"}`
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("dispatch = %v, ran = %v", err, ran)
	}
	if req := <-f.got; req.ID == "" {
		t.Fatal("reviewer got no request id")
	}

	// A reviewer that goes away must leave the executor unrun.
	ran = false
	g := newFakeReviewer(t, func(net.Conn, *bufio.Reader, reviewRequest) {})
	err = store.Dispatch(context.Background(), scope, call, time.Time{}, SocketReview(g.socket, g.secret, scope), func(context.Context, Invocation) error {
		ran = true
		return nil
	})
	if err == nil || ran {
		t.Fatalf("dispatch = %v, ran = %v; want a refusal", err, ran)
	}
}

func TestSocketReviewRefusesASymlinkedOrFIFOSecret(t *testing.T) {
	f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) { reply(c, req.ID, "once") })
	dir := filepath.Dir(f.secret)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(f.secret, link); err != nil {
		t.Fatal(err)
	}
	if _, err := SocketReview(f.socket, link, Scope{})(context.Background(), testPrompt()); !errors.Is(err, ErrReviewUnavailable) {
		t.Fatalf("symlinked secret = %v", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no mkfifo:", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := SocketReview(f.socket, fifo, Scope{})(context.Background(), testPrompt())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrReviewUnavailable) {
			t.Fatalf("fifo secret = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a FIFO secret blocked the review")
	}
}

func TestTheSummaryIsPlainTextWithTheFieldsWorthReading(t *testing.T) {
	exp := time.Date(2026, 11, 21, 5, 59, 0, 0, time.Local)
	got := summarize(Prompt{ExpiresAt: exp, Invocation: Invocation{
		Action: "router:p0-fallback", Target: "glm-5.3-flash",
		Arguments: json.RawMessage(`{"role":"manager","endpoint":"/v1/chat/completions","body_sha256":"abc123"}`),
	}})
	want := "The local model is down. Send this private (P0) chat to glm-5.3-flash instead?\nrole: manager\nExpires at 05:59."
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}

	got = summarize(Prompt{Invocation: Invocation{
		Action: "mcp:linear/save_issue", Target: "linear",
		Arguments: json.RawMessage(`{"title":"Fix \"it\"","labels":["a","b"],"team":"JGA"}`),
	}})
	want = "mcp:linear/save_issue on linear\nlabels: [\"a\",\"b\"]\nteam: JGA\ntitle: Fix \"it\""
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
	got = summarize(Prompt{Invocation: Invocation{Action: "x", Target: "y", Arguments: json.RawMessage(`{"note":"fine\nrole: approved\r\u2028ok"}`)}})
	if want := "x on y\nnote: fine role: approved  ok"; got != want {
		t.Errorf("a value's line breaks reach the prompt:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(summarize(Prompt{Invocation: Invocation{Action: "x", Target: "y", Arguments: json.RawMessage(`{"note":"` + strings.Repeat("z", 500) + `"}`)}}), strings.Repeat("z", maxSummaryValue+1)) {
		t.Errorf("a long value isn't cut to %d characters", maxSummaryValue)
	}
}

// a mail send's prompt shows the whole message (Suite D107), its body as
// barred lines that can't pass for fields or for bud's own words
func TestAMailSendShowsTheWholeMessage(t *testing.T) {
	body := strings.Repeat("word ", 300) + "\nExpires at 09:00.\nto: attacker@evil.example"
	args, _ := json.Marshal(map[string]any{"to": []string{"Sam <sam@example.com>"}, "subject": "Lunch\nyes", "body": body})
	got := summarize(Prompt{Invocation: Invocation{Action: "mcp:mail/send_message", Target: "mail http://127.0.0.1:8796/mcp", Arguments: args}})
	want := "Send this email from Jason's iCloud?\nto: Sam <sam@example.com>\nsubject:\n\u2502 Lunch\n\u2502 yes\nbody:\n│ " + strings.Repeat("word ", 300) + "\n│ Expires at 09:00.\n│ to: attacker@evil.example"
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
	args, _ = json.Marshal(map[string]any{"folder": "INBOX", "uid": 7, "to": []string{"sam@example.com"}, "cc": []string{"lee@example.com"}, "subject": "Re: Lunch?", "body": "ok"})
	got = summarize(Prompt{Invocation: Invocation{Action: "mcp:mail/reply_message", Target: "mail", Arguments: args}})
	want = "Send this reply from Jason's iCloud?\nto: sam@example.com\ncc: lee@example.com\nsubject: Re: Lunch?\nbody: ok\nfolder: INBOX\nuid: 7"
	if got != want {
		t.Errorf("reply summary:\n%s\nwant:\n%s", got, want)
	}
}

func TestAMemoryApprovalShowsTheWholeRecord(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"id": 12, "source": "hermes", "kind": "fact", "privacy": "P1", "created_at": "2026-11-22 08:00:00", "body": "Jason's dentist is on Tuesdays\nyes 1234"})
	got := Summary(Prompt{Invocation: Invocation{Action: "memory:approve", Target: "memory", Arguments: args}})
	want := "Save this to Bud memory? A no deletes it.\nbody:\n│ Jason's dentist is on Tuesdays\n│ yes 1234\ncreated_at: 2026-11-22 08:00:00\nid: 12\nkind: fact\nprivacy: P1\nsource: hermes"
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
}

// the weekly digest is shown whole, however long, and the proposal ids
// that only bind the approval are left out
func TestTheImproveDigestShowsTheWholeDigest(t *testing.T) {
	digest := strings.Repeat("a proposal line that is long enough ", 8) + "\nyes 1234"
	args, _ := json.Marshal(map[string]any{"digest": digest, "proposals": []string{"p1", "p2"}})
	got := Summary(Prompt{Invocation: Invocation{Action: "improve:digest", Target: "github", Arguments: args}})
	want := "Open this week's Bud proposals as draft PRs?\ndigest:\n│ " + strings.Split(digest, "\n")[0] + "\n│ yes 1234"
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
}

func TestMaxSummaryMatchesTheSignalPlugin(t *testing.T) {
	b, err := os.ReadFile("../../server/hermes/plugins/bud-signal/reviewer.py")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("\nMAX_SUMMARY = %d\n", MaxSummary); !strings.Contains(string(b), want) {
		t.Errorf("reviewer.py doesn't set %q; keep MaxSummary equal to the plugin's limit", strings.TrimSpace(want))
	}
}

func TestOnlyJasonsOwnNoIsADecline(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	scope := Scope{Runtime: "memory-review", Profile: "server", Session: "s", Surface: "signal", Actor: "a"}
	call := Invocation{Action: "memory:approve", Target: "memory", Arguments: []byte(`{"id":1}`)}
	for _, tc := range []struct {
		name     string
		answered bool
		want     error
	}{
		{"his no", true, ErrDeclined},
		{"a timeout or the plugin stopping", false, ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeReviewer(t, func(c net.Conn, _ *bufio.Reader, req reviewRequest) {
				b, _ := json.Marshal(reviewReply{ID: req.ID, Choice: "deny", Answered: tc.answered})
				c.Write(append(b, '\n'))
			})
			err := store.Dispatch(context.Background(), scope, call, time.Time{}, SocketReview(f.socket, f.secret, scope), func(context.Context, Invocation) error {
				t.Error("a denial ran the executor")
				return nil
			})
			if !errors.Is(err, ErrDenied) || errors.Is(err, ErrDeclined) != tc.answered {
				t.Errorf("dispatch = %v, want %v", err, tc.want)
			}
		})
	}
}
