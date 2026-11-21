package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"
	"unicode/utf8"
)

// Sends over net/smtp with STARTTLS (Suite D106). Every send is plain text,
// with no attachments, and its arguments are the whole message, so an
// approval covers exactly what goes out (D103, D107)

const (
	// MaxBody is the longest body a send takes, so the whole message fits
	// the Signal prompt (D107)
	MaxBody = 4000
	// MaxRecipients caps To and Cc together
	MaxRecipients = 10
	maxSubject    = 300
)

// Outgoing is one message to send
type Outgoing struct {
	To, Cc     []*netmail.Address
	Subject    string
	Body       string
	InReplyTo  string
	References string
}

// Sender sends as one account over SMTP
type Sender struct {
	addr string
	host string
	user string
	pass string
	from *netmail.Address
	tls  bool // STARTTLS is required; false only for a loopback host (tests)
	now  func() time.Time
}

// NewSender checks the SMTP address, credentials and From address
func NewSender(addr, user, pass, from string) (*Sender, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("--smtp %q isn't host:port, such as smtp.mail.me.com:587", addr)
	}
	if user == "" || pass == "" {
		return nil, errors.New("no mail credentials: set BUD_MAIL_USER and BUD_MAIL_PASSWORD (op run fills them from the Bud vault)")
	}
	if from == "" {
		return nil, errors.New("no From address: set BUD_MAIL_FROM to the iCloud address mail goes out from")
	}
	f, err := netmail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("BUD_MAIL_FROM %q isn't an address: %w", from, err)
	}
	s := &Sender{addr: addr, host: host, user: user, pass: pass, from: f, tls: true, now: time.Now}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		s.tls = false
	}
	return s, nil
}

// From is the address sends come from
func (s *Sender) From() string { return s.from.Address }

// ParseRecipients reads addresses for a send, refusing line breaks and
// anything net/mail can't parse
func ParseRecipients(field string, list []string) ([]*netmail.Address, error) {
	var out []*netmail.Address
	for _, v := range list {
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("%s: %q has a line break", field, v)
		}
		a, err := netmail.ParseAddress(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q isn't an address such as sam@example.com or Sam Lee <sam@example.com>", field, v)
		}
		out = append(out, a)
	}
	return out, nil
}

// Check says what's wrong with a message before it's sent
func (m Outgoing) Check() error {
	switch {
	case len(m.To) == 0:
		return errors.New("to: give at least one recipient")
	case len(m.To)+len(m.Cc) > MaxRecipients:
		return fmt.Errorf("send to at most %d people at once", MaxRecipients)
	case strings.TrimSpace(m.Subject) == "":
		return errors.New("subject: give the message a subject")
	case strings.ContainsAny(m.Subject, "\r\n"):
		return errors.New("subject: keep it to one line")
	case utf8.RuneCountInString(m.Subject) > maxSubject:
		return fmt.Errorf("subject: keep it under %d characters", maxSubject)
	case strings.TrimSpace(m.Body) == "":
		return errors.New("body: the message is empty")
	case utf8.RuneCountInString(m.Body) > MaxBody:
		return fmt.Errorf("body: keep it to %d characters, so Jason sees all of it when he approves", MaxBody)
	case !utf8.ValidString(m.Subject) || !utf8.ValidString(m.Body):
		return errors.New("the subject or body isn't valid UTF-8")
	case strings.ContainsAny(m.InReplyTo+m.References, "\r\n"):
		return errors.New("the reply headers have a line break")
	}
	return nil
}

// build writes the RFC 5322 message
func (s *Sender) build(m Outgoing, id string, date time.Time) ([]byte, error) {
	var b bytes.Buffer
	join := func(list []*netmail.Address) string {
		out := make([]string, len(list))
		for i, a := range list {
			out[i] = a.String()
		}
		return strings.Join(out, ", ")
	}
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", s.from.String())
	hdr("To", join(m.To))
	if len(m.Cc) > 0 {
		hdr("Cc", join(m.Cc))
	}
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", date.Format(time.RFC1123Z))
	hdr("Message-ID", id)
	if m.InReplyTo != "" {
		hdr("In-Reply-To", m.InReplyTo)
	}
	if m.References != "" {
		hdr("References", m.References)
	}
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	body := strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	b.WriteString("\r\n")
	return b.Bytes(), nil
}

func (s *Sender) newID() (string, error) {
	var r [12]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", err
	}
	domain := s.from.Address[strings.LastIndexByte(s.from.Address, '@')+1:]
	return "<" + hex.EncodeToString(r[:]) + "@" + domain + ">", nil
}

// Send sends one message and returns its Message-ID
func (s *Sender) Send(ctx context.Context, m Outgoing) (string, error) {
	if err := m.Check(); err != nil {
		return "", err
	}
	id, err := s.newID()
	if err != nil {
		return "", err
	}
	data, err := s.build(m, id, s.now())
	if err != nil {
		return "", err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return "", fmt.Errorf("connect to the SMTP server %s: %w", s.addr, err)
	}
	dl, _ := ctx.Deadline()
	_ = conn.SetDeadline(dl)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("start SMTP with %s: %w", s.addr, err)
	}
	defer c.Close()
	if err := c.Hello("localhost"); err != nil {
		return "", fmt.Errorf("SMTP hello: %w", err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return "", fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	} else if s.tls {
		return "", errors.New("refusing to send: the SMTP server doesn't offer STARTTLS, so the password would go in the clear")
	}
	if err := c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
		return "", fmt.Errorf("iCloud refused the SMTP login (check BUD_MAIL_USER and the app-specific password): %w", err)
	}
	if err := c.Mail(s.from.Address); err != nil {
		return "", fmt.Errorf("SMTP MAIL FROM %s: %w", s.from.Address, err)
	}
	for _, a := range append(append([]*netmail.Address{}, m.To...), m.Cc...) {
		if err := c.Rcpt(a.Address); err != nil {
			return "", fmt.Errorf("the SMTP server refused the recipient %s, so nothing was sent: %w", a.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return "", fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("send the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("the SMTP server didn't accept the message: %w", err)
	}
	_ = c.Quit()
	return id, nil
}
