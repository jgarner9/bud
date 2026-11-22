// Package kb reads and writes bud-kb, the private knowledge base of
// Markdown pages that the curator maintains and the KB site renders (Suite
// D9, D20, D122). A page is Markdown with JSON front matter, which Hugo
// reads as it is and Go reads with the standard library. See
// docs/internals/kb.md for the format
package kb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Kinds maps a page kind to the content directory its pages live in
var Kinds = map[string]string{"project": "projects", "system": "systems"}

// Statuses a section can carry: empty while it's current
const (
	// StatusReview flags a section nothing has referenced for 6 months
	StatusReview = "review"
	// StatusRefresh flags an evergreen section due its 12-month check
	StatusRefresh = "refresh"
)

// Source is one memory record behind a fact
type Source struct {
	Record int64  `json:"record"`
	Source string `json:"source"`
	// Date is the day the record was saved, YYYY-MM-DD
	Date string `json:"date"`
}

// Fact is a curated fact: one bullet in its section, with its evidence
type Fact struct {
	Section string `json:"section"`
	Value   string `json:"value"`
	// Privacy is the most private tier among its sources
	Privacy string `json:"privacy,omitempty"`
	// Updated is the day the curator last wrote or confirmed it
	Updated string   `json:"updated"`
	Sources []Source `json:"sources,omitempty"`
}

// Section is what the curator tracks about one ## section of a page
type Section struct {
	// Referenced is the last day something referenced or confirmed it, when
	// the curator knows of one; Jason's own commits to the page count too
	Referenced string `json:"referenced,omitempty"`
	Evergreen  bool   `json:"evergreen,omitempty"`
	Status     string `json:"status,omitempty"`
}

// Meta is the front matter bud reads. Keys it doesn't know are kept
type Meta struct {
	Title string `json:"title"`
	// Kind is project or system
	Kind string `json:"kind"`
	// Linear links a project page to its Linear project, where status lives (D8)
	Linear string `json:"linear,omitempty"`
	// Evergreen pages hold long-standing facts: their sections get a refresh
	// check at 12 months instead of being archived (D39)
	Evergreen bool `json:"evergreen,omitempty"`
	// Archived is set on archive pages
	Archived bool                `json:"archived,omitempty"`
	Sections map[string]*Section `json:"sections,omitempty"`
	Facts    map[string]*Fact    `json:"facts,omitempty"`
}

// metaKeys are the front matter keys Meta owns
var metaKeys = []string{"title", "kind", "linear", "evergreen", "archived", "sections", "facts"}

// Page is one Markdown page of the KB
type Page struct {
	Meta  Meta
	Body  string
	extra map[string]json.RawMessage
}

// Parse reads a page: a JSON object, then the Markdown body
func Parse(b []byte) (*Page, error) {
	trimmed := bytes.TrimLeft(b, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("no JSON front matter: a page starts with a { ... } object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("front matter: %w", err)
	}
	front := trimmed[:dec.InputOffset()]
	p := &Page{extra: raw}
	if err := json.Unmarshal(front, &p.Meta); err != nil {
		return nil, fmt.Errorf("front matter: %w", err)
	}
	for _, k := range metaKeys {
		delete(p.extra, k)
	}
	body := trimmed[dec.InputOffset():]
	body = bytes.TrimPrefix(bytes.TrimPrefix(body, []byte("\r")), []byte("\n"))
	p.Body = strings.TrimLeft(string(body), "\n")
	if p.Meta.Sections == nil {
		p.Meta.Sections = map[string]*Section{}
	}
	if p.Meta.Facts == nil {
		p.Meta.Facts = map[string]*Fact{}
	}

	return p, nil
}

// NewPage starts an empty page
func NewPage(title, kind string) *Page {
	return &Page{Meta: Meta{Title: title, Kind: kind, Sections: map[string]*Section{}, Facts: map[string]*Fact{}}}
}

// Bytes writes the page back: sorted front matter, a blank line, the body
func (p *Page) Bytes() ([]byte, error) {
	known, err := json.Marshal(p.Meta)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(known, &fields); err != nil {
		return nil, err
	}
	for k, v := range p.extra {
		fields[k] = v
	}
	front, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Write(front)
	out.WriteString("\n\n")
	out.WriteString(strings.TrimRight(p.Body, "\n"))
	out.WriteString("\n")

	return out.Bytes(), nil
}

// Slug makes a heading's anchor the way Hugo's default ("github") does:
// lowercase, spaces to hyphens, punctuation dropped
func Slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}

	return b.String()
}

// BodySection is one ## section of a page body, by line
type BodySection struct {
	Slug  string
	Title string
	// Start is the heading's line and End the line after the section's last
	Start, End int
}

// lines splits a body into lines without their newlines
func lines(body string) []string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil
	}

	return strings.Split(body, "\n")
}

// Sections lists the body's ## sections in order, skipping fenced code
func (p *Page) Sections() []BodySection {
	var out []BodySection
	fence := false
	ls := lines(p.Body)
	for i, l := range ls {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if strings.HasPrefix(l, "## ") || strings.HasPrefix(l, "# ") {
			if n := len(out); n > 0 {
				out[n-1].End = i
			}
			if strings.HasPrefix(l, "## ") {
				title := strings.TrimSpace(strings.TrimPrefix(l, "## "))
				out = append(out, BodySection{Slug: Slug(title), Title: title, Start: i, End: len(ls)})
			}
		}
	}

	return out
}

// Section finds a section by slug
func (p *Page) Section(slug string) (BodySection, bool) {
	for _, s := range p.Sections() {
		if s.Slug == slug {
			return s, true
		}
	}

	return BodySection{}, false
}

// marker ends a curated fact's bullet; Hugo drops raw HTML, so it never shows
func marker(key string) string { return "<!-- fact:" + key + " -->" }

// SetFact writes a fact's bullet: in place when the page has it, else at
// the end of its section, which is added (titled title) when missing
func (p *Page) SetFact(key, statement, section, title string) {
	bullet := "- " + oneLine(statement) + " " + marker(key)
	ls := lines(p.Body)
	for i, l := range ls {
		if strings.HasSuffix(strings.TrimSpace(l), marker(key)) {
			ls[i] = bullet
			p.Body = strings.Join(ls, "\n") + "\n"
			return
		}
	}
	s, ok := p.Section(section)
	if !ok {
		if len(ls) > 0 {
			ls = append(ls, "")
		}
		ls = append(ls, "## "+title, "", bullet)
		p.Body = strings.Join(ls, "\n") + "\n"
		return
	}
	at := s.End
	for at > s.Start+1 && strings.TrimSpace(ls[at-1]) == "" {
		at--
	}
	if at == s.Start+1 {
		// only the heading so far: keep a blank line under it
		ls = slices.Insert(ls, at, "", bullet)
	} else {
		ls = slices.Insert(ls, at, bullet)
	}
	p.Body = strings.Join(ls, "\n") + "\n"
}

// FactLine reports the bullet of a fact, without its marker
func (p *Page) FactLine(key string) (string, bool) {
	for _, l := range lines(p.Body) {
		t := strings.TrimSpace(l)
		if strings.HasSuffix(t, marker(key)) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(t, marker(key)), "- ")), true
		}
	}

	return "", false
}

// CutSection removes a section from the body and returns its text
func (p *Page) CutSection(slug string) (string, bool) {
	s, ok := p.Section(slug)
	if !ok {
		return "", false
	}
	ls := lines(p.Body)
	cut := strings.TrimRight(strings.Join(ls[s.Start:s.End], "\n"), "\n") + "\n"
	rest := append(slices.Clone(ls[:s.Start]), ls[s.End:]...)
	p.Body = strings.TrimRight(strings.Join(rest, "\n"), "\n") + "\n"

	return cut, true
}

// AppendSection adds section text at the end of the body
func (p *Page) AppendSection(text string) {
	body := strings.TrimRight(p.Body, "\n")
	if body != "" {
		body += "\n\n"
	}
	p.Body = body + strings.TrimRight(text, "\n") + "\n"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// keyRe is a fact key: <projects|systems>/<page>#<section>/<name>
var keyRe = regexp.MustCompile(`^(projects|systems)/([a-z0-9][a-z0-9-]*)#([a-z0-9][a-z0-9-]*)/([a-z0-9][a-z0-9-]*)$`)

// Key names one fact in the KB
type Key struct {
	Dir     string // projects or systems
	Page    string // the page's slug
	Section string // the section's slug
	Name    string
}

// ParseKey reads a fact key such as systems/archie#router/port
func ParseKey(s string) (Key, error) {
	m := keyRe.FindStringSubmatch(s)
	if m == nil {
		return Key{}, fmt.Errorf("fact key %q isn't <projects|systems>/<page>#<section>/<name> in lowercase letters, digits and '-'", s)
	}

	return Key{Dir: m[1], Page: m[2], Section: m[3], Name: m[4]}, nil
}

func (k Key) String() string { return k.Dir + "/" + k.Page + "#" + k.Section + "/" + k.Name }

// Path is the page's file in the KB
func (k Key) Path() string { return PagePath(k.Dir, k.Page) }

// Local is the fact's key inside its page's front matter
func (k Key) Local() string { return k.Section + "/" + k.Name }

// Kind is the page kind its directory holds
func (k Key) Kind() string {
	for kind, dir := range Kinds {
		if dir == k.Dir {
			return kind
		}
	}

	return ""
}

// PagePath is the file of the page slug under dir (projects or systems)
func PagePath(dir, slug string) string { return "content/" + dir + "/" + slug + ".md" }

// ArchivePath is where a page's archived sections go
func ArchivePath(dir, slug string) string { return "content/archive/" + dir + "/" + slug + ".md" }

// readAll is io.ReadAll bounded, so one huge file can't fill memory
func readAll(r io.Reader) ([]byte, error) {
	const max = 4 << 20
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err == nil && len(b) > max {
		err = errors.New("larger than 4 MiB")
	}

	return b, err
}
