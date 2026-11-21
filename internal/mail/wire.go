package mail

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The IMAP response grammar (RFC 9051 section 9), as much of it as the
// client reads. A parsed value is an atom, a string (quoted or literal), a
// list ([]any) or nil for NIL

// atom is a bare IMAP atom, such as a flag or a FETCH item name
type atom string

// resp is one server response line, with any literals it carried
type resp struct {
	tag    string // "*", "+" or the command's tag
	num    uint32 // the number before EXISTS, EXPUNGE or FETCH
	hasNum bool
	kind   string // upper-cased: OK, NO, BAD, BYE, PREAUTH, CAPABILITY, LIST, FETCH...
	fields []any  // the values after kind, for a data response
	code   string // a status response's [code], without the brackets
	text   string // a status response's human text
}

const (
	maxLine    = 64 << 10 // the longest atom, quoted string or text line
	maxLiteral = 1 << 20  // the biggest literal, matching read_message's fetch cap
	maxDepth   = 16       // how deeply lists may nest
)

var errLong = errors.New("the IMAP server sent a line or literal longer than bud accepts")

type reader struct {
	r *bufio.Reader
}

func (p *reader) peek() (byte, error) {
	b, err := p.r.Peek(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (p *reader) skipSP() error {
	for {
		c, err := p.peek()
		if err != nil {
			return err
		}
		if c != ' ' {
			return nil
		}
		_, _ = p.r.ReadByte()
	}
}

func (p *reader) atEOL() (bool, error) {
	c, err := p.peek()
	return c == '\r' || c == '\n', err
}

// eol consumes CRLF, or a bare LF from a sloppy server
func (p *reader) eol() error {
	c, err := p.r.ReadByte()
	if err != nil {
		return err
	}
	if c == '\r' {
		if c, err = p.r.ReadByte(); err != nil {
			return err
		}
	}
	if c != '\n' {
		return fmt.Errorf("the IMAP server sent %q where a line should end", c)
	}
	return nil
}

// restOfLine reads to the end of the line, without the line ending
func (p *reader) restOfLine() (string, error) {
	var b strings.Builder
	for {
		c, err := p.r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == '\n' {
			return strings.TrimSuffix(b.String(), "\r"), nil
		}
		if b.Len() >= maxLine {
			return "", errLong
		}
		b.WriteByte(c)
	}
}

// atom reads atom characters. A [ opens a section, as in
// BODY[HEADER.FIELDS (FROM)] or [CAPABILITY ...], which runs to its ] with
// spaces and parentheses inside
func (p *reader) atom() (string, error) {
	var b strings.Builder
	depth := 0
	for {
		c, err := p.peek()
		if err != nil {
			return "", err
		}
		if c == '\r' || c == '\n' {
			break
		}
		if depth == 0 && (c == ' ' || c == '(' || c == ')' || c == '"' || c == '{') {
			break
		}
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		}
		if b.Len() >= maxLine {
			return "", errLong
		}
		_, _ = p.r.ReadByte()
		b.WriteByte(c)
	}
	return b.String(), nil
}

func (p *reader) value(depth int) (any, error) {
	c, err := p.peek()
	if err != nil {
		return nil, err
	}
	switch c {
	case '(':
		if depth >= maxDepth {
			return nil, errors.New("the IMAP server sent lists nested too deeply")
		}
		_, _ = p.r.ReadByte()
		list := []any{}
		for {
			if err := p.skipSP(); err != nil {
				return nil, err
			}
			c, err := p.peek()
			if err != nil {
				return nil, err
			}
			if c == ')' {
				_, _ = p.r.ReadByte()
				return list, nil
			}
			if c == '\r' || c == '\n' {
				return nil, errors.New("the IMAP server ended a line inside a list")
			}
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
	case '"':
		_, _ = p.r.ReadByte()
		var b strings.Builder
		for {
			c, err := p.r.ReadByte()
			if err != nil {
				return nil, err
			}
			switch c {
			case '"':
				return b.String(), nil
			case '\\':
				if c, err = p.r.ReadByte(); err != nil {
					return nil, err
				}
			case '\r', '\n':
				return nil, errors.New("the IMAP server ended a line inside a quoted string")
			}
			if b.Len() >= maxLine {
				return nil, errLong
			}
			b.WriteByte(c)
		}
	case '{':
		_, _ = p.r.ReadByte()
		digits, err := p.r.ReadString('}')
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(digits, "}"), 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("the IMAP server sent a bad literal length {%s", digits)
		}
		if n > maxLiteral {
			return nil, errLong
		}
		if err := p.eol(); err != nil {
			return nil, err
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(p.r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil
	}
	a, err := p.atom()
	if err != nil {
		return nil, err
	}
	if a == "" {
		return nil, fmt.Errorf("the IMAP server sent %q where a value should be", c)
	}
	if strings.EqualFold(a, "NIL") {
		return nil, nil
	}
	return atom(a), nil
}

func (p *reader) readResp() (*resp, error) {
	tag, err := p.atom()
	if err != nil {
		return nil, err
	}
	if tag == "" {
		return nil, errors.New("the IMAP server sent a line with no tag")
	}
	r := &resp{tag: tag}
	if tag == "+" {
		text, err := p.restOfLine()
		r.text = strings.TrimSpace(text)
		return r, err
	}
	if err := p.skipSP(); err != nil {
		return nil, err
	}
	tok, err := p.atom()
	if err != nil {
		return nil, err
	}
	if n, err := strconv.ParseUint(tok, 10, 32); tag == "*" && err == nil {
		r.num, r.hasNum = uint32(n), true
		if err := p.skipSP(); err != nil {
			return nil, err
		}
		if tok, err = p.atom(); err != nil {
			return nil, err
		}
	}
	r.kind = strings.ToUpper(tok)
	switch r.kind {
	case "OK", "NO", "BAD", "BYE", "PREAUTH":
		text, err := p.restOfLine()
		if err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "[") {
			if i := strings.IndexByte(text, ']'); i > 0 {
				r.code, text = text[1:i], strings.TrimSpace(text[i+1:])
			}
		}
		r.text = text
		return r, nil
	}
	if tag != "*" {
		return nil, fmt.Errorf("the IMAP server answered %s with %q, not OK, NO or BAD", tag, tok)
	}
	for {
		if err := p.skipSP(); err != nil {
			return nil, err
		}
		end, err := p.atEOL()
		if err != nil {
			return nil, err
		}
		if end {
			return r, p.eol()
		}
		v, err := p.value(0)
		if err != nil {
			return nil, err
		}
		r.fields = append(r.fields, v)
	}
}

// str reads an atom or a string as text, and NIL as ""
func str(v any) (string, bool) {
	switch v := v.(type) {
	case atom:
		return string(v), true
	case string:
		return v, true
	case nil:
		return "", true
	}
	return "", false
}

func num(v any) (uint32, bool) {
	a, ok := v.(atom)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(string(a), 10, 32)
	return uint32(n), err == nil
}

// quote writes s as an IMAP quoted string. Callers check s with
// needsLiteral first
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// needsLiteral says s can't go in a quoted string: 8-bit, control
// characters or too long for one line
func needsLiteral(s string) bool {
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
