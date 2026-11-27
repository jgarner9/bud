package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/curator"
	"jgarner.dev/bud/internal/mail"
	"jgarner.dev/bud/internal/mailingest"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(MailIngest())
}

// mailIngestFlags are bud mail-ingest's flags
type mailIngestFlags struct {
	imap, folders, registry string
	limit, backfill         int
}

// MailIngest summarizes new mail with a local model that has no tools
// (Suite D106, JGA-317); see docs/internals/mail.md
func MailIngest() *Command {
	var f mailIngestFlags
	return &Command{
		Name:  "mail-ingest",
		Usage: "Summarizes new iCloud mail with a local model that has no tools, for the mail server's list_summaries",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&f.imap, "imap", "imap.mail.me.com:993", "the IMAP server, over TLS")
			fs.StringVar(&f.folders, "folders", "INBOX", "the folders to read, separated by commas")
			fs.StringVar(&f.registry, "registry", "", "a registry.jsonc (default: the synced content's registry, with the overlay's merged over it)")
			fs.IntVar(&f.limit, "limit", 25, "the most messages to summarize in one run")
			fs.IntVar(&f.backfill, "backfill", 0, "how many of a folder's newest messages to summarize the first time it is seen")
		},
		Run: func(g *Globals, args []string) error { return runMailIngest(g, args, f) },
	}
}

// mailIngestRegistry and doMailIngest are swappable for tests
var (
	mailIngestRegistry = loadRouterRegistry
	doMailIngest       = func(ctx context.Context, r *mailingest.Run) (mailingest.Result, error) { return r.Do(ctx) }
)

func runMailIngest(g *Globals, args []string, f mailIngestFlags) error {
	if len(args) > 0 {
		return usageErrorf("mail-ingest takes no arguments")
	}
	if f.limit < 1 || f.limit > 200 {
		return usageErrorf("--limit %d: give 1 to 200", f.limit)
	}
	if f.backfill < 0 || f.backfill > f.limit {
		return usageErrorf("--backfill %d: give 0 to --limit (%d)", f.backfill, f.limit)
	}
	var folders []string
	for name := range strings.SplitSeq(f.folders, ",") {
		if name = strings.TrimSpace(name); name != "" {
			folders = append(folders, name)
		}
	}
	if len(folders) == 0 {
		return usageErrorf("--folders names no folder")
	}
	acct, err := mail.NewAccount(f.imap, os.Getenv("BUD_MAIL_USER"), os.Getenv("BUD_MAIL_PASSWORD"))
	if err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	reg, err := mailIngestRegistry(f.registry)
	if err != nil {
		return err
	}
	// the curator role's local models: mail is P0, so never the router and
	// never a provider that isn't local
	endpoints, err := curator.LocalModels(reg)
	if err != nil {
		return err
	}
	store, err := mailingest.NewStore(p.MailSummaries())
	if err != nil {
		return err
	}
	run := &mailingest.Run{
		Fetch:     acct,
		Summarize: mailingest.ChatSummarizer(endpoints, nil),
		Store:     store,
		StatePath: p.MailIngest(),
		Audit:     openAudit(g, p),
		Opts:      mailingest.Options{Folders: folders, Limit: f.limit, MaxText: 16 << 10, Backfill: f.backfill},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	res, err := doMailIngest(ctx, run)
	fmt.Fprintf(mailIngestOut(g), "mail-ingest: %s\n", describe(res))
	return err
}

func mailIngestOut(g *Globals) io.Writer {
	if g.stdout != nil {
		return g.stdout
	}
	return os.Stdout
}

// describe says what a run did in counts, never in text from the mail
func describe(r mailingest.Result) string {
	s := fmt.Sprintf("%d summarized, %d failed, %d already stored", r.Summarized, r.Failed, r.Skipped)
	if len(r.Started) > 0 {
		s += "; began " + strings.Join(r.Started, ", ") + " at its newest message"
	}
	if len(r.Reset) > 0 {
		s += "; UIDVALIDITY changed in " + strings.Join(r.Reset, ", ") + ", so it restarts at its newest message"
	}
	if r.More {
		s += "; more mail waits for the next run"
	}
	return s
}
