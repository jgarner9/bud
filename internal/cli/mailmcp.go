package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"jgarner.dev/bud/internal/mail"
)

func init() {
	RegisterCommand(MailMCP())
}

// MailMCP runs the home server's mail MCP server over IMAP (Suite D106);
// see docs/internals/mail.md
func MailMCP() *Command {
	var listen, imap string
	return &Command{
		Name:  "mail-mcp",
		Usage: "Runs the home server's iCloud mail MCP server, behind the MCP gateway",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&listen, "listen", "127.0.0.1:8796", "the loopback host:port to serve MCP on, at /mcp")
			fs.StringVar(&imap, "imap", "imap.mail.me.com:993", "the IMAP server, over TLS")
		},
		Run: func(g *Globals, args []string) error { return runMailMCP(g, args, listen, imap) },
	}
}

// serveMail is swappable so tests stop before listening
var serveMail = mail.Serve

func runMailMCP(g *Globals, args []string, listen, imap string) error {
	if len(args) > 0 {
		return usageErrorf("mail-mcp takes no arguments")
	}
	if err := mail.CheckListen(listen); err != nil {
		return usageErrorf("%v", err)
	}
	acct, err := mail.NewAccount(imap, os.Getenv("BUD_MAIL_USER"), os.Getenv("BUD_MAIL_PASSWORD"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("mail-mcp: serving %s on http://%s/mcp", imap, listen)
	if err := serveMail(ctx, listen, mail.NewServer(acct)); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("mail-mcp: %w", err)
	}
	return nil
}
