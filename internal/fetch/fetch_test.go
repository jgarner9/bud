package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// opts fetches from a test server on loopback, so private targets are allowed.
func opts(url string) Options { return Options{URL: url, AllowPrivate: true, UserAgent: "brain/test"} }

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func page(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, body)
}

func TestFetchExtractsAndReportsTheFinalURL(t *testing.T) {
	var ua string
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new?x=1", http.StatusMovedPermanently)
			return
		}
		page(w, "<title>T</title><main><h1>Hello</h1><p>world</p></main><footer>no</footer>")
	})
	r, err := Fetch(context.Background(), opts(s.URL+"/old#frag"))
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != s.URL+"/new?x=1" || r.Title != "T" || r.Text != "# Hello\nworld" || r.Status != 200 || r.ContentType != "text/html" || r.Cached {
		t.Errorf("got %+v", r)
	}
	if ua != "brain/test" {
		t.Errorf("user agent %q", ua)
	}
	if r.Chars != len("# Hello\nworld") {
		t.Errorf("chars %d", r.Chars)
	}
}

func TestFetchPassesTextThroughAndRawKeepsTheSource(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "{\"a\": 1}\r\n\r\n")
		case "/latin":
			w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
			w.Write([]byte("caf\xe9"))
		case "/nosniff": // no Content-Type header at all
			w.Header()["Content-Type"] = nil
			fmt.Fprint(w, "<html><body><p>sniffed html</p></body></html>")
		default:
			page(w, "<p>a <b>b</b></p>")
		}
	})
	if r, err := Fetch(context.Background(), opts(s.URL+"/data.json")); err != nil || r.Text != `{"a": 1}` {
		t.Errorf("json: %+v, %v", r, err)
	}
	if r, err := Fetch(context.Background(), opts(s.URL+"/latin")); err != nil || r.Text != "café" {
		t.Errorf("latin-1: %+v, %v", r, err)
	}
	if r, err := Fetch(context.Background(), opts(s.URL+"/nosniff")); err != nil || r.Text != "sniffed html" {
		t.Errorf("sniffed: %+v, %v", r, err)
	}
	o := opts(s.URL + "/page")
	o.Raw = true
	if r, err := Fetch(context.Background(), o); err != nil || r.Text != "<p>a <b>b</b></p>" {
		t.Errorf("raw: %+v, %v", r, err)
	}
}

func TestFetchRefusesBinariesAndPDFs(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doc.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF-1.4 ...")
		case "/sniffed.pdf":
			w.Header()["Content-Type"] = nil
			fmt.Fprint(w, "%PDF-1.4 ...")
		default:
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n"))
		}
	})
	for path, want := range map[string]string{"/doc.pdf": "a PDF", "/sniffed.pdf": "a PDF", "/pic.png": "image/png"} {
		_, err := Fetch(context.Background(), opts(s.URL+path))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "not text") {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestFetchCutsAnOversizedBody(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		chunk := strings.Repeat("0123456789abcdef\n", 1024) // 17 KiB
		for i := 0; i < 200; i++ {                          // ~3.4 MiB
			fmt.Fprint(w, chunk)
		}
	})
	r, err := Fetch(context.Background(), opts(s.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !r.BodyCut || len(r.Text) > MaxBody {
		t.Errorf("body cut %v, %d bytes", r.BodyCut, len(r.Text))
	}
	if r.Chars < MaxBody-100 {
		t.Errorf("only %d chars kept", r.Chars)
	}
}

func TestFetchHTTPErrors(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			w.WriteHeader(403)
			return
		}
		http.NotFound(w, r)
	})
	if _, err := Fetch(context.Background(), opts(s.URL+"/missing")); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("404: %v", err)
	}
	if _, err := Fetch(context.Background(), opts(s.URL+"/blocked")); err == nil || !strings.Contains(err.Error(), "block automated clients") {
		t.Errorf("403: %v", err)
	}
}

func TestFetchRefusesPrivateTargetsByDefault(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { page(w, "secret") })
	_, err := Fetch(context.Background(), Options{URL: s.URL})
	if err == nil || !strings.Contains(err.Error(), "--allow-private") || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("loopback: %v", err)
	}
	// A name that resolves to loopback is caught after DNS, not by the URL text.
	_, err = Fetch(context.Background(), Options{URL: strings.Replace(s.URL, "127.0.0.1", "localhost", 1)})
	if err == nil || !strings.Contains(err.Error(), "--allow-private") {
		t.Errorf("localhost: %v", err)
	}
	if r, err := Fetch(context.Background(), Options{URL: s.URL, AllowPrivate: true}); err != nil || r.Text != "secret" {
		t.Errorf("allowed: %+v, %v", r, err)
	}
}

func TestFetchChecksEveryRedirectHop(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/to-metadata":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/to-ftp":
			http.Redirect(w, r, "ftp://example.com/x", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	})
	// Pretend the first hop is a public site: only loopback (the test server) is let through.
	o := Options{guard: func(a netip.Addr) error {
		if a.IsLoopback() {
			return nil
		}
		return &BlockedError{Addr: a}
	}}
	o.URL = s.URL + "/to-metadata"
	_, err := Fetch(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "a redirect to http://169.254.169.254/") || !strings.Contains(err.Error(), "private address 169.254.169.254") {
		t.Errorf("metadata redirect: %v", err)
	}
	o.URL = s.URL + "/to-ftp"
	if _, err := Fetch(context.Background(), o); err == nil || !strings.Contains(err.Error(), "ftp") {
		t.Errorf("ftp redirect: %v", err)
	}
	o.URL = s.URL + "/loop"
	hits.Store(0)
	if _, err := Fetch(context.Background(), o); err == nil || !strings.Contains(err.Error(), "5 redirects") || hits.Load() != MaxRedirects+1 {
		t.Errorf("loop: %v after %d requests", err, hits.Load())
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/a#b":      "https://example.com/a",
		"example.com/path?q=1":         "https://example.com/path?q=1",
		"  http://example.com  ":       "http://example.com",
		"HTTPS://Example.com/Page":     "https://Example.com/Page",
		"example.com/r?u=http://x.com": "https://example.com/r?u=http://x.com",
	} {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ftp://x.org/f", "file:///etc/passwd", "javascript:alert(1)", "http://", "https://u:p@example.com/", "http://exa mple.com"} {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("%q should be refused, got %q", in, got)
		}
	}
}

func TestIsPrivate(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "100.127.255.255",
		"0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "ff02::1", "198.18.0.1", "240.0.0.1", "0.1.2.3", "192.88.99.1",
		"64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "2002:7f00:1::", "2002:a9fe:a9fe::1", "64:ff9b:1::1", "2001:db8::1", "2001:2::1", "100::1"} {
		if !IsPrivate(netip.MustParseAddr(a)) {
			t.Errorf("%s should be private", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "100.63.255.255", "100.128.0.1", "2606:4700::1111", "::ffff:8.8.8.8", "64:ff9b::808:808", "2002:808:808::1"} {
		if IsPrivate(netip.MustParseAddr(a)) {
			t.Errorf("%s should be public", a)
		}
	}
}

func TestCacheHitMissAndExpiry(t *testing.T) {
	var served atomic.Int32
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		page(w, fmt.Sprintf("<p>version %d</p>", served.Add(1)))
	})
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "fetch")
	o := opts(s.URL)
	o.CacheDir = dir
	o.Now = func() time.Time { return now }

	r1, _ := Fetch(context.Background(), o)
	if r1.Cached || r1.Text != "version 1" {
		t.Fatalf("first: %+v", r1)
	}
	now = now.Add(5 * time.Minute)
	r2, _ := Fetch(context.Background(), o)
	if !r2.Cached || r2.Text != "version 1" || r2.AgeSeconds != 300 || served.Load() != 1 {
		t.Errorf("hit: %+v (served %d)", r2, served.Load())
	}

	// --raw is a different cache entry; --no-cache neither reads nor writes.
	raw := o
	raw.Raw = true
	if r, _ := Fetch(context.Background(), raw); r.Cached || served.Load() != 2 {
		t.Errorf("raw must not share the text entry: %+v", r)
	}
	fresh := o
	fresh.NoCache = true
	if r, _ := Fetch(context.Background(), fresh); r.Cached || r.Text != "version 3" {
		t.Errorf("no-cache: %+v", r)
	}
	if r, _ := Fetch(context.Background(), o); !r.Cached || r.Text != "version 1" {
		t.Errorf("no-cache must not overwrite the entry: %+v", r)
	}

	now = now.Add(CacheTTL - 5*time.Minute - time.Second) // 1s before expiry
	if r, _ := Fetch(context.Background(), o); !r.Cached {
		t.Errorf("still fresh: %+v", r)
	}
	now = now.Add(time.Second) // exactly the TTL
	r4, _ := Fetch(context.Background(), o)
	if r4.Cached || r4.Text != "version 4" {
		t.Errorf("expired: %+v", r4)
	}
	if r, _ := Fetch(context.Background(), o); !r.Cached || r.Text != "version 4" {
		t.Errorf("refetch should refresh the entry: %+v", r)
	}
}

func TestCacheFilesArePrivateAndErrorsAreNotCached(t *testing.T) {
	var fail atomic.Bool
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		page(w, "<p>ok</p>")
	})
	dir := filepath.Join(t.TempDir(), "fetch")
	o := opts(s.URL)
	o.CacheDir = dir

	fail.Store(true)
	if _, err := Fetch(context.Background(), o); err == nil {
		t.Fatal("want an error")
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Errorf("an error was cached: %v", es)
	}
	fail.Store(false)
	if _, err := Fetch(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 1 {
		t.Fatalf("cache has %d entries", len(es))
	}
	info, _ := es[0].Info()
	dinfo, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o600 || dinfo.Mode().Perm() != 0o700 {
		t.Errorf("file %v, dir %v", info.Mode().Perm(), dinfo.Mode().Perm())
	}
	if strings.Contains(es[0].Name(), "127.0.0.1") {
		t.Errorf("the key must be a hash, not the URL: %s", es[0].Name())
	}
}

func TestCacheSurvivesCorruptionAndPrunesOldFiles(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { page(w, "<p>ok</p>") })
	dir := t.TempDir()
	o := opts(s.URL)
	o.CacheDir = dir
	if _, err := Fetch(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	es, _ := os.ReadDir(dir)
	path := filepath.Join(dir, es[0].Name())
	os.WriteFile(path, []byte("{not json"), 0o600)
	if r, err := Fetch(context.Background(), o); err != nil || r.Cached || r.Text != "ok" {
		t.Errorf("corrupt entry must be a miss: %+v, %v", r, err)
	}

	old := filepath.Join(dir, "old.json")
	os.WriteFile(old, []byte("{}"), 0o600)
	when := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, when, when)
	o.NoCache = false
	o.Raw = true // a new entry, so a write happens
	if _, err := Fetch(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a file older than a day should be pruned on write")
	}
}

func TestPrivateFetchIsNotServedFromTheCacheWithoutAllowPrivate(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { page(w, "<p>internal</p>") })
	dir := t.TempDir()
	o := Options{URL: s.URL, AllowPrivate: true, CacheDir: dir}
	if _, err := Fetch(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if r, _ := Fetch(context.Background(), o); !r.Cached {
		t.Fatal("the private fetch itself should cache")
	}
	o.AllowPrivate = false
	r, err := Fetch(context.Background(), o)
	if err == nil || r.Cached || !strings.Contains(err.Error(), "--allow-private") {
		t.Errorf("cached private page leaked past the guard: %+v, %v", r, err)
	}
}
