package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jgarner.dev/bud/internal/contacts"
	"jgarner.dev/bud/internal/mcpops"
)

func init() {
	RegisterCommand(ContactsMCP())
}

// ContactsMCP runs the read-only iCloud contacts service for private Hermes.
func ContactsMCP() *Command {
	var listen string
	return &Command{
		Name:  "contacts-mcp",
		Usage: "Runs the home server's read-only CardDAV contacts MCP server for private Hermes",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&listen, "listen", "127.0.0.1:8794", "the loopback host:port to serve MCP on, at /mcp")
		},
		Run: func(g *Globals, args []string) error {
			if len(args) > 0 {
				return usageErrorf("contacts-mcp takes no arguments")
			}
			if err := mcpops.CheckListen(listen); err != nil {
				return usageErrorf("contacts-mcp --listen must be a loopback IP address and port, such as 127.0.0.1:8794")
			}
			c, err := contacts.NewClient(os.Getenv("BUD_CARDDAV_USER"), os.Getenv("BUD_CARDDAV_PASSWORD"))
			if err != nil {
				return err
			}
			h, err := contacts.NewServer(c, os.Getenv("BUD_CONTACTS_MCP_TOKEN"))
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			g.Logf("contacts-mcp: serving on http://%s/mcp", listen)
			if err := mcpops.Serve(ctx, listen, h); err != nil {
				return fmt.Errorf("contacts-mcp: %w", err)
			}
			return nil
		},
	}
}
