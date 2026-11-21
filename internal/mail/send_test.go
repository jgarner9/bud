package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	netmail "net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeSMTP is a loopback SMTP server without STARTTLS that records one
// session. Recipients in refuse get 550
type fakeSMTP struct {
	addr   string
	auth   string
	from   string
	rcpt   []string
	data   string
	refuse map[string]bool
	done   chan struct{}
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{addr: ln.Addr().String(), refuse: map[string]bool{}, done: make(chan struct{})}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		defer close(f.done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
			switch {
			case cmd == "EHLO":
				say("250-fake")
				say("250 AUTH PLAIN")
			case strings.HasPrefix(strings.ToUpper(line), "AUTH PLAIN"):
				f.auth = strings.TrimSpace(line[len("AUTH PLAIN"):])
				say("235 ok")
			case strings.HasPrefix(strings.ToUpper(line), "MAIL FROM:"):
				f.from = line[len("MAIL FROM:"):]
				say("250 ok")
			case strings.HasPrefix(strings.ToUpper(line), "RCPT TO:"):
				to := strings.Trim(line[len("RCPT TO:"):], "<>")
				if f.refuse[to] {
					say("550 no such user")
					continue
				}
				f.rcpt = append(f.rcpt, to)
				say("250 ok")
			case cmd == "DATA":
				say("354 go ahead")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				f.data = b.String()
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("502 unknown")
			}
		}
	}()
	return f
}

func (f *fakeSMTP) wait(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake SMTP server didn't finish")
	}
}

func addrs(t *testing.T, list ...string) []*netmail.Address {
	t.Helper()
	out, err := ParseRecipients("to", list)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func newTestSender(t *testing.T, addr string) *Sender {
	t.Helper()
	s, err := NewSender(addr, "someone@example.com", "app-pass", "Jason <someone@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC) }
	return s
}

func TestASendIsPlainTextFromTheAccountWithEveryRecipient(t *testing.T) {
	f := newFakeSMTP(t)
	s := newTestSender(t, f.addr)
	id, err := s.Send(context.Background(), Outgoing{
		To: addrs(t, "Sam Lee <sam@example.com>"), Cc: addrs(t, "lee@example.com"),
		Subject: "Café at noon", Body: "See you there.\nBring the map.",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(t)
	if auth, _ := base64.StdEncoding.DecodeString(f.auth); string(auth) != "\x00someone@example.com\x00app-pass" {
		t.Errorf("auth %q", auth)
	}
	if f.from != "<someone@example.com>" || !reflect.DeepEqual(f.rcpt, []string{"sam@example.com", "lee@example.com"}) {
		t.Errorf("from %q, rcpt %q", f.from, f.rcpt)
	}
	for _, want := range []string{
		"From: \"Jason\" <someone@example.com>\r\n",
		"To: \"Sam Lee\" <sam@example.com>\r\n",
		"Cc: <lee@example.com>\r\n",
		"Subject: =?utf-8?q?Caf=C3=A9_at_noon?=\r\n",
		"Date: Sat, 21 Nov 2026 09:00:00 +0000\r\n",
		"Message-ID: " + id + "\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\n\r\nSee you there.\r\nBring the map.\r\n",
	} {
		if !strings.Contains(f.data, want) {
			t.Errorf("the message lacks %q:\n%s", want, f.data)
		}
	}
	if !strings.HasSuffix(id, "@example.com>") {
		t.Errorf("message id %q", id)
	}
}

func TestARefusedRecipientSendsNothing(t *testing.T) {
	f := newFakeSMTP(t)
	f.refuse["nobody@example.com"] = true
	s := newTestSender(t, f.addr)
	_, err := s.Send(context.Background(), Outgoing{To: addrs(t, "sam@example.com", "nobody@example.com"), Subject: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "nobody@example.com") {
		t.Fatalf("got %v, want the refused recipient named", err)
	}
	f.wait(t)
	if f.data != "" {
		t.Errorf("a message was sent: %q", f.data)
	}
}

func TestASendWithoutSTARTTLSIsRefusedOffLoopback(t *testing.T) {
	f := newFakeSMTP(t)
	s := newTestSender(t, f.addr)
	s.tls = true
	_, err := s.Send(context.Background(), Outgoing{To: addrs(t, "sam@example.com"), Subject: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("got %v, want a refusal naming STARTTLS", err)
	}
	if f.auth != "" {
		t.Error("the password was sent without TLS")
	}
}

func TestAMessageThatCantBeShownWholeOrAddsHeadersIsRefused(t *testing.T) {
	to := addrs(t, "sam@example.com")
	for name, m := range map[string]Outgoing{
		"no recipient":    {Subject: "x", Body: "y"},
		"long body":       {To: to, Subject: "x", Body: strings.Repeat("é", MaxBody+1)},
		"subject CRLF":    {To: to, Subject: "hi\r\nBcc: evil@example.com", Body: "y"},
		"empty body":      {To: to, Subject: "x", Body: " \n"},
		"many recipients": {To: addrs(t, strings.Split(strings.Repeat("a@example.com,", MaxRecipients+1), ",")[:MaxRecipients+1]...), Subject: "x", Body: "y"},
		"reply CRLF":      {To: to, Subject: "x", Body: "y", InReplyTo: "<a@b>\r\nBcc: evil@example.com"},
	} {
		if err := m.Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := (Outgoing{To: to, Subject: "x", Body: strings.Repeat("é", MaxBody)}).Check(); err != nil {
		t.Errorf("a body of exactly %d characters: %v", MaxBody, err)
	}
	if _, err := ParseRecipients("to", []string{"sam@example.com\r\nBcc: evil@example.com"}); err == nil {
		t.Error("a recipient with CRLF parsed")
	}
}

func newTestAccount(t *testing.T, imap string) *Account {
	t.Helper()
	a, err := NewAccount(imap, "someone@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	if a.Log, err = NewLog(filepath.Join(t.TempDir(), "state", "mail-actions.jsonl")); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestMoveToTrashLogsEachMessageAndRestoreUndoesIt(t *testing.T) {
	ctx := context.Background()
	a := newTestAccount(t, replay(t, "trash"))
	moved, trash, err := a.MoveToTrash(ctx, "INBOX", []uint32{7, 8})
	if err != nil {
		t.Fatal(err)
	}
	want := []Moved{{UID: 7, MessageID: "<sale@example.net>", From: []string{"Store <deals@example.net>"}, Subject: "Sale"}}
	if trash != "Deleted Messages" || !reflect.DeepEqual(moved, want) {
		t.Fatalf("moved %+v to %q", moved, trash)
	}
	info, err := os.Stat(a.Log.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log %v, %v", info, err)
	}
	acts, _ := a.Digest(time.Now().Add(-time.Hour))
	if len(acts) != 1 || acts[0].Action != "trash" || acts[0].Folder != "INBOX" || acts[0].MessageID != "<sale@example.net>" {
		t.Fatalf("digest %+v", acts)
	}

	b := newTestAccount(t, replay(t, "restore"))
	b.Log = a.Log
	r, err := b.Restore(ctx, "<sale@example.net>")
	if err != nil {
		t.Fatal(err)
	}
	if r.ToFolder != "INBOX" || r.Folder != "Deleted Messages" {
		t.Errorf("restored %+v", r)
	}
	if _, err := b.Restore(ctx, "<sale@example.net>"); err == nil || !strings.Contains(err.Error(), "only undoes bud's own moves") {
		t.Errorf("a second restore: %v", err)
	}
}

func TestMoveToTrashChecksItsArguments(t *testing.T) {
	a := newTestAccount(t, "127.0.0.1:1")
	if _, _, err := a.MoveToTrash(context.Background(), "INBOX", nil); err == nil {
		t.Error("no uids was accepted")
	}
	if _, _, err := a.MoveToTrash(context.Background(), "INBOX", make([]uint32, MaxMove+1)); err == nil {
		t.Error("too many uids was accepted")
	}
	a.Log = nil
	if _, _, err := a.MoveToTrash(context.Background(), "INBOX", []uint32{1}); err != ErrNoLog {
		t.Errorf("no log: %v", err)
	}
}

func TestAMoveIsLoggedWhenTheLogoutFails(t *testing.T) {
	a := newTestAccount(t, replay(t, "trash_dropped"))
	if _, _, err := a.MoveToTrash(context.Background(), "INBOX", []uint32{7, 8}); err != nil {
		t.Fatal(err)
	}
	if acts, _ := a.Log.Read(); len(acts) != 1 || acts[0].MessageID != "<sale@example.net>" {
		t.Fatalf("log %+v", acts)
	}
}

func TestNewLogMakesAnOldLogPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail-actions.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLog(path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log %v, %v", info, err)
	}
}

func TestAReplyGoesOnlyToPeopleOnTheOriginalAndThreads(t *testing.T) {
	ctx := context.Background()
	f := newFakeSMTP(t)
	a := newTestAccount(t, replay(t, "reply"))
	a.Sender = newTestSender(t, f.addr)
	id, err := a.Reply(ctx, "INBOX", 7, addrs(t, "sam@example.com", "pat@example.com"), addrs(t, "lee@example.com"), "Re: Lunch?", "Noon works.")
	if err != nil {
		t.Fatal(err)
	}
	f.wait(t)
	for _, want := range []string{
		"Subject: Re: Lunch?\r\n",
		"In-Reply-To: <m7@example.com>\r\n",
		"References: <m1@example.com> <m5@example.com> <m7@example.com>\r\n",
	} {
		if !strings.Contains(f.data, want) {
			t.Errorf("the reply lacks %q:\n%s", want, f.data)
		}
	}
	acts, _ := a.Log.Read()
	if len(acts) != 1 || acts[0].Action != "send" || acts[0].MessageID != id || acts[0].Subject != "Re: Lunch?" {
		t.Errorf("log %+v", acts)
	}
	if strings.Contains(mustRead(t, a.Log.path), "Noon works") {
		t.Error("the log holds the body")
	}
}

func TestAReplyToSomeoneNotOnTheOriginalIsRefused(t *testing.T) {
	ctx := context.Background()
	a := newTestAccount(t, replay(t, "reply"))
	a.Sender = newTestSender(t, "127.0.0.1:1")
	_, err := a.Reply(ctx, "INBOX", 7, addrs(t, "sam@example.com", "attacker@evil.example"), nil, "Re: Lunch?", "hi")
	if err == nil || !strings.Contains(err.Error(), "attacker@evil.example isn't on the message") {
		t.Fatalf("got %v", err)
	}
	b := newTestAccount(t, replay(t, "reply"))
	b.Sender = a.Sender
	if _, err := b.Reply(ctx, "INBOX", 7, addrs(t, "pat@example.com"), nil, "Re: Lunch?", "hi"); err == nil || !strings.Contains(err.Error(), "who sent the message") {
		t.Fatalf("a reply that leaves out the sender: %v", err)
	}
	c := newTestAccount(t, replay(t, "reply"))
	c.Sender = a.Sender
	if _, err := c.Reply(ctx, "INBOX", 7, addrs(t, "sam@example.com"), nil, "Lunch?", "hi"); err == nil || !strings.Contains(err.Error(), `"Re: Lunch?"`) {
		t.Fatalf("a reply under another subject: %v", err)
	}
}

func TestReplySubject(t *testing.T) {
	for in, want := range map[string]string{
		"Lunch?":               "Re: Lunch?",
		"RE: Lunch?":           "RE: Lunch?",
		"  Lunch\r\n  at noon": "Re: Lunch at noon",
		"":                     "Re: ",
	} {
		if got := ReplySubject(in); got != want {
			t.Errorf("ReplySubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheSendToolsTakeListsAndOnlyTheirOwnArguments(t *testing.T) {
	f := &fakeAcct{}
	s := NewServer(f)
	r := callTool(t, s, "send_message", `{"to":["Sam <sam@example.com>"],"subject":"Hi","body":"Hello"}`)
	if e := errorText(r); e != "" {
		t.Fatal(e)
	}
	if f.sent == nil || f.sent.To[0].Address != "sam@example.com" || f.sent.Subject != "Hi" || f.sent.Body != "Hello" {
		t.Errorf("sent %+v", f.sent)
	}
	for args, want := range map[string]string{
		`{"to":"sam@example.com","subject":"Hi","body":"x"}`:           "list",
		`{"to":["not an address"],"subject":"Hi","body":"x"}`:          "isn't an address",
		`{"to":["sam@example.com"],"subject":"Hi","body":"x","uid":3}`: `no "uid"`,
	} {
		if e := errorText(callTool(t, s, "send_message", args)); !strings.Contains(e, want) {
			t.Errorf("%s: %q, want %q", args, e, want)
		}
	}
	if e := errorText(callTool(t, s, "reply_message", `{"uid":7,"to":["sam@example.com"],"body":"ok"}`)); !strings.Contains(e, "subject") {
		t.Errorf("a reply without a subject: %q", e)
	}
	if e := errorText(callTool(t, s, "move_to_trash", `{"uids":[7],"to":["sam@example.com"]}`)); !strings.Contains(e, `no "to"`) {
		t.Errorf("a list of recipients on move_to_trash: %q", e)
	}
	f.sent = nil
	r = callTool(t, s, "reply_message", `{"uid":7,"to":["sam@example.com"],"subject":"Re: Hi","body":"ok"}`)
	if e := errorText(r); e != "" || f.gotUID != 7 || f.gotFolder != "INBOX" || f.sent == nil || f.sent.Subject != "Re: Hi" {
		t.Errorf("reply: %q, uid %d, %+v", e, f.gotUID, f.sent)
	}
	if e := errorText(callTool(t, s, "reply_message", `{"uid":7,"to":["sam@example.com"],"subject":"Re: Hi","body":"`+strings.Repeat("x", MaxBody+1)+`"}`)); !strings.Contains(e, "4000") {
		t.Errorf("a long reply: %q", e)
	}
}

func TestCleanupToolsPassTheirArguments(t *testing.T) {
	f := &fakeAcct{moved: []Moved{{UID: 7, MessageID: "<a@b>"}}, acts: []Action{{Action: "trash"}, {Action: "restore"}, {Action: "trash"}}}
	s := NewServer(f)
	s.now = func() time.Time { return time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC) }
	if e := errorText(callTool(t, s, "move_to_trash", `{"folder":"Promotions","uids":[7,9]}`)); e != "" || f.gotFolder != "Promotions" || !reflect.DeepEqual(f.gotUIDs, []uint32{7, 9}) {
		t.Errorf("move: %q %q %v", e, f.gotFolder, f.gotUIDs)
	}
	if e := errorText(callTool(t, s, "restore_message", `{"message_id":" <a@b> "}`)); e != "" || f.gotID != "<a@b>" {
		t.Errorf("restore: %q %q", e, f.gotID)
	}
	r := callTool(t, s, "cleanup_digest", `{}`)
	sc := r["structuredContent"].(map[string]any)
	if !f.gotSince.Equal(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)) || sc["moved_to_trash"] != float64(2) || sc["restored"] != float64(1) {
		t.Errorf("digest since %v: %v", f.gotSince, sc)
	}
	callTool(t, s, "cleanup_digest", `{"since":"2026-11-01"}`)
	if !f.gotSince.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since a date: %v", f.gotSince)
	}
}

func TestARecordingRedactsTheLoginLiteralsAndAddresses(t *testing.T) {
	a, err := NewAccount(replay(t, "record"), "someone@example.com", "app-pass")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	f, err := a.Record(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Move || !f.UIDPlus || f.Trash != "Deleted Messages" || len(f.Folders) != 2 {
		t.Errorf("findings %+v", f)
	}
	got := out.String()
	for _, secret := range []string{"app-pass", "private", "secret body"} {
		if strings.Contains(got, secret) {
			t.Errorf("the recording holds %q:\n%s", secret, got)
		}
	}
	for _, want := range []string{
		"C: a1 LOGIN <redacted>\n",
		"S: * CAPABILITY IMAP4rev1 MOVE UIDPLUS\n",
		"REPLY-TO)] {#}\nL: [redacted]\nS: )\n",
		"C: a9 LOGOUT\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the recording lacks %q:\n%s", want, got)
		}
	}
}
