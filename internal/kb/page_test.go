package kb

import (
	"strings"
	"testing"
)

const samplePage = `{
  "title": "archie",
  "kind": "system",
  "weight": 3,
  "sections": {"router": {"referenced": "2026-11-01"}}
}

Intro written by hand.

## Router

- The router listens on 8795. <!-- fact:router/port -->

` + "```sh\n## not a heading\n```" + `

## Mail setup

Prose.
`

func TestParseKeepsUnknownKeysAndRoundTrips(t *testing.T) {
	p, err := Parse([]byte(samplePage))
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.Title != "archie" || p.Meta.Kind != "system" || p.Meta.Sections["router"].Referenced != "2026-11-01" {
		t.Fatalf("meta = %+v", p.Meta)
	}
	b, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"weight": 3`) {
		t.Errorf("an unknown key was dropped:\n%s", b)
	}
	again, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := again.Bytes()
	if string(b) != string(b2) {
		t.Errorf("not stable:\n%s\n---\n%s", b, b2)
	}
	if !strings.HasPrefix(again.Body, "Intro written by hand.") {
		t.Errorf("body = %q", again.Body)
	}
}

func TestParseRefusesNoFrontMatter(t *testing.T) {
	if _, err := Parse([]byte("# just markdown\n")); err == nil {
		t.Fatal("no error for a page without front matter")
	}
}

func TestSectionsSkipFencedCode(t *testing.T) {
	p, _ := Parse([]byte(samplePage))
	var slugs []string
	for _, s := range p.Sections() {
		slugs = append(slugs, s.Slug)
	}
	if strings.Join(slugs, ",") != "router,mail-setup" {
		t.Errorf("sections = %v", slugs)
	}
}

func TestSetFact(t *testing.T) {
	p, _ := Parse([]byte(samplePage))

	p.SetFact("router/port", "The router listens on 8800.", "router", "Router")
	if got, _ := p.FactLine("router/port"); got != "The router listens on 8800." {
		t.Errorf("in place: %q", got)
	}
	if strings.Contains(p.Body, "8795") {
		t.Errorf("the old bullet stayed:\n%s", p.Body)
	}

	p.SetFact("router/unit", "It runs as bud-router.service.", "router", "Router")
	s, _ := p.Section("router")
	sec := strings.Join(lines(p.Body)[s.Start:s.End], "\n")
	if !strings.Contains(sec, "bud-router.service") {
		t.Errorf("not added to its section:\n%s", sec)
	}

	p.SetFact("backups/tool", "Backups use restic.", "backups", "Backups")
	if !strings.HasSuffix(p.Body, "## Backups\n\n- Backups use restic. <!-- fact:backups/tool -->\n") {
		t.Errorf("new section:\n%s", p.Body)
	}
}

func TestCutAndAppendSection(t *testing.T) {
	p, _ := Parse([]byte(samplePage))
	text, ok := p.CutSection("router")
	if !ok || !strings.HasPrefix(text, "## Router\n") || !strings.Contains(text, "not a heading") {
		t.Fatalf("cut = %q", text)
	}
	if strings.Contains(p.Body, "## Router") || !strings.Contains(p.Body, "## Mail setup") {
		t.Errorf("body after the cut:\n%s", p.Body)
	}
	q := NewPage("archie (archived)", "system")
	q.AppendSection(text)
	if _, ok := q.Section("router"); !ok {
		t.Errorf("archive lacks the section:\n%s", q.Body)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Router": "router", "Mail setup": "mail-setup", "Q&A: ports!": "qa-ports", "bud_kb": "bud_kb"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseKey(t *testing.T) {
	k, err := ParseKey("systems/archie#router/port")
	if err != nil {
		t.Fatal(err)
	}
	if k.Path() != "content/systems/archie.md" || k.Local() != "router/port" || k.Kind() != "system" || k.String() != "systems/archie#router/port" {
		t.Errorf("key = %+v", k)
	}
	for _, bad := range []string{"archie#router/port", "systems/Archie#router/port", "systems/../x#a/b", "notes/x#a/b", "systems/x#a"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) took it", bad)
		}
	}
}
