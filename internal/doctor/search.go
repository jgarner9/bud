package doctor

import (
	"context"
	"os"
	"strings"

	"jgarner.dev/bud/internal/search"
	"jgarner.dev/bud/internal/version"
)

// swappable in tests
var searchProbe = search.Probe

// checkSearch asks the SearXNG instance bud search uses whether it answers
// JSON searches. It only ever warns: a down search engine must not fail
// doctor, which would roll back a bud update
func checkSearch(ctx context.Context) (Result, bool) {
	raw := strings.TrimSpace(os.Getenv("BUD_SEARXNG_URL"))
	if raw == "" {
		return Result{}, false
	}
	base := strings.TrimRight(raw, "/")
	if err := searchProbe(ctx, netClient, base, "bud/"+version.String()); err != nil {
		return Result{Name: "search", Status: Warn, Detail: err.Error(), Fix: "check BUD_SEARXNG_URL and that the instance is running"}, true
	}
	return Result{Name: "search", Status: OK, Detail: "SearXNG at " + base + " answers JSON searches"}, true
}
