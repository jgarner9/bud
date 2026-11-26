package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// The IMAP tests run against go-imap's own in-process server (imapserver
// with imapmemserver behind it), so both ends of the wire are the library's
// and the tests pin bud's adapter, not a hand-written transcript (Suite
// D138). A few cases the library's server can't produce (a server that
// disables LOGIN, a PREAUTH greeting, one that never answers) are scripted
// by hand instead

const (
	testUser = "someone@example.com"
	testPass = "app-pass"
)

// capsFull is a server with MOVE and UIDPLUS, as iCloud is expected to be.
// Never IMAP4rev2, which implies both and would blur the matrix
var capsFull = []imap.Cap{imap.CapIMAP4rev1, imap.CapMove, imap.CapUIDPlus}

type quietLog struct{}

func (quietLog) Printf(string, ...any) {}

// folderList is what the fixture server's LIST answers, since the library's
// in-memory server can neither flag a folder \Trash nor make one \Noselect
var folderList = []imap.ListData{
	{Mailbox: "INBOX", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrHasNoChildren}},
	{Mailbox: "Archive", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrHasChildren}},
	{Mailbox: "Archive/Café", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrHasNoChildren}},
	{Mailbox: "Deleted Messages", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrHasNoChildren, imap.MailboxAttrTrash}},
	{Mailbox: "Notes", Delim: '/', Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}},
}

// listSession is the in-memory server's session with its LIST replaced by
// folderList and the optional MOVE command passed through
type listSession struct {
	imapserver.Session
}

func (s listSession) List(w *imapserver.ListWriter, ref string, patterns []string, _ *imap.ListOptions) error {
	for _, d := range folderList {
		for _, p := range patterns {
			if imapserver.MatchList(d.Mailbox, '/', ref, p) {
				d := d
				if err := w.WriteList(&d); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func (s listSession) Move(w *imapserver.MoveWriter, set imap.NumSet, dest string) error {
	return s.Session.(imapserver.SessionMove).Move(w, set, dest)
}

type memServer struct {
	addr string
	user *imapmemserver.User
}

// newMemServer starts a server on loopback with the account's folders and no
// mail. caps picks what it advertises
func newMemServer(t *testing.T, caps ...imap.Cap) *memServer {
	t.Helper()
	if len(caps) == 0 {
		caps = capsFull
	}
	mem := imapmemserver.New()
	user := imapmemserver.NewUser(testUser, testPass)
	for _, name := range []string{"INBOX", "Deleted Messages", "Archive", "Archive/Café"} {
		if err := user.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(user)
	set := imap.CapSet{}
	for _, c := range caps {
		set[c] = struct{}{}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return listSession{mem.NewSession()}, nil, nil
		},
		Caps:         set,
		InsecureAuth: true,
		Logger:       quietLog{},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return &memServer{addr: ln.Addr().String(), user: user}
}

// msg builds an RFC 5322 message from header lines and a body
func msg(body string, header ...string) string {
	return strings.Join(header, "\r\n") + "\r\n\r\n" + body
}

var received = time.Date(2026, 11, 3, 15, 15, 0, 0, time.UTC)

// add puts a message in a folder, returning its UID
func (m *memServer) add(t *testing.T, folder, raw string, flags ...imap.Flag) uint32 {
	t.Helper()
	d, err := m.user.Append(folder, strings.NewReader(raw), &imap.AppendOptions{Flags: flags, Time: received})
	if err != nil {
		t.Fatal(err)
	}
	return uint32(d.UID)
}

// count is how many messages a folder holds, asked over a fresh connection
func (m *memServer) count(t *testing.T, folder string) uint32 {
	t.Helper()
	c := dialMem(t, m)
	st, err := c.Status(context.Background(), folder)
	if err != nil {
		t.Fatal(err)
	}
	return st.Messages
}

func dialMem(t *testing.T, m *memServer) *Client {
	t.Helper()
	ctx := context.Background()
	c, err := Dial(ctx, m.addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Login(ctx, testUser, testPass); err != nil {
		t.Fatal(err)
	}
	return c
}

// fixtureInbox seeds the three messages most tests read
func fixtureInbox(t *testing.T, m *memServer) {
	t.Helper()
	m.add(t, "INBOX", msg("See you at the café.\r\n",
		"From: Renée <renee@example.com>", "To: someone@example.com", "Subject: Café plans",
		"Message-ID: <m1@example.com>", "Date: Tue, 03 Nov 2026 09:15:00 -0600", "X-Pad: "+strings.Repeat("x", 200)), imap.FlagSeen)
	m.add(t, "INBOX", msg("Unrelated.\r\n", "From: Pat <pat@example.com>", "Subject: Lunch", "Message-ID: <m2@example.com>"), imap.FlagSeen)
	m.add(t, "INBOX", msg("Hello there\r\n", `From: "Sam \"S\" Lee" <sam@example.com>`, "Subject: Café again", "Message-ID: <m3@example.com>"))
}

func TestListPutsInboxFirstAndDecodesNames(t *testing.T) {
	ctx := context.Background()
	m := newMemServer(t)
	fixtureInbox(t, m)
	c := dialMem(t, m)
	folders, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range folders {
		names = append(names, f.Name)
	}
	want := []string{"INBOX", "Archive", "Archive/Café", "Deleted Messages", "Notes"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("folders %q, want %q", names, want)
	}
	if !folders[3].HasFlag(`\trash`) || folders[4].Selectable() || !folders[0].Selectable() || folders[0].Delim != "/" {
		t.Errorf("flags read wrong: %+v", folders)
	}
	st, err := c.Status(ctx, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if st.Messages != 3 || st.Unseen != 1 || st.UIDNext != 4 || st.UIDValidity == 0 {
		t.Errorf("status %+v", st)
	}
	if st, err := c.Status(ctx, "Archive/Café"); err != nil || st.Messages != 0 {
		t.Errorf("a non-ASCII folder's status %+v, %v", st, err)
	}
	for _, name := range []string{"move", "UIDPLUS"} {
		if ok, err := c.Has(ctx, name); err != nil || !ok {
			t.Errorf("Has(%q) = %v, %v", name, ok, err)
		}
	}
	if ok, _ := c.Has(ctx, "NO-SUCH-THING"); ok {
		t.Error("Has said yes to a capability the server lacks")
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSearchAndFetchReadWithoutMarkingSeen(t *testing.T) {
	ctx := context.Background()
	m := newMemServer(t)
	fixtureInbox(t, m)
	c := dialMem(t, m)
	mb, err := c.Examine(ctx, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if mb.Exists != 3 || mb.UIDNext != 4 || mb.UIDValidity == 0 {
		t.Errorf("mailbox %+v", mb)
	}
	if sel := c.c.Mailbox(); sel == nil || !sel.ReadOnly {
		t.Fatalf("EXAMINE left the folder writable: %+v", sel)
	}
	uids, err := c.Search(ctx, Criteria{Subject: "Café", Since: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(uids, []uint32{1, 3}) {
		t.Errorf("a UTF-8 subject matched uids %v, want 1 and 3", uids)
	}
	if uids, _ := c.Search(ctx, Criteria{Subject: "Café", Unseen: true}); !reflect.DeepEqual(uids, []uint32{3}) {
		t.Errorf("unseen matched %v, want 3", uids)
	}
	if uids, _ := c.Search(ctx, Criteria{From: "pat@example.com", Before: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}); len(uids) != 0 {
		t.Errorf("a message received on the 3rd matched BEFORE the 2nd: %v", uids)
	}
	if all, _ := c.Search(ctx, Criteria{}); !reflect.DeepEqual(all, []uint32{1, 2, 3}) {
		t.Errorf("no criteria matched %v, want everything", all)
	}
	if byID, _ := c.Search(ctx, Criteria{MessageID: "<m2@example.com>"}); !reflect.DeepEqual(byID, []uint32{2}) {
		t.Errorf("Message-ID matched %v, want 2", byID)
	}
	sums, err := c.FetchSummaries(ctx, []uint32{1, 3, 77})
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 {
		t.Fatalf("summaries %+v, want uids 1 and 3 (77 doesn't exist)", sums)
	}
	byUID := map[uint32]Summary{}
	for _, s := range sums {
		byUID[s.UID] = s
	}
	s := byUID[1]
	if !s.Seen() || s.Size == 0 || !s.Received.Equal(received) {
		t.Errorf("summary %+v", s)
	}
	if h := string(s.Header); !strings.Contains(h, "Message-ID: <m1@example.com>\r\n") || !strings.HasSuffix(h, "\r\n\r\n") || strings.Contains(h, "X-Pad") {
		t.Errorf("header %q", s.Header)
	}
	if byUID[3].Seen() || !strings.Contains(string(byUID[3].Header), `"Sam \"S\" Lee"`) {
		t.Errorf("second summary %+v", byUID[3])
	}
	b, err := c.FetchBody(ctx, 3, 40)
	if err != nil {
		t.Fatal(err)
	}
	whole, err := c.FetchBody(ctx, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Truncated || len(b.Raw) != 40 || b.Size != whole.Size || whole.Truncated || !strings.HasSuffix(string(whole.Raw), "\r\n\r\nHello there\r\n") {
		t.Errorf("partial %+v, whole %+v", b, whole)
	}
	if _, err := c.FetchBody(ctx, 8, 64); !errors.Is(err, ErrNoMessage) {
		t.Errorf("a missing uid gave %v, want ErrNoMessage", err)
	}
	// reading the unread message's body must not have marked it read
	after, err := c.FetchSummaries(ctx, []uint32{3})
	if err != nil || len(after) != 1 || after[0].Seen() {
		t.Errorf("after reading, summary %+v, %v: the message was marked read", after, err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestASummaryAsksForAtMostMaxHeaderBytes(t *testing.T) {
	ctx := context.Background()
	m := newMemServer(t)
	m.add(t, "INBOX", msg("x\r\n", "Subject: "+strings.Repeat("s", 3*maxHeader), "Message-ID: <big@example.com>"))
	c := dialMem(t, m)
	if _, err := c.Examine(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	sums, err := c.FetchSummaries(ctx, []uint32{1})
	if err != nil || len(sums) != 1 {
		t.Fatalf("summaries %+v, %v", sums, err)
	}
	if n := len(sums[0].Header); n == 0 || n > maxHeader {
		t.Errorf("the header is %d bytes, want 1 to %d", n, maxHeader)
	}
}

func TestOpeningAFolderThatIsNotThereNamesTheFix(t *testing.T) {
	c := dialMem(t, newMemServer(t))
	_, err := c.Examine(context.Background(), "Nope")
	if err == nil || !strings.Contains(err.Error(), `there's no folder "Nope": list_folders shows the names`) {
		t.Fatalf("got %v", err)
	}
	if _, err := c.Search(context.Background(), Criteria{}); err == nil {
		t.Error("a search after a failed open went ahead")
	}
}

func TestMoveUsesMoveWhenTheServerHasIt(t *testing.T) {
	ctx := context.Background()
	m := newMemServer(t, capsFull...)
	fixtureInbox(t, m)
	c := dialMem(t, m)
	if _, err := c.Select(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := c.Move(ctx, []uint32{3, 1, 3}, "Deleted Messages"); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.count(t, "INBOX"); got != 1 {
		t.Errorf("INBOX has %d messages after moving two, want 1", got)
	}
	if got := m.count(t, "Deleted Messages"); got != 2 {
		t.Errorf("the trash has %d messages, want the 2 moved", got)
	}
}

func TestMoveWithoutMoveExpungesOnlyTheMovedUIDs(t *testing.T) {
	ctx := context.Background()
	m := newMemServer(t, imap.CapIMAP4rev1, imap.CapUIDPlus)
	fixtureInbox(t, m)
	// someone else's pending deletion: a plain EXPUNGE would take it too
	m.add(t, "INBOX", msg("Marked.\r\n", "Subject: marked", "Message-ID: <del@example.com>"), imap.FlagDeleted)
	c := dialMem(t, m)
	if ok, _ := c.Has(ctx, "MOVE"); ok {
		t.Fatal("the fixture server has MOVE; the fallback isn't being tested")
	}
	if _, err := c.Select(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := c.Move(ctx, []uint32{1}, "Deleted Messages"); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.count(t, "INBOX"); got != 3 {
		t.Errorf("INBOX has %d messages, want 3: the moved one gone, the one already marked deleted still there", got)
	}
	if got := m.count(t, "Deleted Messages"); got != 1 {
		t.Errorf("the trash has %d messages, want 1", got)
	}
}

func TestMoveIsRefusedWithoutMoveOrUIDPlus(t *testing.T) {
	ctx := context.Background()
	m := newMemServer(t, imap.CapIMAP4rev1)
	fixtureInbox(t, m)
	c := dialMem(t, m)
	if _, err := c.Select(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	err := c.Move(ctx, []uint32{1}, "Deleted Messages")
	if err == nil || !strings.Contains(err.Error(), "neither MOVE nor UIDPLUS") {
		t.Fatalf("got %v, want a refusal naming MOVE and UIDPLUS", err)
	}
	if got := m.count(t, "INBOX"); got != 3 {
		t.Errorf("a refused move changed INBOX to %d messages", got)
	}
}

func TestMoveNeedsAWritableFolder(t *testing.T) {
	c := &Client{selected: "INBOX"}
	if err := c.Move(context.Background(), []uint32{1}, "Deleted Messages"); err == nil {
		t.Fatal("a move from an examined folder went ahead")
	}
	m := newMemServer(t)
	fixtureInbox(t, m)
	d := dialMem(t, m)
	if _, err := d.Examine(context.Background(), "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := d.Move(context.Background(), []uint32{1}, "Deleted Messages"); err == nil || !strings.Contains(err.Error(), "Select") {
		t.Fatalf("a move from an EXAMINEd folder: %v", err)
	}
}

func TestARefusedLoginSaysToCheckThePassword(t *testing.T) {
	m := newMemServer(t)
	c, err := Dial(context.Background(), m.addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	err = c.Login(context.Background(), testUser, "old-pass")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("got %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), "old-pass") {
		t.Errorf("the error shows the password: %v", err)
	}
}

// scripted serves one connection on loopback by running fn on it, and
// fails the test if fn reports an error
func scripted(t *testing.T, fn func(r *bufio.Reader, conn net.Conn) error) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the scripted server didn't finish")
		}
	})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if err := fn(bufio.NewReader(conn), conn); err != nil {
			t.Errorf("scripted server: %v", err)
		}
	}()
	return ln.Addr().String()
}

func readLine(r *bufio.Reader) (string, error) {
	l, err := r.ReadString('\n')
	return strings.TrimSuffix(l, "\r\n"), err
}

func TestLoginFallsBackToAuthenticatePlain(t *testing.T) {
	ctx := context.Background()
	addr := scripted(t, func(r *bufio.Reader, conn net.Conn) error {
		_, _ = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1 LOGINDISABLED AUTH=PLAIN SASL-IR] ready\r\n")
		l, err := readLine(r)
		if err != nil {
			return err
		}
		tag, rest, _ := strings.Cut(l, " ")
		mech, resp, _ := strings.Cut(rest, " ")
		if mech != "AUTHENTICATE PLAIN" && !strings.HasPrefix(rest, "AUTHENTICATE PLAIN") {
			return fmt.Errorf("the client sent %q, want AUTHENTICATE PLAIN", l)
		}
		resp = strings.TrimPrefix(strings.TrimPrefix(rest, "AUTHENTICATE PLAIN"), " ")
		got, err := base64.StdEncoding.DecodeString(resp)
		if err != nil || string(got) != "\x00"+testUser+"\x00"+testPass {
			return fmt.Errorf("the client's PLAIN response was %q (%v)", got, err)
		}
		_, _ = io.WriteString(conn, tag+" OK [CAPABILITY IMAP4rev1] logged in\r\n")
		l, err = readLine(r)
		if err != nil {
			return err
		}
		tag, rest, _ = strings.Cut(l, " ")
		if rest != "LOGOUT" {
			return fmt.Errorf("the client sent %q, want LOGOUT", l)
		}
		_, _ = io.WriteString(conn, "* BYE bye\r\n"+tag+" OK done\r\n")
		return nil
	})
	c, err := Dial(ctx, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Login(ctx, testUser, testPass); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLoginIsRefusedWhenLoginIsDisabledWithoutPlain(t *testing.T) {
	addr := scripted(t, func(r *bufio.Reader, conn net.Conn) error {
		_, _ = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1 LOGINDISABLED] ready\r\n")
		if l, err := readLine(r); err == nil {
			return fmt.Errorf("the client sent %q to a server it can't log in to", l)
		}
		return nil
	})
	c, err := Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Login(context.Background(), testUser, testPass)
	if err == nil || !strings.Contains(err.Error(), "doesn't offer AUTHENTICATE PLAIN") {
		t.Fatalf("got %v", err)
	}
	_ = c.Close()
}

// fakeConn is a net.Conn whose peer isn't loopback
type fakeConn struct{ net.Conn }

func (fakeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(17, 1, 2, 3), Port: 143}
}

func TestLoginRefusesAPlainConnectionOffLoopback(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	go func() { _, _ = server.Write([]byte("* OK [CAPABILITY IMAP4rev1] ready\r\n")) }()
	c, err := NewClient(context.Background(), fakeConn{client})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(context.Background(), testUser, testPass); err == nil || !strings.Contains(err.Error(), "plain connection") {
		t.Fatalf("got %v, want a refusal to log in in the clear", err)
	}
}

func TestSearchRefusesLineBreaks(t *testing.T) {
	c := &Client{selected: "INBOX"}
	for _, cr := range []Criteria{
		{Subject: "a\r\nb2 LOGOUT"}, {From: "a\nb"}, {To: "a\rb"}, {Text: "a\nb"}, {MessageID: "<a@b>\r\nx"}, {MessageID: "<café@b>"},
	} {
		if _, err := c.Search(context.Background(), cr); err == nil {
			t.Errorf("%+v was sent", cr)
		}
	}
	_, err := c.Search(context.Background(), Criteria{Subject: "a\r\nb"})
	if err == nil || !strings.Contains(err.Error(), "the subject search text has a line break; search for one line") {
		t.Errorf("got %v", err)
	}
}

func TestACanceledContextStopsAWaitingCommand(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	go func() {
		_, _ = server.Write([]byte("* OK [CAPABILITY IMAP4rev1] ready\r\n"))
		_, _ = bufio.NewReader(server).ReadString('\n') // never answers
	}()
	c, err := NewClient(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.List(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the command held the call for %v", time.Since(start))
	}
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if _, err := c.Status(cctx, "INBOX"); !errors.Is(err, context.Canceled) {
		t.Errorf("a canceled context gave %v", err)
	}
}

func TestAPreauthGreetingIsRefused(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	go func() { _, _ = server.Write([]byte("* PREAUTH [CAPABILITY IMAP4rev1] logged in as someone\r\n")) }()
	if _, err := NewClient(context.Background(), client); err == nil || !strings.Contains(err.Error(), "PREAUTH") {
		t.Fatalf("got %v, want PREAUTH refused", err)
	}
}

func TestAByeGreetingIsRefusedWithItsText(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	go func() { _, _ = server.Write([]byte("* BYE too many connections\r\n")) }()
	_, err := NewClient(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), "refused the connection") || !strings.Contains(err.Error(), "too many connections") {
		t.Fatalf("got %v", err)
	}
}

// dropOnLogout proxies to upstream and hangs up on the client's LOGOUT
// instead of passing it on or answering
func dropOnLogout(t *testing.T, upstream string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() { _ = ln.Close(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		up, err := net.Dial("tcp", upstream)
		if err != nil {
			return
		}
		defer func() { _ = up.Close() }()
		go func() { _, _ = io.Copy(conn, up) }()
		r := bufio.NewReader(conn)
		for {
			l, err := r.ReadString('\n')
			if err != nil || strings.HasSuffix(strings.TrimSpace(l), "LOGOUT") {
				return
			}
			_, _ = io.WriteString(up, l)
		}
	}()
	return ln.Addr().String()
}

func TestAccountFoldersSkipsUnselectableOnesAndNamesSpecialUse(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	for i := 0; i < 2; i++ {
		m.add(t, "Deleted Messages", msg("x\r\n", "Subject: old"), imap.FlagSeen)
	}
	a, err := NewAccount(m.addr, testUser, testPass)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := a.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []FolderInfo{
		{Name: "INBOX", Total: 3, Unseen: 1},
		{Name: "Archive"},
		{Name: "Archive/Café"},
		{Name: "Deleted Messages", Special: "trash", Total: 2},
	}
	if !reflect.DeepEqual(fs, want) {
		t.Errorf("folders %+v, want %+v (Notes is \\Noselect, so it's left out)", fs, want)
	}
}

func TestAccountSearchKeepsTheNewestMatchesNewestFirst(t *testing.T) {
	m := newMemServer(t)
	for i := 0; i < 3; i++ {
		m.add(t, "INBOX", msg("x\r\n", fmt.Sprintf("Subject: n%d", i), fmt.Sprintf("Message-ID: <n%d@example.com>", i)))
	}
	a, err := NewAccount(m.addr, testUser, testPass)
	if err != nil {
		t.Fatal(err)
	}
	sums, total, err := a.Search(context.Background(), "INBOX", Criteria{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(sums) != 2 || sums[0].UID != 3 || sums[1].UID != 2 {
		t.Errorf("total %d, summaries %+v", total, sums)
	}
}

func TestARecordingFromTheMemoryServerHasNoSecrets(t *testing.T) {
	m := newMemServer(t)
	m.add(t, "INBOX", msg("the private text\r\n", "From: Private Person <private@example.org>", "Subject: private subject", "Message-ID: <p@example.org>"))
	a, err := NewAccount(m.addr, testUser, testPass)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	f, err := a.Record(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Move || !f.UIDPlus || f.Trash != "Deleted Messages" || len(f.Folders) != 5 {
		t.Errorf("findings %+v", f)
	}
	if !sortedStrings(f.Capabilities) || len(f.Capabilities) == 0 {
		t.Errorf("capabilities %q aren't sorted", f.Capabilities)
	}
	got := out.String()
	for _, secret := range []string{testPass, "private subject", "private text", "private@example.org"} {
		if strings.Contains(got, secret) {
			t.Errorf("the recording holds %q:\n%s", secret, got)
		}
	}
	for _, want := range []string{" LOGIN <redacted>\n", "L: [redacted]\n", " LOGOUT\n", "[CAPABILITY IMAP4rev1"} {
		if !strings.Contains(got, want) {
			t.Errorf("the recording lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "EXAMINE") == false || strings.Contains(got, "BODY.PEEK") == false || strings.Contains(got, " SELECT ") {
		t.Errorf("the session wasn't read-only:\n%s", got)
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestAFailedRecordingKeepsItsTrace(t *testing.T) {
	m := newMemServer(t)
	a, err := NewAccount(m.addr, testUser, "old-pass")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := a.Record(context.Background(), &out); err == nil {
		t.Fatal("a refused login recorded")
	}
	if got := out.String(); !strings.Contains(got, " LOGIN <redacted>\n") || strings.Contains(got, "old-pass") {
		t.Errorf("the trace of a failed session was lost, or holds the password:\n%s", got)
	}
}
