// Package mail is bud's mail client for iCloud: a thin adapter over the
// go-imap library for reading, searching and moving mail (Suite D138) and
// sends over net/smtp (Suite D106)
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
)

// Client is one IMAP connection. It isn't safe for concurrent use; the
// server opens one per tool call
type Client struct {
	conn   net.Conn
	c      *imapclient.Client
	secure bool // TLS, or plain only to loopback

	selected string // the selected folder, "" when none
	writable bool   // selected with SELECT, not EXAMINE
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
	c := &Client{conn: conn, c: imapclient.New(conn, nil)}
	if _, ok := conn.(*tls.Conn); ok {
		c.secure = true
	} else if a, ok := conn.RemoteAddr().(*net.TCPAddr); ok && a.IP.IsLoopback() {
		c.secure = true
	}
	err := c.guard(ctx, "read the IMAP greeting", func() error { return c.c.WaitGreeting() })
	if err != nil {
		var ie *imap.Error
		if errors.As(err, &ie) {
			return nil, fmt.Errorf("the IMAP server refused the connection: %s %s", ie.Type, ie.Text)
		}
		return nil, err
	}
	if c.c.State() == imap.ConnStateAuthenticated {
		// bud always logs in itself; a connection authenticated some other
		// way isn't the account it was given
		return nil, errors.New("the IMAP server sent PREAUTH, an already logged-in connection: bud only uses connections it logs in itself")
	}
	return c, nil
}

// Close drops the connection without logging out
func (c *Client) Close() error {
	_ = c.c.Close()
	return nil
}

// guard runs one library call under ctx. The library's Wait methods take no
// context, so a cancel or expiry kills the connection, and an error that
// arrives after ctx fired is reported as ctx's own
func (c *Client) guard(ctx context.Context, doing string, fn func() error) error {
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	err := fn()
	stop()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fmt.Errorf("%s: %w", doing, err)
}

// Has says the server lists the capability, asking for the list once if
// the greeting didn't carry it. A list that can't be had counts as absent
func (c *Client) Has(ctx context.Context, capability string) (bool, error) {
	caps, err := c.caps(ctx)
	if err != nil {
		return false, err
	}
	return hasCap(caps, capability), nil
}

func (c *Client) caps(ctx context.Context) (imap.CapSet, error) {
	var caps imap.CapSet
	err := c.guard(ctx, "read the IMAP capabilities", func() error {
		caps = c.c.Caps()
		if caps == nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	})
	return caps, err
}

func hasCap(caps imap.CapSet, name string) bool {
	if caps.Has(imap.Cap(name)) {
		return true
	}
	for k := range caps {
		if strings.EqualFold(string(k), name) {
			return true
		}
	}
	return false
}

// Capabilities lists the server's capabilities, sorted
func (c *Client) Capabilities(ctx context.Context) ([]string, error) {
	caps, err := c.caps(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for k := range caps {
		out = append(out, string(k))
	}
	slices.Sort(out)
	return out, nil
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
	caps, err := c.caps(ctx)
	if err != nil {
		return err
	}
	if caps == nil {
		return errors.New("the IMAP server's capabilities couldn't be read, so bud can't tell how to log in")
	}
	disabled := hasCap(caps, string(imap.CapLoginDisabled))
	if disabled && !hasCap(caps, string(imap.AuthCap(sasl.Plain))) {
		return errors.New("the IMAP server disables LOGIN and doesn't offer AUTHENTICATE PLAIN, so bud can't log in")
	}
	err = c.guard(ctx, "log in", func() error {
		if disabled {
			return c.c.Authenticate(sasl.NewPlainClient("", user, pass))
		}
		return c.c.Login(user, pass).Wait()
	})
	var ie *imap.Error
	if errors.As(err, &ie) && ie.Type == imap.StatusResponseTypeNo {
		return ErrAuth
	}
	return err
}

// Logout ends the session and closes the connection
func (c *Client) Logout(ctx context.Context) error {
	err := c.guard(ctx, "log out", func() error { return c.c.Logout().Wait() })
	_ = c.c.Close()
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
	var data []*imap.ListData
	err := c.guard(ctx, "list the folders", func() error {
		var err error
		data, err = c.c.List("", "*", nil).Collect()
		return err
	})
	if err != nil {
		return nil, err
	}
	var out []Folder
	for _, d := range data {
		f := Folder{Name: d.Mailbox}
		if d.Delim != 0 {
			f.Delim = string(d.Delim)
		}
		for _, a := range d.Attrs {
			f.Flags = append(f.Flags, string(a))
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
	var d *imap.StatusData
	err := c.guard(ctx, "ask for the status of "+folder, func() error {
		var err error
		d, err = c.c.Status(folder, &imap.StatusOptions{NumMessages: true, NumUnseen: true, UIDNext: true, UIDValidity: true}).Wait()
		return err
	})
	if err != nil {
		return Status{}, err
	}
	st := Status{UIDNext: uint32(d.UIDNext), UIDValidity: d.UIDValidity}
	if d.NumMessages != nil {
		st.Messages = *d.NumMessages
	}
	if d.NumUnseen != nil {
		st.Unseen = *d.NumUnseen
	}
	return st, nil
}

// Mailbox is what SELECT or EXAMINE reports
type Mailbox struct {
	Exists, UIDValidity, UIDNext uint32
}

// Examine opens a folder read-only, so nothing a read does changes it
func (c *Client) Examine(ctx context.Context, folder string) (Mailbox, error) {
	return c.open(ctx, folder, true)
}

// Select opens a folder for changes, as Move needs
func (c *Client) Select(ctx context.Context, folder string) (Mailbox, error) {
	return c.open(ctx, folder, false)
}

func (c *Client) open(ctx context.Context, folder string, readOnly bool) (Mailbox, error) {
	c.selected, c.writable = "", false
	var d *imap.SelectData
	err := c.guard(ctx, "open "+folder, func() error {
		var err error
		d, err = c.c.Select(folder, &imap.SelectOptions{ReadOnly: readOnly}).Wait()
		return err
	})
	if err != nil {
		var ie *imap.Error
		if errors.As(err, &ie) && ie.Type == imap.StatusResponseTypeNo && ctx.Err() == nil {
			return Mailbox{}, fmt.Errorf("there's no folder %q: list_folders shows the names (%w)", folder, err)
		}
		return Mailbox{}, err
	}
	c.selected, c.writable = folder, !readOnly
	return Mailbox{Exists: d.NumMessages, UIDValidity: d.UIDValidity, UIDNext: uint32(d.UIDNext)}, nil
}

// Criteria picks messages for Search. Empty fields are left out; all set
// fields must match
type Criteria struct {
	From, To, Subject, Text string
	MessageID               string    // the Message-ID header, with or without its <>
	Since, Before           time.Time // by the date the server received it; only the day counts
	Unseen                  bool
}

// criteria checks cr and turns it into the library's, refusing text that
// could carry a second command
func (cr Criteria) criteria() (*imap.SearchCriteria, error) {
	out := &imap.SearchCriteria{}
	header := func(key, v string) error {
		if v == "" {
			return nil
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("the %s search text has a line break; search for one line", strings.ToLower(key))
		}
		out.Header = append(out.Header, imap.SearchCriteriaHeaderField{Key: key, Value: v})
		return nil
	}
	for _, kv := range [][2]string{{"FROM", cr.From}, {"TO", cr.To}, {"SUBJECT", cr.Subject}} {
		if err := header(kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	if cr.Text != "" {
		if strings.ContainsAny(cr.Text, "\r\n") {
			return nil, fmt.Errorf("the %s search text has a line break; search for one line", "text")
		}
		out.Text = []string{cr.Text}
	}
	if cr.MessageID != "" {
		if strings.ContainsAny(cr.MessageID, "\r\n") || plainOnly(cr.MessageID) {
			return nil, fmt.Errorf("%q isn't a Message-ID", cr.MessageID)
		}
		out.Header = append(out.Header, imap.SearchCriteriaHeaderField{Key: "Message-ID", Value: cr.MessageID})
	}
	out.Since, out.Before = cr.Since, cr.Before
	if cr.Unseen {
		out.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	return out, nil
}

// plainOnly is true for text that isn't plain printable ASCII of a sane
// length, which no Message-ID is
func plainOnly(s string) bool {
	if len(s) > 1000 {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return true
		}
	}
	return false
}

// Search returns the UIDs in the selected folder that match, ascending
func (c *Client) Search(ctx context.Context, cr Criteria) ([]uint32, error) {
	if c.selected == "" {
		return nil, errors.New("search needs a folder: examine one first")
	}
	crit, err := cr.criteria()
	if err != nil {
		return nil, err
	}
	var d *imap.SearchData
	err = c.guard(ctx, "search "+c.selected, func() error {
		var err error
		d, err = c.c.UIDSearch(crit, nil).Wait()
		return err
	})
	if err != nil {
		return nil, err
	}
	var uids []uint32
	for _, u := range d.AllUIDs() {
		uids = append(uids, uint32(u))
	}
	slices.Sort(uids)
	return slices.Compact(uids), nil
}

// maxHeader caps the header bytes a summary fetches per message
const maxHeader = 64 << 10

// headerFields are the headers a summary fetches
const headerFields = "DATE FROM TO CC SUBJECT MESSAGE-ID IN-REPLY-TO REFERENCES REPLY-TO"

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

func uidSet(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	return set
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
	section := &imap.FetchItemBodySection{
		Specifier:    imap.PartSpecifierHeader,
		HeaderFields: strings.Fields(headerFields),
		Peek:         true,
		// the library buffers a whole literal, so ask the server to stop
		Partial: &imap.SectionPartial{Offset: 0, Size: maxHeader},
	}
	var msgs []*imapclient.FetchMessageBuffer
	err := c.guard(ctx, "fetch the summaries", func() error {
		var err error
		msgs, err = c.c.Fetch(uidSet(uids), &imap.FetchOptions{
			UID:          true,
			Flags:        true,
			RFC822Size:   true,
			InternalDate: true,
			BodySection:  []*imap.FetchItemBodySection{section},
		}).Collect()
		return err
	})
	if err != nil {
		return nil, err
	}
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var out []Summary
	for _, m := range msgs {
		uid := uint32(m.UID)
		if !want[uid] {
			continue
		}
		delete(want, uid)
		s := Summary{UID: uid, Size: uint32(m.RFC822Size), Received: m.InternalDate, Header: m.FindBodySection(section)}
		for _, f := range m.Flags {
			s.Flags = append(s.Flags, string(f))
		}
		out = append(out, s)
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
	if max <= 0 || max > maxFetch {
		max = maxFetch
	}
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: int64(max)}}
	var msgs []*imapclient.FetchMessageBuffer
	err := c.guard(ctx, "fetch the message", func() error {
		var err error
		msgs, err = c.c.Fetch(uidSet([]uint32{uid}), &imap.FetchOptions{
			UID:         true,
			RFC822Size:  true,
			BodySection: []*imap.FetchItemBodySection{section},
		}).Collect()
		return err
	})
	if err != nil {
		return Body{}, err
	}
	for _, m := range msgs {
		if uint32(m.UID) != uid {
			continue
		}
		b := Body{UID: uid, Size: uint32(m.RFC822Size), Raw: m.FindBodySection(section)}
		b.Truncated = int(b.Size) > len(b.Raw)
		return b, nil
	}
	return Body{}, ErrNoMessage
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
	caps, err := c.caps(ctx)
	if err != nil {
		return err
	}
	switch {
	case hasCap(caps, string(imap.CapMove)):
		return c.guard(ctx, "move the messages", func() error {
			_, err := c.c.Move(set, dest).Wait()
			return err
		})
	case hasCap(caps, string(imap.CapUIDPlus)):
		return c.guard(ctx, "move the messages", func() error {
			if _, err := c.c.Copy(set, dest).Wait(); err != nil {
				return err
			}
			deleted := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}
			if err := c.c.Store(set, deleted, nil).Close(); err != nil {
				return err
			}
			return c.c.UIDExpunge(set).Close()
		})
	default:
		return errors.New("the IMAP server has neither MOVE nor UIDPLUS, so bud won't move mail: expunging without UIDPLUS could remove other messages")
	}
}
