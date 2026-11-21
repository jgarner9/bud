package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// replay serves the transcript testdata/<name>.imap on loopback: it sends
// each S: line, and fails the test on any line the client sends that
// isn't the next C: line, or on lines left over when the client hangs up.
// An X: line drops the connection there
func replay(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".imap"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
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
			t.Errorf("%s: the fake server didn't finish", name)
		}
	})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("%s: accept: %v", name, err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		for i := 0; i < len(lines); i++ {
			kind, text, _ := strings.Cut(lines[i], ":")
			text = strings.TrimPrefix(text, " ")
			switch kind {
			case "S":
				if pre, post, ok := strings.Cut(text, "{#}"); ok {
					var lit strings.Builder
					for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "L:") {
						i++
						lit.WriteString(strings.TrimPrefix(strings.TrimPrefix(lines[i], "L:"), " ") + "\r\n")
					}
					text = pre + "{" + strconv.Itoa(lit.Len()) + "}" + post + "\r\n" + lit.String()
					if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "S:") {
						i++
						text += strings.TrimPrefix(strings.TrimPrefix(lines[i], "S:"), " ")
					}
				}
				if _, err := conn.Write([]byte(text + "\r\n")); err != nil {
					t.Errorf("%s line %d: write: %v", name, i+1, err)
					return
				}
			case "C":
				got, err := r.ReadString('\n')
				if err != nil {
					t.Errorf("%s: the client stopped (%v) where the transcript expects %q", name, err, text)
					return
				}
				if got = strings.TrimSuffix(got, "\r\n"); got != text {
					t.Errorf("%s: the client sent\n\t%q\nwhere the transcript expects\n\t%q", name, got, text)
					return
				}
			case "X":
				// the server drops the connection here
				return
			default:
				t.Errorf("%s: bad transcript line %q", name, lines[i])
				return
			}
		}
		if extra, err := r.ReadString('\n'); err == nil {
			t.Errorf("%s: the client sent %q after the transcript ended", name, extra)
		}
	}()
	return ln.Addr().String()
}

func dialReplay(t *testing.T, name string) *Client {
	t.Helper()
	c, err := Dial(context.Background(), replay(t, name), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func login(t *testing.T, c *Client) {
	t.Helper()
	if err := c.Login(context.Background(), "someone@example.com", "app-pass"); err != nil {
		t.Fatal(err)
	}
}

func TestListPutsInboxFirstAndDecodesNames(t *testing.T) {
	ctx := context.Background()
	c := dialReplay(t, "login_list")
	login(t, c)
	folders, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range folders {
		names = append(names, f.Name)
	}
	want := []string{"INBOX", "Archive", "Archive/Café", "Deleted Messages", "Notes", "Sent Messages"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("folders %q, want %q", names, want)
	}
	if !folders[3].HasFlag(`\trash`) || folders[4].Selectable() || !folders[0].Selectable() {
		t.Errorf("flags read wrong: %+v", folders)
	}
	st, err := c.Status(ctx, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if st != (Status{Messages: 3, Unseen: 1, UIDNext: 42, UIDValidity: 1700000000}) {
		t.Errorf("status %+v", st)
	}
	if ok, _ := c.Has(ctx, "move"); !ok {
		t.Error("the capabilities from the login OK were dropped")
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSearchAndFetchReadWithoutMarkingSeen(t *testing.T) {
	ctx := context.Background()
	c := dialReplay(t, "search_fetch")
	login(t, c)
	mb, err := c.Examine(ctx, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if mb != (Mailbox{Exists: 3, UIDValidity: 1700000000, UIDNext: 42}) {
		t.Errorf("mailbox %+v", mb)
	}
	uids, err := c.Search(ctx, Criteria{
		From: "sam@example.com", Subject: "Café", Unseen: true,
		Since: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(uids, []uint32{7, 8, 9}) {
		t.Errorf("uids %v, want 7 8 9", uids)
	}
	sums, err := c.FetchSummaries(ctx, uids)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 || sums[0].UID != 7 || sums[1].UID != 9 {
		t.Fatalf("summaries %+v, want uids 7 and 9 (8 is gone, the bare FETCH is ignored)", sums)
	}
	s := sums[0]
	if !s.Seen() || s.Size != 2048 || !s.Received.Equal(time.Date(2026, 11, 3, 15, 15, 0, 0, time.UTC)) {
		t.Errorf("summary %+v", s)
	}
	if !strings.Contains(string(s.Header), "Message-ID: <m1@example.com>\r\n") || !strings.HasSuffix(string(s.Header), "\r\n\r\n") {
		t.Errorf("header %q", s.Header)
	}
	if sums[1].Seen() || !strings.Contains(string(sums[1].Header), `"Sam \"S\" Lee"`) {
		t.Errorf("second summary %+v", sums[1])
	}
	b, err := c.FetchBody(ctx, 7, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Truncated || b.Size != 2048 || !strings.HasSuffix(string(b.Raw), "\r\n\r\nHello there\r\n") {
		t.Errorf("body %+v", b)
	}
	if _, err := c.FetchBody(ctx, 8, 64); !errors.Is(err, ErrNoMessage) {
		t.Errorf("a missing uid gave %v, want ErrNoMessage", err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMoveUsesMoveWhenTheServerHasIt(t *testing.T) {
	ctx := context.Background()
	c := dialReplay(t, "move")
	login(t, c)
	if _, err := c.Select(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := c.Move(ctx, []uint32{10, 7, 9, 7}, "Deleted Messages"); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMoveWithoutMoveExpungesOnlyTheMovedUIDs(t *testing.T) {
	ctx := context.Background()
	c := dialReplay(t, "move_copy")
	login(t, c)
	if _, err := c.Select(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := c.Move(ctx, []uint32{7}, "Deleted Messages"); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMoveIsRefusedWithoutMoveOrUIDPlus(t *testing.T) {
	ctx := context.Background()
	c := dialReplay(t, "move_refused")
	login(t, c)
	if _, err := c.Select(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	err := c.Move(ctx, []uint32{7}, "Deleted Messages")
	if err == nil || !strings.Contains(err.Error(), "neither MOVE nor UIDPLUS") {
		t.Fatalf("got %v, want a refusal naming MOVE and UIDPLUS", err)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMoveNeedsAWritableFolder(t *testing.T) {
	c := &Client{selected: "INBOX"}
	if err := c.Move(context.Background(), []uint32{1}, "Deleted Messages"); err == nil {
		t.Fatal("a move from an examined folder went ahead")
	}
}

func TestARefusedLoginSaysToCheckThePassword(t *testing.T) {
	c := dialReplay(t, "login_refused")
	err := c.Login(context.Background(), "someone@example.com", "old-pass")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("got %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), "old-pass") {
		t.Errorf("the error shows the password: %v", err)
	}
}

func TestLoginFallsBackToAuthenticatePlain(t *testing.T) {
	ctx := context.Background()
	c := dialReplay(t, "auth_plain")
	login(t, c)
	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
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
	if err := c.Login(context.Background(), "someone@example.com", "app-pass"); err == nil || !strings.Contains(err.Error(), "plain connection") {
		t.Fatalf("got %v, want a refusal to log in in the clear", err)
	}
}

func TestSearchRefusesLineBreaks(t *testing.T) {
	c := &Client{selected: "INBOX"}
	if _, err := c.Search(context.Background(), Criteria{Subject: "a\r\nb2 LOGOUT"}); err == nil {
		t.Fatal("a subject with CRLF was sent")
	}
	if _, err := c.Search(context.Background(), Criteria{MessageID: "<a@b>\r\nx"}); err == nil {
		t.Fatal("a Message-ID with CRLF was sent")
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
	if _, err := c.run(ctx, "NOOP"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
}

func TestParserReadsNestedListsQuotesAndSections(t *testing.T) {
	in := `* 1 FETCH (UID 5 FLAGS (\Seen $Junk) X-Q "a \"b\" \\c" N NIL BODY[HEADER.FIELDS (FROM)]<0> {3}` + "\r\nab\n)\r\n"
	p := &reader{r: bufio.NewReader(strings.NewReader(in))}
	r, err := p.readResp()
	if err != nil {
		t.Fatal(err)
	}
	want := []any{[]any{atom("UID"), atom("5"), atom("FLAGS"), []any{atom(`\Seen`), atom("$Junk")}, atom("X-Q"), `a "b" \c`, atom("N"), nil, atom("BODY[HEADER.FIELDS (FROM)]<0>"), "ab\n"}}
	if r.kind != "FETCH" || r.num != 1 || !reflect.DeepEqual(r.fields, want) {
		t.Errorf("got %s %d %#v", r.kind, r.num, r.fields)
	}
}

func TestParserSplitsTheResponseCode(t *testing.T) {
	p := &reader{r: bufio.NewReader(strings.NewReader("a3 NO [TRYCREATE] no such (folder\r\n"))}
	r, err := p.readResp()
	if err != nil {
		t.Fatal(err)
	}
	if r.tag != "a3" || r.kind != "NO" || r.code != "TRYCREATE" || r.text != "no such (folder" {
		t.Errorf("got %+v", r)
	}
}

func TestParserRefusesAnOversizedLiteral(t *testing.T) {
	p := &reader{r: bufio.NewReader(strings.NewReader("* 1 FETCH (BODY[] {99999999}\r\n"))}
	if _, err := p.readResp(); !errors.Is(err, errLong) {
		t.Fatalf("got %v, want errLong", err)
	}
}

func TestFolderNamesRoundTripModifiedUTF7(t *testing.T) {
	for _, tc := range []struct{ name, wire string }{
		{"INBOX", "INBOX"},
		{"Tom & Jerry", "Tom &- Jerry"},
		{"Archive/Café", "Archive/Caf&AOk-"},
		{"日本語", "&ZeVnLIqe-"},
		{"Smile 😀", "Smile &2D3eAA-"},
	} {
		if got := encodeFolder(tc.name); got != tc.wire {
			t.Errorf("encode %q = %q, want %q", tc.name, got, tc.wire)
		}
		if got, err := decodeFolder(tc.wire); err != nil || got != tc.name {
			t.Errorf("decode %q = %q, %v; want %q", tc.wire, got, err, tc.name)
		}
	}
	if _, err := decodeFolder("Bad&AOk"); err == nil {
		t.Error("an unterminated & decoded")
	}
}

func TestUIDSetsAreSortedWithRanges(t *testing.T) {
	if got := uidSet([]uint32{9, 3, 4, 5, 1, 4, 10}); got != "1,3:5,9:10" {
		t.Errorf("got %q", got)
	}
}

func TestParserRefusesAnEndlessLiteralLength(t *testing.T) {
	p := &reader{r: bufio.NewReader(strings.NewReader("* 1 FETCH (BODY[] {" + strings.Repeat("9", 100) + "}\r\n"))}
	if _, err := p.readResp(); err == nil || !strings.Contains(err.Error(), "literal length") {
		t.Fatalf("got %v, want a bad literal length", err)
	}
}

func TestParserCapsAWholeResponseNotJustEachValue(t *testing.T) {
	line := "* SEARCH" + strings.Repeat(" 1", maxResp) + "\r\n"
	p := &reader{r: bufio.NewReader(strings.NewReader(line))}
	if _, err := p.readResp(); !errors.Is(err, errLong) {
		t.Fatalf("got %v, want errLong", err)
	}
	list := "* 1 FETCH (" + strings.Repeat("()", maxResp) + ")\r\n"
	p = &reader{r: bufio.NewReader(strings.NewReader(list))}
	if _, err := p.readResp(); !errors.Is(err, errLong) {
		t.Fatalf("lists: got %v, want errLong", err)
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
