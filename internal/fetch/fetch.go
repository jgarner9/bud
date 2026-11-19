// Package fetch is the engine behind `bud fetch`: it downloads one page
// safely, reduces it to readable text and keeps the result on disk for a
// while. Whatever it returns is untrusted text from the internet: nothing
// here executes it, and callers must treat it as data.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"jgarner.dev/bud/internal/atomicfile"
)

// Limits.
const (
	CacheTTL     = 15 * time.Minute // how long a fetched page is served from disk
	Timeout      = 20 * time.Second // the whole request, body included
	MaxBody      = 2 << 20          // bytes read from the server; the rest is dropped
	MaxRedirects = 5
	pruneAfter   = 24 * time.Hour // cache files older than this are deleted on write
)

// Options describe one fetch.
type Options struct {
	URL          string
	Raw          bool // keep the body as text instead of extracting readable text
	NoCache      bool // neither read nor write the cache
	AllowPrivate bool // allow loopback, private, link-local and similar targets
	UserAgent    string
	CacheDir     string           // empty means no cache
	Now          func() time.Time // nil means time.Now
	Client       *http.Client     // nil means one built with the private-address guard

	guard func(netip.Addr) error // replaces the default guard in tests
}

// Result is a fetched page.
type Result struct {
	URL         string    `json:"url"` // after redirects
	Title       string    `json:"title,omitempty"`
	ContentType string    `json:"content_type"`
	Status      int       `json:"status"`
	Text        string    `json:"text"`
	Chars       int       `json:"chars"`
	BodyCut     bool      `json:"body_cut"` // the server sent more than MaxBody
	FetchedAt   time.Time `json:"fetched_at"`
	Cached      bool      `json:"cached"`
	AgeSeconds  int       `json:"age_seconds,omitempty"` // when Cached
}

// BlockedError is returned (inside the transport's error) when a connection
// would go to a private address.
type BlockedError struct{ Addr netip.Addr }

func (e *BlockedError) Error() string { return "blocked private address " + e.Addr.String() }

var schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// NormalizeURL turns what the user gave into a fetchable URL. A bare
// "host/path" (the form `bud search` prints) is taken as https. Only http
// and https are allowed, and credentials in the URL are refused.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("no URL; use `bud fetch <url>`")
	}
	if !schemeRe.MatchString(raw) {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", fmt.Errorf("%q is not a URL (%v); use something like https://example.com/page", raw, err)
	case u.Scheme != "http" && u.Scheme != "https":
		return "", fmt.Errorf("%q is not an http or https URL; bud fetch reads web pages only", raw)
	case u.Hostname() == "":
		return "", fmt.Errorf("%q has no host; use something like https://example.com/page", raw)
	case u.User != nil:
		return "", fmt.Errorf("%q contains credentials; remove them (bud fetch sends none)", raw)
	}
	u.Fragment = ""
	return u.String(), nil
}

// IsPrivate reports whether a connection to a would stay inside a machine or
// a private network: loopback, private, link-local, multicast, unspecified,
// carrier-grade NAT (where tailnet addresses live) and other reserved space.
func IsPrivate(a netip.Addr) bool {
	a = a.Unmap()
	if a.Is6() { // NAT64 and 6to4 carry an IPv4 address: judge that one
		b := a.As16()
		switch {
		case nat64.Contains(a):
			return IsPrivate(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
		case sixToFour.Contains(a):
			return IsPrivate(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
		}
	}
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range reserved {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
)

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.88.99.0/24"), // 6to4 relay anycast
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("2001::/32"),      // Teredo
	netip.MustParsePrefix("2001:2::/48"),    // benchmarking
	netip.MustParsePrefix("2001:db8::/32"),  // documentation
	netip.MustParsePrefix("100::/64"),       // discard-only
	netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT, tailnet
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Fetch returns the readable text of o.URL, from the cache when it holds a
// fresh copy.
func Fetch(ctx context.Context, o Options) (Result, error) {
	target, err := NormalizeURL(o.URL)
	if err != nil {
		return Result{}, err
	}
	useCache := o.CacheDir != "" && !o.NoCache
	key := filepath.Join(o.CacheDir, cacheKey(target, o.Raw, o.AllowPrivate)+".json")
	if useCache {
		if r, ok := readCache(key, o.now()); ok {
			return r, nil
		}
	}
	r, err := download(ctx, o, target)
	if err != nil {
		return Result{}, err
	}
	if useCache {
		_ = writeCache(key, r, o.now()) // a cache that cannot be written only costs a refetch
	}
	return r, nil
}

func download(ctx context.Context, o Options, target string) (Result, error) {
	client := o.Client
	if client == nil {
		client = newClient(o)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Result{}, fmt.Errorf("fetch %s: %w", target, err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	if o.UserAgent != "" {
		req.Header.Set("User-Agent", o.UserAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, describe(err, target)
	}
	defer resp.Body.Close()
	final := resp.Request.URL.String()
	if resp.StatusCode/100 != 2 {
		hint := "check the URL"
		if resp.StatusCode == 403 || resp.StatusCode == 429 || resp.StatusCode == 503 {
			hint = "the site may block automated clients; try another source"
		}
		return Result{}, fmt.Errorf("%s answered HTTP %d; %s", final, resp.StatusCode, hint)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %v; try again", final, unwrapErr(err))
	}
	cut := len(body) > MaxBody
	if cut {
		body = body[:MaxBody]
	}

	ct := resp.Header.Get("Content-Type")
	mt, params, _ := mime.ParseMediaType(ct)
	if mt == "" {
		sniff := body
		if len(sniff) > 512 {
			sniff = sniff[:512]
		}
		mt, _, _ = mime.ParseMediaType(http.DetectContentType(sniff))
	}
	kind := kindOf(mt)
	if kind == kindBinary {
		what := mt
		if mt == "application/pdf" {
			what = "a PDF"
		}
		return Result{}, fmt.Errorf("%s is %s, not text; bud fetch reads HTML and plain text only, so download it and use a tool made for that format", final, what)
	}

	text := decode(body, params["charset"])
	res := Result{URL: final, ContentType: mt, Status: resp.StatusCode, BodyCut: cut, FetchedAt: o.now().UTC()}
	switch {
	case o.Raw:
		res.Text = tidy(text)
	case kind == kindHTML:
		res.Title, res.Text = ExtractHTML(text)
	default:
		res.Text = tidy(text)
	}
	res.Chars = utf8.RuneCountInString(res.Text)
	return res, nil
}

type contentKind int

const (
	kindBinary contentKind = iota
	kindHTML
	kindText
)

func kindOf(mt string) contentKind {
	switch {
	case mt == "text/html" || mt == "application/xhtml+xml":
		return kindHTML
	case strings.HasPrefix(mt, "text/"), strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"),
		mt == "application/json", mt == "application/xml", mt == "application/javascript",
		mt == "application/x-yaml", mt == "application/yaml", mt == "application/toml":
		return kindText
	}
	return kindBinary
}

// tidy normalises line ends and drops trailing blanks, leaving the text as is.
func tidy(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// decode makes body valid UTF-8. UTF-8 is kept; Latin-1 and Windows-1252
// (treated alike) are widened; anything else is kept as far as it is valid
// UTF-8, with the bad bytes replaced.
func decode(body []byte, charset string) string {
	switch strings.ToLower(charset) {
	case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		rs := make([]rune, len(body))
		for i, b := range body {
			rs[i] = rune(b)
		}
		return string(rs)
	}
	return strings.ToValidUTF8(string(body), "�")
}

// newClient builds a client that refuses private addresses at connect time,
// that is after DNS has answered and for every redirect hop, so neither a
// hostname that resolves inwards nor a redirect to one gets through. It
// ignores proxy settings, since a proxy would resolve names where the guard
// cannot see them.
func newClient(o Options) *http.Client {
	guard := o.guard
	if guard == nil {
		guard = func(a netip.Addr) error {
			if !o.AllowPrivate && IsPrivate(a) {
				return &BlockedError{Addr: a.Unmap()}
			}
			return nil
		}
	}
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("unparseable address %q", address)
			}
			return guard(ap.Addr())
		},
	}
	return &http.Client{
		Timeout: Timeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  15 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
			DisableKeepAlives:      true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", MaxRedirects)
			}
			if s := req.URL.Scheme; s != "http" && s != "https" {
				return fmt.Errorf("redirected to a %q URL; only http and https are followed", s)
			}
			return nil
		},
	}
}

func describe(err error, target string) error {
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		where := target
		var ue *url.Error
		if errors.As(err, &ue) && ue.URL != target {
			where = "a redirect to " + ue.URL
		}
		return fmt.Errorf("refusing %s: it points at the private address %s; pass --allow-private to fetch it anyway", where, blocked.Addr)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fmt.Errorf("%s timed out after %s; try again, or use another source", target, Timeout)
	}
	return fmt.Errorf("fetch %s: %v; check the URL and the network", target, unwrapErr(err))
}

// unwrapErr drops the url.Error wrapper, which repeats the URL.
func unwrapErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// cacheKey names a page's cache file: the URL, whether it is raw and whether
// private targets were allowed.
// A page fetched with --allow-private never answers a fetch without it.
func cacheKey(target string, raw, private bool) string {
	mode := "text"
	if raw {
		mode = "raw"
	}
	if private {
		mode += "+private"
	}
	sum := sha256.Sum256([]byte(mode + "\n" + target))
	return hex.EncodeToString(sum[:])
}

func readCache(path string, now time.Time) (Result, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, false
	}
	var r Result
	if json.Unmarshal(data, &r) != nil {
		return Result{}, false
	}
	age := now.Sub(r.FetchedAt)
	if age < 0 || age >= CacheTTL { // a clock that went backwards counts as a miss
		return Result{}, false
	}
	r.Cached, r.AgeSeconds = true, int(age/time.Second)
	return r, true
}

func writeCache(path string, r Result, now time.Time) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := atomicfile.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	prune(filepath.Dir(path), now)
	return nil
}

// prune deletes cache files older than a day, so the directory cannot grow
// without bound. Failures are ignored: it is housekeeping.
func prune(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && now.Sub(info.ModTime()) > pruneAfter {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
