package cli

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"jgarner.dev/bud/internal/mcpops"
)

func init() {
	RegisterCommand(MCPOps())
}

// MCPOps runs the Bud ops MCP server (Suite D83); see docs/internals/mcp-ops.md
func MCPOps() *Command {
	var listen string
	return &Command{
		Name:  "mcp-ops",
		Usage: "Runs the Bud ops MCP server: doctor and sync status as read-only tools, on loopback",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&listen, "listen", "127.0.0.1:8792", "the loopback address and port to listen on")
		},
		Run: func(g *Globals, args []string) error { return runMCPOps(g, args, listen) },
	}
}

// serveOps is swappable so tests stop before listening
var serveOps = mcpops.Serve

func runMCPOps(g *Globals, args []string, listen string) error {
	if len(args) > 0 {
		return usageErrorf("mcp-ops takes no arguments")
	}
	if err := mcpops.CheckListen(listen); err != nil {
		return usageErrorf("%v", err)
	}
	var h http.Handler = mcpops.New(mcpops.Tools(mcpops.Defaults()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("mcp-ops: serving on http://%s/mcp", listen)
	return serveOps(ctx, listen, h)
}
