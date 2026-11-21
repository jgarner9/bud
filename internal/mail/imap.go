// Package mail is bud's mail client for iCloud: a small IMAP client (the
// subset RFC 9051 needs for reading, searching and moving mail) and, later,
// sends over net/smtp (Suite D106)
package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Client is one IMAP connection. It isn't safe for concurrent use; the
// server opens one per tool call
type Client struct {
	conn   net.Conn
	r      *reader
	w      *bufio.Writer
	tagN   int
	caps   map[string]bool
	secure bool // TLS, or plain only to loopback

	selected string // the selected folder, "" when none
	writable bool   // selected with SELECT, not EXAMINE
}

// ServerError is a NO or BAD answer to a command
type ServerError struct {
	Command string
	Status  string // NO or BAD
	Code    string
	Text    string
}

func (e *ServerError) Error() string {
	msg := fmt.Sprintf("the IMAP server answered %s with %s", e.Command, e.Status)
	if e.Code != "" {
		msg += " [" + e.Code + "]"
	}
	if e.Text != "" {
		msg += ": " + e.Text
	}
	return msg
}

// Dial connects to addr with implicit TLS (port 993) and reads the
// greeting. A nil config dials in the clear, which Login allows only on
// loopback, for tests
func Dial(ctx context.Context, addr string, conf *tls.Config) (*Client, error) {
	var conn net.Conn
	var err error
	if conf != nil {
		d := tls.Dialer{Config: conf}
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to the IMAP server %s: %w", addr, err)
	}
	c, err := NewClient(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// NewClient reads the server's greeting on conn
func NewClient(ctx context.Context, conn net.Conn) (*Client, error) {
	c := &Client{conn: conn, r: &reader{r: bufio.NewReader(conn)}, w: bufio.NewWriter(conn)}
	if _, ok := conn.(*tls.Conn); ok {
		c.secure = true
	} else if a, ok := conn.RemoteAddr().(*net.TCPAddr); ok && a.IP.IsLoopback() {
		c.secure = true
	}
	defer c.deadline(ctx)()
	g, err := c.r.readResp()
	if err != nil {
		return nil, c.wrap(ctx, "read the IMAP greeting", err)
	}
	if g.tag != "*" || (g.kind != "OK" && g.kind != "PREAUTH") {
		return nil, fmt.Errorf("the IMAP server refused the connection: %s %s", g.kind, g.text)
	}
	c.noteCaps(g)
	return c, nil
}

// Close drops the connection without logging out
func (c *Client) Close() error { return c.conn.Close() }

// deadline applies ctx's deadline and cancellation to the connection
// until the returned func runs
func (c *Client) deadline(ctx context.Context) func() {
	dl, _ := ctx.Deadline()
	_ = c.conn.SetDeadline(dl)
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Unix(1, 0)) })
	return func() { stop() }
}

func (c *Client) wrap(ctx context.Context, doing string, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fmt.Errorf("%s: %w", doing, err)
}

// noteCaps keeps a [CAPABILITY ...] code from a status response
func (c *Client) noteCaps(r *resp) {
	if rest, ok := strings.CutPrefix(r.code, "CAPABILITY "); ok {
		c.setCaps(strings.Fields(rest))
	}
}

func (c *Client) setCaps(list []string) {
	c.caps = map[string]bool{}
	for _, s := range list {
		c.caps[strings.ToUpper(s)] = true
	}
}

// qstr is a string argument, sent quoted, or as a literal when it can't be
type qstr string

// run sends one command and reads to its tagged answer, returning the
// untagged responses it saw. Parts are raw protocol text, except qstr
func (c *Client) run(ctx context.Context, parts ...any) ([]*resp, error) {
	defer c.deadline(ctx)()
	name := fmt.Sprint(parts[0])
	if name == "UID" && len(parts) > 1 {
		name += " " + fmt.Sprint(parts[1])
	}
	c.tagN++
	tag := "a" + strconv.Itoa(c.tagN)
	var untagged []*resp
	_, _ = c.w.WriteString(tag)
	for _, p := range parts {
		_ = c.w.WriteByte(' ')
		s, isQ := p.(qstr)
		if !isQ {
			_, _ = fmt.Fprint(c.w, p)
			continue
		}
		if !needsLiteral(string(s)) {
			_, _ = c.w.WriteString(quote(string(s)))
			continue
		}
		// a synchronizing literal: wait for the server's go-ahead
		_, _ = fmt.Fprintf(c.w, "{%d}\r\n", len(s))
		if err := c.w.Flush(); err != nil {
			return nil, c.wrap(ctx, "send "+name, err)
		}
		for {
			r, err := c.r.readResp()
			if err != nil {
				return nil, c.wrap(ctx, "send "+name, err)
			}
			if r.tag == "+" {
				break
			}
			if r.tag == tag {
				return nil, &ServerError{Command: name, Status: r.kind, Code: r.code, Text: r.text}
			}
			untagged = append(untagged, r)
		}
		_, _ = c.w.WriteString(string(s))
	}
	_, _ = c.w.WriteString("\r\n")
	if err := c.w.Flush(); err != nil {
		return nil, c.wrap(ctx, "send "+name, err)
	}
	return c.finish(ctx, tag, name, untagged)
}

func (c *Client) finish(ctx context.Context, tag, name string, untagged []*resp) ([]*resp, error) {
	for {
		r, err := c.r.readResp()
		if err != nil {
			return nil, c.wrap(ctx, "read the answer to "+name, err)
		}
		switch {
		case r.tag == "*":
			if r.kind == "BYE" && name != "LOGOUT" {
				return nil, fmt.Errorf("the IMAP server closed the connection during %s: %s", name, r.text)
			}
			if r.kind == "CAPABILITY" {
				var list []string
				for _, f := range r.fields {
					if s, ok := str(f); ok {
						list = append(list, s)
					}
				}
				c.setCaps(list)
			}
			c.noteCaps(r)
			untagged = append(untagged, r)
		case r.tag == tag:
			c.noteCaps(r)
			if r.kind != "OK" {
				return nil, &ServerError{Command: name, Status: r.kind, Code: r.code, Text: r.text}
			}
			return untagged, nil
		default:
			return nil, fmt.Errorf("the IMAP server sent %s %s during %s", r.tag, r.kind, name)
		}
	}
}

// Has says the server lists the capability, asking for the list once if
// the greeting didn't carry it
func (c *Client) Has(ctx context.Context, capability string) (bool, error) {
	if c.caps == nil {
		if _, err := c.run(ctx, "CAPABILITY"); err != nil {
			return false, err
		}
	}
	return c.caps[strings.ToUpper(capability)], nil
}

// ErrAuth is a refused login
var ErrAuth = errors.New("iCloud refused the login: check the Apple ID in BUD_MAIL_USER and that BUD_MAIL_PASSWORD is a current app-specific password")

// Login logs in with LOGIN, or AUTHENTICATE PLAIN when the server
// disables LOGIN. It refuses to send a password in the clear except on
// loopback
func (c *Client) Login(ctx context.Context, user, pass string) error {
	if !c.secure {
		return errors.New("refusing to log in over a plain connection: use TLS (port 993)")
	}
	disabled, err := c.Has(ctx, "LOGINDISABLED")
	if err != nil {
		return err
	}
	if disabled {
		plain, err := c.Has(ctx, "AUTH=PLAIN")
		if err != nil {
			return err
		}
		if !plain {
			return errors.New("the IMAP server disables LOGIN and doesn't offer AUTHENTICATE PLAIN, so bud can't log in")
		}
	}
	// capabilities often grow after login: keep a list the OK carries, else
	// ask again when one is needed
	c.caps = nil
	if disabled {
		err = c.authPlain(ctx, user, pass)
	} else {
		_, err = c.run(ctx, "LOGIN", qstr(user), qstr(pass))
	}
	var se *ServerError
	if errors.As(err, &se) && se.Status == "NO" {
		return ErrAuth
	}
	return err
}

func (c *Client) authPlain(ctx context.Context, user, pass string) error {
	defer c.deadline(ctx)()
	c.tagN++
	tag := "a" + strconv.Itoa(c.tagN)
	_, _ = c.w.WriteString(tag + " AUTHENTICATE PLAIN\r\n")
	if err := c.w.Flush(); err != nil {
		return c.wrap(ctx, "send AUTHENTICATE", err)
	}
	r, err := c.r.readResp()
	if err != nil {
		return c.wrap(ctx, "read the answer to AUTHENTICATE", err)
	}
	if r.tag != "+" {
		if r.tag == tag {
			return &ServerError{Command: "AUTHENTICATE", Status: r.kind, Code: r.code, Text: r.text}
		}
		return fmt.Errorf("the IMAP server sent %s %s instead of a go-ahead for AUTHENTICATE", r.tag, r.kind)
	}
	_, _ = c.w.WriteString(base64.StdEncoding.EncodeToString([]byte("\x00"+user+"\x00"+pass)) + "\r\n")
	if err := c.w.Flush(); err != nil {
		return c.wrap(ctx, "send AUTHENTICATE", err)
	}
	_, err = c.finish(ctx, tag, "AUTHENTICATE", nil)
	return err
}

// Logout ends the session and closes the connection
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.run(ctx, "LOGOUT")
	if cerr := c.conn.Close(); err == nil {
		err = cerr
	}
	return err
}

// Folder is one mailbox from LIST
type Folder struct {
	Name  string   // decoded from modified UTF-7
	Delim string   // the hierarchy delimiter, "" for none
	Flags []string // as the server sent them, such as \HasNoChildren or \Trash
}

// Selectable says the folder can hold messages
func (f Folder) Selectable() bool {
	return !f.HasFlag(`\Noselect`) && !f.HasFlag(`\NonExistent`)
}

// HasFlag reports a LIST flag, ignoring case
func (f Folder) HasFlag(flag string) bool {
	return slices.ContainsFunc(f.Flags, func(s string) bool { return strings.EqualFold(s, flag) })
}

// List returns every folder, INBOX first and the rest by name
func (c *Client) List(ctx context.Context) ([]Folder, error) {
	rs, err := c.run(ctx, "LIST", qstr(""), qstr("*"))
	if err != nil {
		return nil, err
	}
	var out []Folder
	for _, r := range rs {
		if r.kind != "LIST" || len(r.fields) < 3 {
			continue
		}
		flags, _ := r.fields[0].([]any)
		delim, _ := str(r.fields[1])
		raw, ok := str(r.fields[2])
		if !ok {
			continue
		}
		name, err := decodeFolder(raw)
		if err != nil {
			return nil, err
		}
		f := Folder{Name: name, Delim: delim}
		for _, fl := range flags {
			if s, ok := str(fl); ok {
				f.Flags = append(f.Flags, s)
			}
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Folder) int {
		ai, bi := strings.EqualFold(a.Name, "INBOX"), strings.EqualFold(b.Name, "INBOX")
		if ai != bi {
			if ai {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// Status is a folder's counts from STATUS
type Status struct {
	Messages, Unseen, UIDNext, UIDValidity uint32
}

// Status asks for a folder's counts without selecting it
func (c *Client) Status(ctx context.Context, folder string) (Status, error) {
	rs, err := c.run(ctx, "STATUS", qstr(encodeFolder(folder)), "(MESSAGES UNSEEN UIDNEXT UIDVALIDITY)")
	if err != nil {
		return Status{}, err
	}
	var st Status
	for _, r := range rs {
		if r.kind != "STATUS" || len(r.fields) < 2 {
			continue
		}
		items, _ := r.fields[1].([]any)
		for i := 0; i+1 < len(items); i += 2 {
			k, _ := str(items[i])
			n, _ := num(items[i+1])
			switch strings.ToUpper(k) {
			case "MESSAGES":
				st.Messages = n
			case "UNSEEN":
				st.Unseen = n
			case "UIDNEXT":
				st.UIDNext = n
			case "UIDVALIDITY":
				st.UIDValidity = n
			}
		}
	}
	return st, nil
}

// Mailbox is what SELECT or EXAMINE reports
type Mailbox struct {
	Exists, UIDValidity, UIDNext uint32
}

// Examine opens a folder read-only, so nothing a read does changes it
func (c *Client) Examine(ctx context.Context, folder string) (Mailbox, error) {
	return c.open(ctx, "EXAMINE", folder)
}

// Select opens a folder for changes, as Move needs
func (c *Client) Select(ctx context.Context, folder string) (Mailbox, error) {
	return c.open(ctx, "SELECT", folder)
}

func (c *Client) open(ctx context.Context, cmd, folder string) (Mailbox, error) {
	c.selected, c.writable = "", false
	rs, err := c.run(ctx, cmd, qstr(encodeFolder(folder)))
	if err != nil {
		var se *ServerError
		if errors.As(err, &se) && se.Status == "NO" {
			return Mailbox{}, fmt.Errorf("there's no folder %q: list_folders shows the names (%w)", folder, err)
		}
		return Mailbox{}, err
	}
	var mb Mailbox
	for _, r := range rs {
		switch {
		case r.kind == "EXISTS" && r.hasNum:
			mb.Exists = r.num
		case r.kind == "OK":
			k, v, _ := strings.Cut(r.code, " ")
			n, _ := strconv.ParseUint(v, 10, 32)
			switch strings.ToUpper(k) {
			case "UIDVALIDITY":
				mb.UIDValidity = uint32(n)
			case "UIDNEXT":
				mb.UIDNext = uint32(n)
			}
		}
	}
	c.selected, c.writable = folder, cmd == "SELECT"
	return mb, nil
}

// Criteria picks messages for Search. Empty fields are left out; all set
// fields must match
type Criteria struct {
	From, To, Subject, Text string
	MessageID               string    // the Message-ID header, with or without its <>
	Since, Before           time.Time // by the date the server received it; only the day counts
	Unseen                  bool
}

// Search returns the UIDs in the selected folder that match, ascending
func (c *Client) Search(ctx context.Context, cr Criteria) ([]uint32, error) {
	if c.selected == "" {
		return nil, errors.New("search needs a folder: examine one first")
	}
	var keys []any
	utf8 := false
	add := func(key, v string) error {
		if v == "" {
			return nil
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("the %s search text has a line break; search for one line", strings.ToLower(key))
		}
		for i := 0; i < len(v); i++ {
			if v[i] >= 0x80 {
				utf8 = true
			}
		}
		keys = append(keys, key, qstr(v))
		return nil
	}
	for _, kv := range [][2]string{{"FROM", cr.From}, {"TO", cr.To}, {"SUBJECT", cr.Subject}, {"TEXT", cr.Text}} {
		if err := add(kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	if cr.MessageID != "" {
		if strings.ContainsAny(cr.MessageID, "\r\n") || needsLiteral(cr.MessageID) {
			return nil, fmt.Errorf("%q isn't a Message-ID", cr.MessageID)
		}
		keys = append(keys, "HEADER", "Message-ID", qstr(cr.MessageID))
	}
	if !cr.Since.IsZero() {
		keys = append(keys, "SINCE", cr.Since.Format("2-Jan-2006"))
	}
	if !cr.Before.IsZero() {
		keys = append(keys, "BEFORE", cr.Before.Format("2-Jan-2006"))
	}
	if cr.Unseen {
		keys = append(keys, "UNSEEN")
	}
	if len(keys) == 0 {
		keys = append(keys, "ALL")
	}
	parts := []any{"UID", "SEARCH"}
	if utf8 {
		parts = append(parts, "CHARSET", "UTF-8")
	}
	rs, err := c.run(ctx, append(parts, keys...)...)
	if err != nil {
		return nil, err
	}
	var uids []uint32
	for _, r := range rs {
		if r.kind != "SEARCH" {
			continue
		}
		for _, f := range r.fields {
			if n, ok := num(f); ok {
				uids = append(uids, n)
			}
		}
	}
	slices.Sort(uids)
	return slices.Compact(uids), nil
}

// headerFields are the headers a summary fetches
const headerFields = "DATE FROM TO CC SUBJECT MESSAGE-ID IN-REPLY-TO REFERENCES"

// Summary is one message's flags, size and main headers, fetched without
// marking it read
type Summary struct {
	UID      uint32
	Flags    []string
	Size     uint32
	Received time.Time // INTERNALDATE
	Header   []byte    // the raw header fields in headerFields
}

// Seen says the message has been read
func (s Summary) Seen() bool {
	return slices.ContainsFunc(s.Flags, func(f string) bool { return strings.EqualFold(f, `\Seen`) })
}

// FetchSummaries fetches the summaries of uids in the selected folder, in
// the order the server sends them. UIDs that no longer exist are left out
func (c *Client) FetchSummaries(ctx context.Context, uids []uint32) ([]Summary, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if c.selected == "" {
		return nil, errors.New("fetch needs a folder: examine one first")
	}
	rs, err := c.run(ctx, "UID", "FETCH", uidSet(uids), "(UID FLAGS RFC822.SIZE INTERNALDATE BODY.PEEK[HEADER.FIELDS ("+headerFields+")])")
	if err != nil {
		return nil, err
	}
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var out []Summary
	for _, r := range rs {
		items := fetchItems(r)
		if items == nil {
			continue
		}
		s := Summary{}
		for k, v := range items {
			switch {
			case k == "UID":
				s.UID, _ = num(v)
			case k == "RFC822.SIZE":
				s.Size, _ = num(v)
			case k == "FLAGS":
				list, _ := v.([]any)
				for _, f := range list {
					if fs, ok := str(f); ok {
						s.Flags = append(s.Flags, fs)
					}
				}
			case k == "INTERNALDATE":
				d, _ := str(v)
				s.Received, _ = time.Parse("_2-Jan-2006 15:04:05 -0700", d)
			case strings.HasPrefix(k, "BODY[HEADER.FIELDS"):
				h, _ := str(v)
				s.Header = []byte(h)
			}
		}
		if want[s.UID] {
			out = append(out, s)
			delete(want, s.UID)
		}
	}
	return out, nil
}

// Body is the start of one message's raw RFC 5322 text
type Body struct {
	UID       uint32
	Size      uint32 // the whole message
	Raw       []byte // at most the max asked for
	Truncated bool
}

// ErrNoMessage says a UID isn't in the selected folder
var ErrNoMessage = errors.New("no message has that uid in the folder: search again, since it may have moved")

// FetchBody fetches up to max bytes of one message with BODY.PEEK, so it
// stays unread
func (c *Client) FetchBody(ctx context.Context, uid uint32, max int) (Body, error) {
	if c.selected == "" {
		return Body{}, errors.New("fetch needs a folder: examine one first")
	}
	if max <= 0 || max > maxLiteral {
		max = maxLiteral
	}
	rs, err := c.run(ctx, "UID", "FETCH", uidSet([]uint32{uid}), fmt.Sprintf("(UID RFC822.SIZE BODY.PEEK[]<0.%d>)", max))
	if err != nil {
		return Body{}, err
	}
	for _, r := range rs {
		items := fetchItems(r)
		if n, _ := num(items["UID"]); items == nil || n != uid {
			continue
		}
		b := Body{UID: uid}
		b.Size, _ = num(items["RFC822.SIZE"])
		for k, v := range items {
			if k == "BODY[]" || strings.HasPrefix(k, "BODY[]<") {
				s, _ := str(v)
				b.Raw = []byte(s)
			}
		}
		b.Truncated = int(b.Size) > len(b.Raw)
		return b, nil
	}
	return Body{}, ErrNoMessage
}

// fetchItems turns a FETCH response's list into item name to value, with
// names upper-cased. It's nil for any other response
func fetchItems(r *resp) map[string]any {
	if r.kind != "FETCH" || len(r.fields) != 1 {
		return nil
	}
	list, ok := r.fields[0].([]any)
	if !ok {
		return nil
	}
	items := map[string]any{}
	for i := 0; i+1 < len(list); i += 2 {
		if k, ok := list[i].(atom); ok {
			items[strings.ToUpper(string(k))] = list[i+1]
		}
	}
	return items
}

// Move moves uids from the selected folder to dest, with MOVE when the
// server has it. Otherwise it copies, marks exactly those uids deleted and
// expunges only them, which needs UIDPLUS; without it bud refuses, since a
// plain EXPUNGE would also remove anything else already marked deleted. If
// the copy works and a later step fails, the message is in both folders,
// never in neither
func (c *Client) Move(ctx context.Context, uids []uint32, dest string) error {
	if len(uids) == 0 {
		return nil
	}
	if c.selected == "" || !c.writable {
		return errors.New("move needs a folder opened with Select")
	}
	set := uidSet(uids)
	box := qstr(encodeFolder(dest))
	move, err := c.Has(ctx, "MOVE")
	if err != nil {
		return err
	}
	if move {
		_, err := c.run(ctx, "UID", "MOVE", set, box)
		return err
	}
	plus, err := c.Has(ctx, "UIDPLUS")
	if err != nil {
		return err
	}
	if !plus {
		return errors.New("the IMAP server has neither MOVE nor UIDPLUS, so bud won't move mail: expunging without UIDPLUS could remove other messages")
	}
	if _, err := c.run(ctx, "UID", "COPY", set, box); err != nil {
		return err
	}
	if _, err := c.run(ctx, "UID", "STORE", set, "+FLAGS.SILENT", `(\Deleted)`); err != nil {
		return err
	}
	_, err = c.run(ctx, "UID", "EXPUNGE", set)
	return err
}

// uidSet writes uids as an IMAP sequence set, sorted, with runs as ranges
func uidSet(uids []uint32) string {
	u := slices.Clone(uids)
	slices.Sort(u)
	u = slices.Compact(u)
	var b strings.Builder
	for i := 0; i < len(u); {
		j := i
		for j+1 < len(u) && u[j+1] == u[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatUint(uint64(u[i]), 10))
		if j > i {
			b.WriteByte(':')
			b.WriteString(strconv.FormatUint(uint64(u[j]), 10))
		}
		i = j + 1
	}
	return b.String()
}
