package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/mail"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(MailMCP())
}

// mailFlags are bud mail-mcp's flags
type mailFlags struct {
	listen, imap, smtp, trash, log string
	record                         string
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
			fs.StringVar(&f.record, "record", "", "instead of serving, record one read-only IMAP session to this file, redacted, and print what it found")
		},
		Run: func(g *Globals, args []string) error { return runMailMCP(g, args, f) },
	}
}

// recordMail writes a redacted transcript of one read-only session and
// prints what the live check needs: MOVE, UIDPLUS and the trash folder
func recordMail(g *Globals, acct *mail.Account, path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create the recording (it must not exist yet): %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	found, err := acct.Record(ctx, file)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("record a mail session (what was recorded is in %s): %w", path, err)
	}
	w := g.stdout
	if w == nil {
		w = os.Stdout
	}
	trash := found.Trash
	if trash == "" {
		trash = "none has the \\Trash flag; cleanup uses --trash"
	}
	fmt.Fprintf(w, "recorded to %s\nMOVE: %v\nUIDPLUS: %v\ntrash folder: %s\nfolders: %s\n", path, found.Move, found.UIDPlus, trash, strings.Join(found.Folders, ", "))
	if !found.Move && !found.UIDPlus {
		fmt.Fprintln(w, "warning: without MOVE or UIDPLUS, bud won't move mail to the trash")
	}
	return nil
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
	if f.record != "" {
		return recordMail(g, acct, f.record)
	}
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
	if p, err := paths.Resolve(); err == nil {
		acct.Log.Audit = openAudit(g, p)
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
