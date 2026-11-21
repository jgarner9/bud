package approval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
