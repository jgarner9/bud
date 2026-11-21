package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/mcpops"
	"jgarner.dev/bud/internal/paths"
	models "jgarner.dev/bud/internal/registry"
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
	p, err := paths.Resolve()
	if err != nil {
		return fmt.Errorf("resolve bud paths: %w", err)
	}
	src := mcpops.Defaults()
	src.Registry = func() (*models.Registry, error) { return loadRouterRegistry("") }
	srv := mcpops.New(mcpops.Tools(src))
	srv.SetApprovals(mcpops.Approvals{
		Store: approval.Store{Path: p.ApprovalStore()},
		Review: func(s approval.Scope) approval.Review {
			return approval.SocketReview(p.ReviewSocket(), p.ReviewSecret(), s)
		},
	})
	var h http.Handler = srv

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("mcp-ops: serving on http://%s/mcp", listen)
	return serveOps(ctx, listen, h)
}
