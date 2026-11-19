package cli

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"jgarner.dev/bud/internal/fetch"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/search"
	"jgarner.dev/bud/internal/version"
)

var searchLang = regexp.MustCompile(`^(all|[a-z]{2,3}([-_][A-Za-z0-9]{2,8})?)$`)
var searchCategories = regexp.MustCompile(`^[a-z0-9_ ]+(,[a-z0-9_ ]+)*$`)

func init() {
	RegisterCommand(Search())
	RegisterCommand(Fetch())
}

// Search queries a configured SearXNG instance.
func Search() *Command {
	var out toolOutput
	var max int
	var timeRange, lang, categories string
	return &Command{
		Name: "search", Usage: "Search a SearXNG instance: search [flags] <query>",
		Flags: func(fs *flag.FlagSet) {
			out.Bind(fs, 6000)
			fs.IntVar(&max, "max", search.DefaultMax, "maximum results (0 for all)")
			fs.StringVar(&timeRange, "time-range", "", "day, week, month or year")
			fs.StringVar(&lang, "lang", "", "result language")
			fs.StringVar(&categories, "categories", "", "comma-separated categories")
		},
		Run: func(g *Globals, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" || max < 0 || out.MaxChars < 0 {
				return usageErrorf("search: provide a query and non-negative limits")
			}
			if timeRange != "" && !search.ValidTimeRange(timeRange) {
				return usageErrorf("search: --time-range must be %s", search.TimeRanges)
			}
			if lang != "" && !searchLang.MatchString(lang) {
				return usageErrorf("search: invalid language %q", lang)
			}
			if categories != "" && !searchCategories.MatchString(categories) {
				return usageErrorf("search: invalid categories %q", categories)
			}
			base := os.Getenv("BUD_SEARXNG_URL")
			u, err := url.Parse(base)
			if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
				return fmt.Errorf("set BUD_SEARXNG_URL to an http(s) SearXNG base URL without credentials, query or fragment")
			}
			result, err := search.Run(context.Background(), search.Options{BaseURL: strings.TrimRight(base, "/"), Query: query, Max: max, TimeRange: timeRange, Lang: lang, Categories: categories, UserAgent: "bud/" + version.String()})
			if err != nil {
				return err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "search %q: %d results", result.Query, result.Total)
			for _, hit := range result.Hits {
				fmt.Fprintf(&b, "\n%d. %s - %s", hit.N, hit.Title, hit.URL)
				if hit.Snippet != "" {
					fmt.Fprintf(&b, "\n   %s", hit.Snippet)
				}
			}
			if result.Cut > 0 {
				fmt.Fprintf(&b, "\n[%d more results cut; use --max 0]", result.Cut)
			}
			w := g.stdout
			if w == nil {
				w = os.Stdout
			}
			return out.Print(w, b.String(), result)
		},
	}
}

// Fetch retrieves a page with private-address and redirect checks.
func Fetch() *Command {
	var out toolOutput
	var raw, noCache, allowPrivate bool
	return &Command{
		Name: "fetch", Usage: "Fetch a page as readable text: fetch [flags] <url>",
		Flags: func(fs *flag.FlagSet) {
			out.Bind(fs, 6000)
			fs.BoolVar(&raw, "raw", false, "return raw text")
			fs.BoolVar(&noCache, "no-cache", false, "skip cache")
			fs.BoolVar(&allowPrivate, "allow-private", false, "allow private addresses")
		},
		Run: func(g *Globals, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" || out.MaxChars < 0 {
				return usageErrorf("fetch: provide one URL and a non-negative --max-chars")
			}
			p, err := paths.Resolve()
			if err != nil {
				return err
			}
			result, err := fetch.Fetch(context.Background(), fetch.Options{URL: args[0], Raw: raw, NoCache: noCache, AllowPrivate: allowPrivate, CacheDir: filepath.Join(p.Cache, "fetch"), UserAgent: "bud/" + version.String()})
			if err != nil {
				return err
			}
			var b strings.Builder
			b.WriteString(result.URL)
			if result.Title != "" {
				fmt.Fprintf(&b, "\ntitle: %s", result.Title)
			}
			if result.Cached {
				fmt.Fprintf(&b, "\nnote: cached %ds ago", result.AgeSeconds)
			}
			if result.BodyCut {
				fmt.Fprintf(&b, "\nnote: body truncated at %d bytes", fetch.MaxBody)
			}
			if result.Text == "" {
				b.WriteString("\nnote: no readable text; try --raw")
			} else {
				b.WriteString("\n" + result.Text)
			}
			w := g.stdout
			if w == nil {
				w = os.Stdout
			}
			return out.Print(w, b.String(), result)
		},
	}
}
