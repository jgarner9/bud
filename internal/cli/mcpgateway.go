package cli

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/mcpgateway"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(MCPGateway())
}

// MCPGateway runs the home server's MCP gateway (Suite D72, D81); see
// docs/internals/mcp-gateway.md
func MCPGateway() *Command {
	var configPath, registryPath string
	return &Command{
		Name:  "mcp-gateway",
		Usage: "Runs the home server's MCP gateway: keyed MCP servers for devices on the tailnet",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&configPath, "config", "", "the gateway config (default ~/.local/state/bud/mcp-gateway.json)")
			fs.StringVar(&registryPath, "registry", "", "a registry.jsonc for the privacy rules (default: the synced content's registry, with the overlay's merged over it)")
		},
		Run: func(g *Globals, args []string) error { return runMCPGateway(g, args, configPath, registryPath) },
	}
}

// serveGateway is swappable so tests stop before listening
var serveGateway = mcpgateway.Serve

func runMCPGateway(g *Globals, args []string, configPath, registryPath string) error {
	if len(args) > 0 {
		return usageErrorf("mcp-gateway takes no arguments")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if configPath == "" {
		configPath = p.MCPGateway()
	}
	c, err := mcpgateway.Read(configPath)
	if err != nil {
		return err
	}
	// the privacy rules raise a tool call's tier (Suite D112); without a
	// registry the gateway doesn't start, so no call slips past them
	reg, err := loadRouterRegistry(registryPath)
	if err != nil {
		return err
	}
	rules, err := reg.CompileRules()
	if err != nil {
		return err
	}
	logPath := c.AuditLog
	if logPath == "" {
		logPath = p.MCPAudit()
	}
	audit, err := mcpgateway.OpenAudit(logPath)
	if err != nil {
		return err
	}
	defer audit.Close()
	gw, err := mcpgateway.New(c, audit, os.Getenv)
	if err != nil {
		return err
	}

	gw.SetRules(rules)
	gw.SetApprovals(mcpgateway.Approvals{
		Store: approval.Store{Path: p.ApprovalStore()},
		Review: func(s approval.Scope) approval.Review {
			return approval.SocketReview(p.ReviewSocket(), p.ReviewSecret(), s)
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Noticef("mcp-gateway: serving %d servers on %s, %d privacy rules, audit log %s", len(c.Servers), c.Listen, len(rules), logPath)
	return serveGateway(ctx, c, gw)
}
