package fetch

import (
	"os"
	"strings"
	"testing"
)

func TestExtractArticleFixture(t *testing.T) {
	src, err := os.ReadFile("testdata/article.html")
	if err != nil {
		t.Fatal(err)
	}
	title, text := ExtractHTML(string(src))
	if title != "The & Great Page" {
		t.Errorf("title %q", title)
	}
	want := strings.Join([]string{
		"# Getting started",
		"First boldtext, a link and an entity <tag> © 2026. Second line of the same paragraph.",
		"Line one",
		"Line two",
		"Line three",
		"## Steps",
		"1. Install it",
		"2. Run it",
		"  - with flags",
		"  - without",
		"3. Wrapped item",
		"- plain",
		"- unclosed items",
		"func main() {",
		"\tfmt.Println(\"hi\")",
		"",
		"\treturn",
		"}",
		"Name | Value",
		"alpha | 1",
		"beta two | 2",
	}, "\n")
	if text != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", text, want)
	}
}

func TestExtractWithoutMainDropsBoilerplateByTag(t *testing.T) {
	_, text := ExtractHTML(`<body><nav>menu</nav><div>keep <span>this</span></div><footer>foot</footer><aside>side</aside></body>`)
	if text != "keep this" {
		t.Errorf("got %q", text)
	}
}

func TestExtractPrefersArticleWhenThereIsNoMain(t *testing.T) {
	_, text := ExtractHTML(`<body><div>outside</div><article><p>inside</p><article><p>nested</p></article><p>after</p></article><div>tail</div></body>`)
	if text != "inside\nnested\nafter" {
		t.Errorf("got %q", text)
	}
}

func TestExtractFallsBackToTheWholePageWhenMainIsEmpty(t *testing.T) {
	_, text := ExtractHTML(`<body><p>real content</p><main><div id="app"></div></main></body>`)
	if text != "real content" {
		t.Errorf("got %q", text)
	}
}

func TestExtractKeepsFormsWhenTheWholePageIsOneForm(t *testing.T) {
	_, text := ExtractHTML(`<body><form id="aspnet"><h1>Everything</h1><p>is in a form</p></form></body>`)
	if text != "# Everything\nis in a form" {
		t.Errorf("got %q", text)
	}
}

func TestExtractSurvivesMalformedHTML(t *testing.T) {
	for _, src := range []string{
		"", "plain text only", "<p>unclosed <b>tags", "<div <p>weird</div>", "a < b and c > d",
		"<script>never closed", "<!-- never closed", "<p attr=\"never closed>text", "<![CDATA[x]]><?php echo 1 ?><p>after</p>",
		"</p></div></li>stray ends", "<ul><li>a</ul></ul></ul><li>b",
	} {
		ExtractHTML(src) // must not panic or hang
	}
	if _, text := ExtractHTML("a < b and c > d"); text != "a < b and c > d" {
		t.Errorf("literal < kept as text: %q", text)
	}
	if _, text := ExtractHTML("<p>one</p><script>never closed <p>x</p>"); text != "one" {
		t.Errorf("an unclosed script drops the rest of the page only: %q", text)
	}
	if _, text := ExtractHTML("<![CDATA[x]]><?php echo 1 ?><p>after</p>"); text != "after" {
		t.Errorf("got %q", text)
	}
	if _, text := ExtractHTML("<nav>menu<div>content</div>"); text != "menu\ncontent" {
		t.Errorf("unclosed nav swallowed the page: %q", text)
	}
}

func TestExtractIsCaseInsensitiveAndKeepsInlineSpacing(t *testing.T) {
	_, text := ExtractHTML(`<P>A<B>b</B> <I>c</I>d</P><SCRIPT>x</SCRIPT><H3>Head</H3>`)
	if text != "Ab cd\n### Head" {
		t.Errorf("got %q", text)
	}
}

func TestTokenizerEdgeCases(t *testing.T) {
	// non-ASCII before the end tag of a script (a lowercased copy changes length)
	if _, text := ExtractHTML("<script>var s = 'İİİİ';</script><p>after</p>"); text != "after" {
		t.Errorf("script with non-ASCII: %q", text)
	}
	if _, text := ExtractHTML("<SCRIPT>x</ScRiPt><p>after</p>"); text != "after" {
		t.Errorf("mixed-case script end: %q", text)
	}
	if _, text := ExtractHTML("<script></scriptx><p>hidden</p></script><p>after</p>"); text != "after" {
		t.Errorf("scriptx was treated as a script end tag: %q", text)
	}
	// an unterminated quote ends the tag at the first '>' instead of dropping the page
	if _, text := ExtractHTML(`<p>one</p><a href="broken>link</a><p>two</p>`); !strings.Contains(text, "two") {
		t.Errorf("unterminated quote lost the rest of the page: %q", text)
	}
	// "/" at the end of an unquoted value is not a self-closing slash
	if _, text := ExtractHTML(`<div><a href=http://x.com/>link</a> text</div>`); text != "link text" {
		t.Errorf("unquoted href: %q", text)
	}
	// a real self-closing slash still works, and "/" never closes a dropped element
	if _, text := ExtractHTML(`<p>a<br/>b<br />c</p>`); text != "a\nb\nc" {
		t.Errorf("br: %q", text)
	}
	if _, text := ExtractHTML(`<nav/><p>hidden</p></nav><p>shown</p>`); text != "shown" {
		t.Errorf("nav/ must open a dropped element: %q", text)
	}
	// raw-text search is linear: a long run of '<' must not stall
	ExtractHTML("<script>" + strings.Repeat("<", 200000))
}
