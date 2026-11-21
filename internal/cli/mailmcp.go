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
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(MailMCP())
}

// mailFlags are bud mail-mcp's flags
type mailFlags struct {
	listen, imap, smtp, trash, log string
}

// MailMCP runs the home server's mail MCP server over IMAP and SMTP (Suite
// D106); see docs/internals/mail.md
func MailMCP() *Command {
	var f mailFlags
	return &Command{
		Name:  "mail-mcp",
		Usage: "Runs the home server's iCloud mail MCP server, behind the MCP gateway",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&f.listen, "listen", "127.0.0.1:8796", "the loopback host:port to serve MCP on, at /mcp")
			fs.StringVar(&f.imap, "imap", "imap.mail.me.com:993", "the IMAP server, over TLS")
			fs.StringVar(&f.smtp, "smtp", "smtp.mail.me.com:587", "the SMTP server, with STARTTLS")
			fs.StringVar(&f.trash, "trash", "Deleted Messages", "the folder cleanup moves to when none has the \\Trash flag")
			fs.StringVar(&f.log, "log", "", "the action log of moves, restores and sends (default ~/.local/state/bud/mail-actions.jsonl)")
		},
		Run: func(g *Globals, args []string) error { return runMailMCP(g, args, f) },
	}
}

// serveMail is swappable so tests stop before listening
var serveMail = mail.Serve

func runMailMCP(g *Globals, args []string, f mailFlags) error {
	if len(args) > 0 {
		return usageErrorf("mail-mcp takes no arguments")
	}
	if err := mail.CheckListen(f.listen); err != nil {
		return usageErrorf("%v", err)
	}
	user, pass := os.Getenv("BUD_MAIL_USER"), os.Getenv("BUD_MAIL_PASSWORD")
	acct, err := mail.NewAccount(f.imap, user, pass)
	if err != nil {
		return err
	}
	acct.Trash = f.trash
	if f.log == "" {
		p, err := paths.Resolve()
		if err != nil {
			return err
		}
		f.log = p.MailActions()
	}
	if acct.Log, err = mail.NewLog(f.log); err != nil {
		return err
	}
	// sends need the From address; without it bud mail-mcp still reads
	// and cleans up, and the send tools say what to set
	if from := os.Getenv("BUD_MAIL_FROM"); from != "" {
		if acct.Sender, err = mail.NewSender(f.smtp, user, pass, from); err != nil {
			return err
		}
	} else {
		g.Logf("mail-mcp: BUD_MAIL_FROM is unset, so sending is off")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("mail-mcp: serving %s on http://%s/mcp", f.imap, f.listen)
	if err := serveMail(ctx, f.listen, mail.NewServer(acct)); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("mail-mcp: %w", err)
	}
	return nil
}
