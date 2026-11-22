package cli

import (
	"flag"
	"io"
	"net"
	"net/url"
	"sort"
	"testing"

	"jgarner.dev/bud/internal/adapter/hermes"
)

// Every home-server service listens on its own port. A default that lands
// on another service's port fails at start on the server, as memory-mcp's
// 8793 did against signal-cli's (server/README.md has the table)
func TestServerDefaultPortsDontCollide(t *testing.T) {
	sig, err := url.Parse(hermes.SignalURL)
	if err != nil {
		t.Fatal(err)
	}
	owner := map[string]string{
		sig.Port(): "signal-cli",
		"8797":     "planning OpenCode (server/planning-opencode)",
		"8082":     "the embedding server (server/embed)",
	}
	names := make([]string, 0, len(CommandRegistry))
	for name := range CommandRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	found := 0
	for _, name := range names {
		cmd := CommandRegistry[name]
		if cmd.Flags == nil {
			continue
		}
		fs := flag.NewFlagSet(name, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		cmd.Flags(fs)
		f := fs.Lookup("listen")
		if f == nil || f.DefValue == "" {
			continue
		}
		_, port, err := net.SplitHostPort(f.DefValue)
		if err != nil {
			t.Errorf("bud %s --listen default %q: %v", name, f.DefValue, err)
			continue
		}
		found++
		if other, ok := owner[port]; ok {
			t.Errorf("bud %s defaults to port %s, which %s uses", name, port, other)
			continue
		}
		owner[port] = "bud " + name
	}
	if found < 5 {
		t.Errorf("found %d server commands with a --listen default; the lookup has gone stale", found)
	}
}
