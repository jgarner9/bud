package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, h http.HandlerFunc) string {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

func TestRunShapesDedupesAndCaps(t *testing.T) {
	var got http.Request
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(r.Context())
		fmt.Fprint(w, `{"results":[
		 {"url":"https://www.Example.com/a/?utm_source=x#top","title":"  A\n title ","content":"snippet   one\nline","engines":["ddg"]},
		 {"url":"http://example.com/a","title":"dup of A"},
		 {"url":"","title":"no url"},
		 {"url":"https://b.org/b","title":"B"},
		 {"url":"https://c.org/c","title":"C"}]}`)
	})
	res, err := Run(context.Background(), Options{BaseURL: base, Query: "go test", Max: 2, TimeRange: "week", Lang: "de", Categories: "general,news", UserAgent: "brain/test"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 || len(res.Hits) != 2 || res.Cut != 1 {
		t.Fatalf("total %d hits %d cut %d", res.Total, len(res.Hits), res.Cut)
	}
	if h := res.Hits[0]; h.N != 1 || h.Title != "A title" || h.Snippet != "snippet one line" || h.URL != "https://www.Example.com/a/?utm_source=x#top" {
		t.Errorf("hit 0 = %+v", h)
	}
	if res.Hits[1].N != 2 || res.Hits[1].URL != "https://b.org/b" {
		t.Errorf("hit 1 = %+v", res.Hits[1])
	}
	q := got.URL.Query()
	if got.URL.Path != "/search" || q.Get("q") != "go test" || q.Get("format") != "json" || q.Get("time_range") != "week" || q.Get("language") != "de" || q.Get("categories") != "general,news" {
		t.Errorf("request %s", got.URL)
	}
	if got.Header.Get("User-Agent") != "brain/test" {
		t.Errorf("user agent %q", got.Header.Get("User-Agent"))
	}
}

func TestRunMaxZeroKeepsAll(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"url":"https://a.org/1"},{"url":"https://a.org/2"},{"url":"https://a.org/3"}]}`)
	})
	res, err := Run(context.Background(), Options{BaseURL: base, Query: "x"})
	if err != nil || len(res.Hits) != 3 || res.Cut != 0 {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestRunClipsLongFields(t *testing.T) {
	long := strings.Repeat("é", 500)
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[{"url":"https://a.org/x","title":%q,"content":%q}]}`, long, long)
	})
	res, _ := Run(context.Background(), Options{BaseURL: base, Query: "x"})
	h := res.Hits[0]
	if n := len([]rune(h.Title)); n != MaxTitle || !strings.HasSuffix(h.Title, "…") {
		t.Errorf("title %d runes", n)
	}
	if n := len([]rune(h.Snippet)); n != MaxSnippet {
		t.Errorf("snippet %d runes", n)
	}
}

func TestRunErrorsSayWhatToDoNext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		h      http.HandlerFunc
		expect string
	}{
		{"json disabled", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }, "enable json in settings.yml"},
		{"html answer", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>hi</html>") }, "enable json in settings.yml"},
		{"rate limit", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }, "rate-limited"},
		{"server error", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }, "HTTP 502"},
	} {
		base := serve(t, tc.h)
		_, err := Run(context.Background(), Options{BaseURL: base, Query: "x"})
		if err == nil || !strings.Contains(err.Error(), tc.expect) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if _, err := Run(context.Background(), Options{Query: "x"}); err == nil || !strings.Contains(err.Error(), "BUD_SEARXNG_URL") {
		t.Errorf("unconfigured: %v", err)
	}
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL
	s.Close()
	_, err := Run(context.Background(), Options{BaseURL: url, Query: "secret words"})
	if err == nil || !strings.Contains(err.Error(), "reach SearXNG") || strings.Contains(err.Error(), "secret") {
		t.Errorf("unreachable (the query must not be echoed): %v", err)
	}
}

func TestProbe(t *testing.T) {
	ok := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "ping" || r.URL.Query().Get("format") != "json" {
			w.WriteHeader(400)
		}
		fmt.Fprint(w, `{"results":[]}`)
	})
	if err := Probe(context.Background(), nil, ok, "brain/test"); err != nil {
		t.Errorf("ok instance: %v", err)
	}
	bad := serve(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"nope":1}`) })
	if err := Probe(context.Background(), nil, bad, ""); err == nil {
		t.Error("an answer without results must fail")
	}
}

func TestDedupeKey(t *testing.T) {
	if dedupeKey("https://www.a.com/x/") != dedupeKey("http://A.com/x?utm_medium=y#z") {
		t.Error("spellings of one page must share a key")
	}
	if dedupeKey("https://a.com/x?id=1") == dedupeKey("https://a.com/x?id=2") {
		t.Error("real query parameters must keep pages apart")
	}
}

func TestRunNeverClipsTheURLAndRejectsHugeAnswers(t *testing.T) {
	long := "https://example.org/" + strings.Repeat("segment/", 40) + "?q=1"
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[{"url":%q,"title":"T"}]}`, long)
	})
	res, err := Run(context.Background(), Options{BaseURL: base, Query: "x"})
	if err != nil || res.Hits[0].URL != long {
		t.Fatalf("%+v, %v", res, err)
	}
	huge := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[],"pad":"`+strings.Repeat("x", MaxBody)+`"}`)
	})
	_, err = Run(context.Background(), Options{BaseURL: huge, Query: "x"})
	if err == nil || !strings.Contains(err.Error(), "larger than") || strings.Contains(err.Error(), "enable json") {
		t.Errorf("got %v", err)
	}
}
