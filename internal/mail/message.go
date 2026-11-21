package mail

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	netmail "net/mail"
	"net/textproto"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Message parsing for the read tools, with net/mail, mime and
// mime/multipart (Suite D106). Charsets are UTF-8, US-ASCII and Latin-1;
// anything else is read as UTF-8 with bad bytes replaced, and says so

// Header is the part of a message the tools show
type Header struct {
	Date      string   `json:"date,omitempty"`
	From      []string `json:"from,omitempty"`
	To        []string `json:"to,omitempty"`
	Cc        []string `json:"cc,omitempty"`
	ReplyTo   []string `json:"reply_to,omitempty"`
	Subject   string   `json:"subject,omitempty"`
	MessageID string   `json:"message_id,omitempty"`
	InReplyTo string   `json:"in_reply_to,omitempty"`
	// References is kept for replies, and not shown
	References string `json:"-"`
}

// Attachment is named and sized only; its contents never leave the server
type Attachment struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Bytes int    `json:"bytes"`
}

// Message is a parsed message's header, readable text and attachments
type Message struct {
	Header
	Text        string       `json:"text"`
	FromHTML    bool         `json:"from_html,omitempty"` // the text was stripped from HTML
	Attachments []Attachment `json:"attachments,omitempty"`
	Notes       []string     `json:"notes,omitempty"` // what was cut or guessed
}

// MaxText caps the text a read returns
const MaxText = 64 << 10

var decoder = &mime.WordDecoder{CharsetReader: charsetReader}

func charsetReader(charset string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "utf-8", "us-ascii", "ascii":
		return r, nil
	case "iso-8859-1", "latin1", "latin-1":
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return strings.NewReader(latin1(b)), nil
	}
	return nil, fmt.Errorf("unsupported charset %q", charset)
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// decodeWords decodes RFC 2047 words, keeping the raw text if it can't
func decodeWords(s string) string {
	if d, err := decoder.DecodeHeader(s); err == nil {
		s = d
	}
	return strings.ToValidUTF8(s, "�")
}

func addresses(h netmail.Header, key string) []string {
	raw := h.Get(key)
	if raw == "" {
		return nil
	}
	p := netmail.AddressParser{WordDecoder: decoder}
	list, err := p.ParseList(raw)
	if err != nil {
		return []string{decodeWords(raw)}
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Name != "" {
			out = append(out, strings.ToValidUTF8(a.Name, "�")+" <"+a.Address+">")
		} else {
			out = append(out, a.Address)
		}
	}
	return out
}

func readHeader(h netmail.Header) Header {
	return Header{
		Date:      dateOf(h),
		From:      addresses(h, "From"),
		To:        addresses(h, "To"),
		Cc:        addresses(h, "Cc"),
		ReplyTo:   addresses(h, "Reply-To"),
		Subject:   decodeWords(h.Get("Subject")),
		MessageID: strings.TrimSpace(h.Get("Message-Id")),
		InReplyTo: strings.TrimSpace(h.Get("In-Reply-To")),
		// unfolded and one line, since a reply copies it into a header
		References: strings.Join(strings.Fields(h.Get("References")), " "),
	}
}

func dateOf(h netmail.Header) string {
	if t, err := h.Date(); err == nil {
		return t.Format(time.RFC3339)
	}
	return strings.TrimSpace(h.Get("Date"))
}

// ParseHeader reads the header fields a summary fetched
func ParseHeader(raw []byte) (Header, error) {
	if !bytes.HasSuffix(raw, []byte("\r\n\r\n")) && !bytes.HasSuffix(raw, []byte("\n\n")) {
		raw = append(append([]byte{}, raw...), "\r\n"...)
	}
	m, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Header{}, fmt.Errorf("read the message header: %w", err)
	}
	return readHeader(m.Header), nil
}

// ParseMessage reads a raw message, which may be cut short at the fetch
// cap; cut says it was
func ParseMessage(raw []byte, cut bool) (Message, error) {
	m, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Message{}, fmt.Errorf("read the message: %w", err)
	}
	msg := Message{Header: readHeader(m.Header)}
	w := &walker{msg: &msg}
	err = w.part(textproto.MIMEHeader(m.Header), m.Body, 0)
	switch {
	case cut:
		msg.Notes = append(msg.Notes, "the message is longer than bud fetches, so its end is missing")
	case err != nil:
		msg.Notes = append(msg.Notes, "part of the message couldn't be read: "+err.Error())
	}
	text, fromHTML := w.plain, false
	if text == "" && w.html != "" {
		text, fromHTML = stripHTML(w.html), true
	}
	text = strings.TrimSpace(text)
	if len(text) > MaxText {
		i := MaxText
		for i > 0 && !utf8.RuneStart(text[i]) {
			i--
		}
		text = text[:i]
		msg.Notes = append(msg.Notes, fmt.Sprintf("the text is cut at %d KiB", MaxText>>10))
	}
	msg.Text, msg.FromHTML = text, fromHTML
	msg.Notes = append(msg.Notes, w.notes...)
	return msg, nil
}

type walker struct {
	msg   *Message
	plain string // the first text/plain part
	html  string // the first text/html part
	notes []string
}

const maxParts = 100

func (w *walker) part(h textproto.MIMEHeader, body io.Reader, depth int) error {
	if depth > 8 {
		return errors.New("the parts nest more than 8 deep")
	}
	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		mt, params = "text/plain", map[string]string{"charset": "us-ascii"}
	}
	disp, dparams, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for i := 0; ; i++ {
			if i >= maxParts {
				return fmt.Errorf("more than %d parts", maxParts)
			}
			p, err := mr.NextRawPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := w.part(p.Header, p, depth+1); err != nil {
				return err
			}
		}
	}
	data, err := io.ReadAll(transferDecoder(h.Get("Content-Transfer-Encoding"), body))
	if disp == "attachment" || name != "" || (mt != "text/plain" && mt != "text/html") {
		if name == "" && mt == "message/rfc822" {
			name = "forwarded message"
		}
		w.msg.Attachments = append(w.msg.Attachments, Attachment{Name: decodeWords(name), Type: mt, Bytes: len(data)})
		return err
	}
	text := w.decodeCharset(data, params["charset"])
	switch {
	case mt == "text/html" && w.html == "":
		w.html = text
	case mt == "text/plain" && w.plain == "":
		w.plain = text
	}
	return err
}

func transferDecoder(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &noSpace{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// noSpace drops the line breaks and spaces base64 bodies carry
type noSpace struct{ r io.Reader }

func (n *noSpace) Read(p []byte) (int, error) {
	for {
		k, err := n.r.Read(p)
		j := 0
		for _, c := range p[:k] {
			if c != '\r' && c != '\n' && c != ' ' && c != '\t' {
				p[j] = c
				j++
			}
		}
		if j > 0 || err != nil {
			return j, err
		}
	}
}

func (w *walker) decodeCharset(b []byte, charset string) string {
	switch strings.ToLower(charset) {
	case "", "utf-8", "us-ascii", "ascii":
	case "iso-8859-1", "latin1", "latin-1":
		return latin1(b)
	default:
		w.notes = append(w.notes, fmt.Sprintf("the %s text is shown as UTF-8, so some characters may be wrong", charset))
	}
	return strings.ToValidUTF8(string(b), "�")
}

var (
	reHidden = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)\s*>`)
	reBreak  = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/h[1-6]|li|/li|/table)\b[^>]*>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank  = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)
	reSpaces = regexp.MustCompile(`[ \t\x{a0}]+`)
)

// stripHTML turns an HTML body into plain text: no scripts, styles or
// tags, breaks where blocks end, entities decoded
func stripHTML(s string) string {
	s = reHidden.ReplaceAllString(s, "")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = reSpaces.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return reBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
}
