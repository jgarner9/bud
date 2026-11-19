// Package search is the engine behind `bud search`: one request to a
// SearXNG instance's JSON API, shaped into short, deduplicated results.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Limits. Result text is cut so a page of results stays a few hundred tokens.
const (
	Timeout      = 15 * time.Second
	MaxBody      = 2 << 20 // bytes read from SearXNG
	MaxTitle     = 100     // characters
	MaxSnippet   = 160
	DefaultMax   = 8
	TimeRanges   = "day, week, month or year"
	jsonHint     = "enable json in settings.yml: search.formats must list json (formats: [html, json]), then restart SearXNG"
	probeTimeout = 3 * time.Second
)

// Options describe one search.
type Options struct {
	BaseURL    string // normalized, no trailing slash
	Query      string
	Max        int // results to keep; 0 keeps all that came back
	TimeRange  string
	Lang       string
	Categories string
	UserAgent  string
	Client     *http.Client // nil means a client with Timeout
}

// Hit is one result.
type Hit struct {
	N       int      `json:"n"`
	Title   string   `json:"title"`
	URL     string   `json:"url"`
	Snippet string   `json:"snippet,omitempty"`
	Engines []string `json:"engines,omitempty"`
}

// Result is a search outcome. Total counts the distinct results SearXNG
// returned; Hits holds the first Max of them.
type Result struct {
	Query string `json:"query"`
	Total int    `json:"total"`
	Hits  []Hit  `json:"results"`
	Cut   int    `json:"cut"`
}

// ValidTimeRange reports whether s is a time range SearXNG understands.
func ValidTimeRange(s string) bool {
	switch s {
	case "day", "week", "month", "year":
		return true
	}
	return false
}

type apiResult struct {
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Engines []string `json:"engines"`
}

// Run asks SearXNG and returns the shaped result.
func Run(ctx context.Context, o Options) (Result, error) {
	if o.BaseURL == "" {
		return Result{}, errors.New("SearXNG is not configured; set BUD_SEARXNG_URL to your SearXNG base URL")
	}
	q := url.Values{"q": {o.Query}, "format": {"json"}}
	if o.TimeRange != "" {
		q.Set("time_range", o.TimeRange)
	}
	if o.Lang != "" {
		q.Set("language", o.Lang)
	}
	if o.Categories != "" {
		q.Set("categories", o.Categories)
	}
	body, err := get(ctx, o.Client, o.BaseURL+"/search?"+q.Encode(), o.UserAgent, o.BaseURL, Timeout)
	if err != nil {
		return Result{}, err
	}
	var resp struct {
		Results []apiResult `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Result{}, fmt.Errorf("SearXNG at %s did not answer with JSON (%v); %s", o.BaseURL, err, jsonHint)
	}

	res := Result{Query: o.Query, Hits: []Hit{}}
	seen := map[string]bool{}
	for _, r := range resp.Results {
		key := dedupeKey(r.URL)
		if r.URL == "" || seen[key] {
			continue
		}
		seen[key] = true
		res.Total++
		if o.Max > 0 && len(res.Hits) >= o.Max {
			continue
		}
		res.Hits = append(res.Hits, Hit{
			N: len(res.Hits) + 1, URL: r.URL, Engines: r.Engines,
			Title:   Clip(oneLine(r.Title), MaxTitle),
			Snippet: Clip(oneLine(r.Content), MaxSnippet),
		})
	}
	res.Cut = res.Total - len(res.Hits)
	return res, nil
}

// Probe makes the request `a health check` uses to check an instance: it must
// answer a JSON search. It returns nil when it does.
func Probe(ctx context.Context, client *http.Client, base, userAgent string) error {
	body, err := get(ctx, client, base+"/search?q=ping&format=json", userAgent, base, probeTimeout)
	if err != nil {
		return err
	}
	var resp struct {
		Results *json.RawMessage `json:"results"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Results == nil {
		return fmt.Errorf("SearXNG at %s did not answer with JSON; %s", base, jsonHint)
	}
	return nil
}

func get(ctx context.Context, client *http.Client, u, userAgent, base string, timeout time.Duration) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build the SearXNG request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach SearXNG at %s: %s; check that it is running and the URL (BUD_SEARXNG_URL)", base, unwrap(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read the SearXNG answer: %w", err)
	}
	if len(body) > MaxBody {
		return nil, fmt.Errorf("SearXNG at %s sent an answer larger than %d MiB; narrow the query (--time-range, --categories)", base, MaxBody>>20)
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("SearXNG at %s refused the JSON request (HTTP 403); %s", base, jsonHint)
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("SearXNG at %s rate-limited the request (HTTP 429); wait and retry, or relax its limiter for this client", base)
	case resp.StatusCode/100 != 2:
		return nil, fmt.Errorf("SearXNG at %s answered HTTP %d; check its logs", base, resp.StatusCode)
	}
	return body, nil
}

// unwrap drops the url.Error wrapper, whose text repeats the whole URL
// (query included).
func unwrap(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// dedupeKey makes two spellings of one page equal: scheme, a leading www.,
// case of the host, a trailing slash, the fragment and utm_ parameters are
// ignored.
func dedupeKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") {
			q.Del(k)
		}
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	return host + strings.TrimRight(u.EscapedPath(), "/") + "?" + q.Encode()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Clip cuts s to n characters, ending in an ellipsis when it was longer.
func Clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}
