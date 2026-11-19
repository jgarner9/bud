package fetch

import (
	"html"
	"strconv"
	"strings"
	"unicode"
)

// This file is the readable-text extractor. The standard library has no HTML
// parser (D19 rules out golang.org/x/net/html), so it is a small tokenizer
// plus a walker. Limits, on purpose:
//
//   - It is not a browser. There is no DOM, no CSS and no JavaScript: text a
//     script writes into the page, or CSS hides, is not treated specially.
//   - Boilerplate is dropped by tag name only (nav, footer, aside, form,
//     script, style...), never by class names or scoring. A site that puts
//     its menu in a plain <div> keeps the menu.
//   - An element in the drop list that is never closed swallows the rest of
//     the page; if that leaves nothing, the walk is retried without it.
//   - Links, images and their URLs are not kept: only link text and nothing
//     of an image. Frames, SVG and canvas contribute nothing.
//   - Tables become one line per row with cells joined by " | ". Nested tables
//     are flattened into their row.
//   - Character sets other than UTF-8 and Latin-1 are not converted.

type tokenKind int

const (
	tokText tokenKind = iota
	tokStart
	tokEnd
)

type token struct {
	kind tokenKind
	name string // lower-case tag name
	data string // the text of a tokText
	self bool   // <x/>
}

// tokenize splits src into text, start and end tags. Comments, doctypes and
// processing instructions are dropped, and so is the content of <script> and
// <style>, which may contain "<".
func tokenize(src string) []token {
	var toks []token
	i := 0
	for i < len(src) {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			toks = append(toks, token{kind: tokText, data: src[i:]})
			break
		}
		if lt > 0 {
			toks = append(toks, token{kind: tokText, data: src[i : i+lt]})
		}
		i += lt
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				return toks
			}
			i += 4 + end + 3
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return toks
			}
			i += end + 1
		case len(rest) > 2 && rest[1] == '/' && isLetter(rest[2]):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return toks
			}
			toks = append(toks, token{kind: tokEnd, name: tagName(rest[2:end])})
			i += end + 1
		case len(rest) > 1 && isLetter(rest[1]):
			end := tagEnd(rest)
			if end < 0 {
				return toks
			}
			inner := rest[1:end]
			t := token{kind: tokStart, name: tagName(inner), self: selfClosing(inner)}
			toks = append(toks, t)
			i += end + 1
			if t.name == "script" || t.name == "style" { // HTML ignores "/" on these
				i += rawTextLen(src[i:], t.name)
			}
		default:
			toks = append(toks, token{kind: tokText, data: "<"})
			i++
		}
	}
	return toks
}

// selfClosing reports whether the inside of a tag ends in a "/>" that closes
// it: after the name, whitespace or a closing quote. The slash of an unquoted
// value (<a href=http://x.com/>) is part of the value.
func selfClosing(inner string) bool {
	if !strings.HasSuffix(inner, "/") {
		return false
	}
	body := inner[:len(inner)-1]
	if body == tagName(inner) {
		return true
	}
	switch body[len(body)-1] {
	case ' ', '\t', '\r', '\n', '\f', '"', '\'':
		return true
	}
	return false
}

func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

// tagName is the lower-case name at the start of s ("div class=x" -> "div").
func tagName(s string) string {
	end := strings.IndexAny(s, " \t\r\n\f/")
	if end < 0 {
		end = len(s)
	}
	return strings.ToLower(s[:end])
}

// tagEnd is the index of the '>' that closes the tag at the start of s,
// skipping any '>' inside quoted attribute values. If a quote never closes,
// the tag ends at the first '>' instead; -1 if there is none.
func tagEnd(s string) int {
	var quote byte
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
	}
	return strings.IndexByte(s, '>')
}

// rawTextLen is how many bytes of s are the body of a raw-text element, up to
// (not including) its end tag.
func rawTextLen(s, name string) int {
	for i := 0; i < len(s); {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		if end := i + 2 + len(name); end <= len(s) && s[i+1] == '/' && strings.EqualFold(s[i+2:end], name) {
			return i
		}
		i++
	}
	return len(s)
}

var (
	void = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
		"img": true, "input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true}

	// dropped with everything inside them.
	dropped = map[string]bool{"script": true, "style": true, "noscript": true, "template": true, "nav": true,
		"footer": true, "aside": true, "form": true, "svg": true, "iframe": true, "title": true,
		"select": true, "button": true, "object": true, "canvas": true}

	// start a new line.
	block = map[string]bool{"p": true, "div": true, "section": true, "article": true, "main": true, "header": true,
		"blockquote": true, "dl": true, "dt": true, "dd": true, "figure": true, "figcaption": true,
		"details": true, "summary": true, "address": true, "fieldset": true, "legend": true,
		"caption": true, "body": true, "html": true, "hgroup": true, "menu": true, "center": true}
)

// ExtractHTML returns the page's title and its readable text, one block per
// line: headings as "# Title", list items as "- item" or "1. item", table rows
// as "a | b", and code blocks as they are.
func ExtractHTML(src string) (title, text string) {
	toks := tokenize(src)
	title = pageTitle(toks)
	type attempt struct {
		toks      []token
		keepForms bool
	}
	var attempts []attempt
	for _, name := range []string{"main", "article"} { // the main content, if the page says where it is
		if lo, hi, ok := innerRange(toks, name); ok {
			attempts = append(attempts, attempt{toks[lo:hi], false})
			break
		}
	}
	// then the whole page; then again with forms kept, for pages that wrap
	// everything in one <form>
	attempts = append(attempts, attempt{toks, false}, attempt{toks, true})
	for _, a := range attempts {
		if text = walk(a.toks, a.keepForms); text != "" {
			break
		}
	}
	return title, text
}

func pageTitle(toks []token) string {
	for i, t := range toks {
		if t.kind == tokStart && t.name == "title" {
			var b strings.Builder
			for _, u := range toks[i+1:] {
				if u.kind == tokText {
					b.WriteString(u.data)
				} else if u.kind == tokEnd && u.name == "title" {
					break
				}
			}
			return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
		}
	}
	return ""
}

// innerRange finds the first <name> element and returns the token range
// inside it; an element that is never closed runs to the end.
func innerRange(toks []token, name string) (lo, hi int, ok bool) {
	for i, t := range toks {
		if t.kind != tokStart || t.name != name {
			continue
		}
		depth := 1
		for j := i + 1; j < len(toks); j++ {
			switch {
			case toks[j].kind == tokStart && toks[j].name == name && !toks[j].self:
				depth++
			case toks[j].kind == tokEnd && toks[j].name == name:
				if depth--; depth == 0 {
					return i + 1, j, true
				}
			}
		}
		return i + 1, len(toks), true
	}
	return 0, 0, false
}

type listState struct {
	ordered bool
	n       int
}

type walker struct {
	lines  []string
	cur    strings.Builder
	space  bool // whitespace is pending before the next word
	prefix string

	skip      string // name of the dropped element being skipped
	skipDepth int

	lists  []listState
	inPre  bool
	pre    []string // lines of the code block being read
	preCur strings.Builder
	inCell bool
	cells  []string
}

func walk(toks []token, keepForms bool) string {
	w := &walker{}
	for _, t := range toks {
		if w.skip != "" {
			switch {
			case t.kind == tokStart && t.name == w.skip:
				w.skipDepth++
			case t.kind == tokEnd && t.name == w.skip:
				if w.skipDepth--; w.skipDepth == 0 {
					w.skip = ""
				}
			}
			continue
		}
		switch t.kind {
		case tokText:
			w.text(t.data)
		case tokStart:
			if dropped[t.name] && !(keepForms && t.name == "form") {
				w.flush()
				if !void[t.name] { // a "/" does not close a dropped element in HTML
					w.skip, w.skipDepth = t.name, 1
				}
				continue
			}
			w.start(t.name)
		case tokEnd:
			w.end(t.name)
		}
	}
	w.closePre()
	w.rowEnd()
	w.flush()
	return strings.Join(w.lines, "\n")
}

func headingLevel(name string) int {
	if len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' {
		return int(name[1] - '0')
	}
	return 0
}

func (w *walker) start(name string) {
	switch {
	case name == "br" && w.inPre:
		w.preText("\n")
	case name == "br" || name == "hr":
		w.flush()
	case name == "pre":
		w.flush()
		w.closePre()
		w.inPre = true
	case name == "ul" || name == "ol":
		w.flush()
		w.lists = append(w.lists, listState{ordered: name == "ol"})
	case name == "li":
		w.flush()
		indent := ""
		if len(w.lists) > 1 {
			indent = strings.Repeat("  ", len(w.lists)-1)
		}
		if n := len(w.lists); n > 0 && w.lists[n-1].ordered {
			w.lists[n-1].n++
			w.prefix = indent + strconv.Itoa(w.lists[n-1].n) + ". "
		} else {
			w.prefix = indent + "- "
		}
	case headingLevel(name) > 0:
		w.flush()
		w.prefix = strings.Repeat("#", headingLevel(name)) + " "
	case name == "table" || name == "tr":
		w.flush()
		w.rowEnd()
	case name == "td" || name == "th":
		if w.inCell {
			w.cellEnd()
		}
		w.inCell = true
	case block[name]:
		w.flush()
	}
}

func (w *walker) end(name string) {
	switch {
	case name == "pre":
		w.closePre()
	case name == "ul" || name == "ol":
		w.flush()
		if n := len(w.lists); n > 0 {
			w.lists = w.lists[:n-1]
		}
	case name == "li" || headingLevel(name) > 0:
		w.flush()
		w.prefix = ""
	case name == "td" || name == "th":
		if w.inCell {
			w.cellEnd()
		}
	case name == "tr" || name == "table":
		w.rowEnd()
	case block[name]:
		w.flush()
	}
}

// text adds a run of text from the page. Outside code blocks whitespace
// collapses to single spaces; a line never starts with one.
func (w *walker) text(raw string) {
	s := html.UnescapeString(raw)
	if w.inPre {
		w.preText(s)
		return
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		if s != "" && w.cur.Len() > 0 {
			w.space = true
		}
		return
	}
	if first := []rune(s)[0]; unicode.IsSpace(first) && w.cur.Len() > 0 {
		w.space = true
	}
	for _, word := range words {
		if w.cur.Len() > 0 && w.space {
			w.cur.WriteByte(' ')
		}
		w.cur.WriteString(word)
		w.space = true
	}
	rs := []rune(s)
	w.space = unicode.IsSpace(rs[len(rs)-1])
}

// flush ends the current line. Inside a table cell it only separates words,
// so a cell stays one piece.
func (w *walker) flush() {
	if w.inCell {
		w.space = w.cur.Len() > 0
		return
	}
	line := w.cur.String()
	w.cur.Reset()
	w.space = false
	if line == "" {
		return
	}
	w.lines = append(w.lines, w.prefix+line)
	w.prefix = ""
}

func (w *walker) cellEnd() {
	if cell := w.cur.String(); cell != "" {
		w.cells = append(w.cells, cell)
	}
	w.cur.Reset()
	w.space = false
	w.inCell = false
}

func (w *walker) rowEnd() {
	if w.inCell {
		w.cellEnd()
	}
	if len(w.cells) > 0 {
		w.lines = append(w.lines, strings.Join(w.cells, " | "))
		w.cells = nil
	}
}

func (w *walker) preText(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if len(w.pre) == 0 && w.preCur.Len() == 0 {
		s = strings.TrimPrefix(s, "\n") // the newline right after <pre> is not content
	}
	for {
		nl := strings.IndexByte(s, '\n')
		if nl < 0 {
			w.preCur.WriteString(s)
			return
		}
		w.preCur.WriteString(s[:nl])
		w.pre = append(w.pre, strings.TrimRight(w.preCur.String(), " \t"))
		w.preCur.Reset()
		s = s[nl+1:]
	}
}

// closePre writes a finished code block: blank lines at its edges removed and
// runs of blank lines inside it cut to one.
func (w *walker) closePre() {
	if !w.inPre {
		return
	}
	w.inPre = false
	if w.preCur.Len() > 0 {
		w.pre = append(w.pre, strings.TrimRight(w.preCur.String(), " \t"))
		w.preCur.Reset()
	}
	blank := false
	started := false
	for _, l := range w.pre {
		if l == "" {
			blank = started
			continue
		}
		if blank {
			w.lines = append(w.lines, "")
			blank = false
		}
		started = true
		w.lines = append(w.lines, l)
	}
	w.pre = nil
}
