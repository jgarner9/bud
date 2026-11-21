package mail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Recording a live session (bud mail-mcp --record): one scripted, read-only
// IMAP session written as a transcript in the testdata format, with the
// login, every literal and every address redacted, so the hand-written
// transcripts can be checked against what iCloud really sends. It reports
// what the live run needs to know: MOVE, UIDPLUS and the trash folder

// Findings is what a recording learned about the server
type Findings struct {
	Capabilities []string
	Move         bool
	UIDPlus      bool
	Trash        string // the folder with \Trash, "" when none has it
	Folders      []string
}

// Record runs the scripted session against the account, writing the
// transcript to w. It only reads: EXAMINE, SEARCH and FETCH with PEEK
func (a *Account) Record(ctx context.Context, w io.Writer) (Findings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var f Findings
	var conn net.Conn
	var err error
	if a.tls != nil {
		d := tls.Dialer{Config: a.tls}
		conn, err = d.DialContext(ctx, "tcp", a.addr)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", a.addr)
	}
	if err != nil {
		return f, fmt.Errorf("connect to the IMAP server %s: %w", a.addr, err)
	}
	tr := newTracer(conn, w)
	c, err := NewClient(ctx, tr)
	if err != nil {
		_ = conn.Close()
		return f, err
	}
	c.secure = a.tls != nil || c.secure
	defer c.Close()
	if err := c.Login(ctx, a.user, a.pass); err != nil {
		return f, err
	}
	if _, err := c.Has(ctx, "MOVE"); err != nil {
		return f, err
	}
	for k := range c.caps {
		f.Capabilities = append(f.Capabilities, k)
	}
	f.Move, f.UIDPlus = c.caps["MOVE"], c.caps["UIDPLUS"]
	list, err := c.List(ctx)
	if err != nil {
		return f, err
	}
	for _, fo := range list {
		f.Folders = append(f.Folders, fo.Name)
		if fo.HasFlag(`\Trash`) && f.Trash == "" {
			f.Trash = fo.Name
		}
	}
	if _, err := c.Status(ctx, "INBOX"); err != nil {
		return f, err
	}
	if _, err := c.Examine(ctx, "INBOX"); err != nil {
		return f, err
	}
	uids, err := c.Search(ctx, Criteria{})
	if err != nil {
		return f, err
	}
	if len(uids) > 2 {
		uids = uids[len(uids)-2:]
	}
	if _, err := c.FetchSummaries(ctx, uids); err != nil {
		return f, err
	}
	if len(uids) > 0 {
		if _, err := c.FetchBody(ctx, uids[len(uids)-1], 512); err != nil {
			return f, err
		}
	}
	if err := c.Logout(ctx); err != nil {
		return f, err
	}
	tr.flush()
	return f, nil
}

// tracer copies a connection's lines into a transcript as it goes,
// redacting as it writes
type tracer struct {
	net.Conn
	mu      sync.Mutex
	out     *bufio.Writer
	client  lineSplitter
	server  lineSplitter
	authing bool // the next client line is AUTHENTICATE's secret
}

func newTracer(conn net.Conn, w io.Writer) *tracer {
	t := &tracer{Conn: conn, out: bufio.NewWriter(w)}
	_, _ = t.out.WriteString("# Recorded by bud mail-mcp --record: the login, literals and addresses are\n# redacted, so a literal here is a placeholder and not its real length\n")
	t.client.emit = func(l string, lit bool) { t.line("C", l, lit) }
	t.server.emit = func(l string, lit bool) { t.line("S", l, lit) }
	return t
}

func (t *tracer) Read(p []byte) (int, error) {
	n, err := t.Conn.Read(p)
	t.mu.Lock()
	t.server.feed(p[:n])
	t.mu.Unlock()
	return n, err
}

func (t *tracer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.client.feed(p)
	t.mu.Unlock()
	return t.Conn.Write(p)
}

func (t *tracer) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.out.Flush()
}

var (
	reLogin   = regexp.MustCompile(`(?i)^(\S+ LOGIN) .*$`)
	reAuth    = regexp.MustCompile(`(?i)^\S+ AUTHENTICATE PLAIN`)
	reAddress = regexp.MustCompile(`[^\s"<>()]+@[^\s"<>()]+`)
	reLitMark = regexp.MustCompile(`\{\d+\}$`)
)

func (t *tracer) line(dir, l string, lit bool) {
	switch {
	case lit:
		_, _ = t.out.WriteString("L: [redacted]\n")
		return
	case dir == "C" && t.authing:
		l, t.authing = "<redacted>", false
	case dir == "C" && reLogin.MatchString(l):
		l = reLogin.ReplaceAllString(l, "$1 <redacted>")
	case dir == "C" && reAuth.MatchString(l):
		t.authing = true
	}
	l = reAddress.ReplaceAllString(l, "someone@example.com")
	l = reLitMark.ReplaceAllString(l, "{#}")
	_, _ = t.out.WriteString(dir + ": " + l + "\n")
}

// lineSplitter cuts one direction of the stream into lines, passing a
// literal's bytes as one placeholder after the line that announced it
type lineSplitter struct {
	buf  bytes.Buffer
	lit  int // literal bytes still to come
	emit func(line string, literal bool)
}

func (s *lineSplitter) feed(p []byte) {
	for len(p) > 0 {
		if s.lit > 0 {
			k := min(s.lit, len(p))
			s.lit -= k
			p = p[k:]
			if s.lit == 0 {
				s.emit("", true)
			}
			continue
		}
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.buf.Write(p)
			return
		}
		s.buf.Write(p[:i])
		p = p[i+1:]
		l := strings.TrimSuffix(s.buf.String(), "\r")
		s.buf.Reset()
		s.emit(l, false)
		if m := reLitMark.FindString(l); m != "" {
			s.lit, _ = strconv.Atoi(m[1 : len(m)-1])
		}
	}
}
