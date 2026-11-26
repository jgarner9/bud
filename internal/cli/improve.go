package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	bud "jgarner.dev/bud"
	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/audit"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/doctor"
	"jgarner.dev/bud/internal/improve"
	"jgarner.dev/bud/internal/paths"
	models "jgarner.dev/bud/internal/registry"
	"jgarner.dev/bud/internal/router"
	"jgarner.dev/bud/internal/syncer"
)

func init() {
	RegisterCommand(Improve())
}

// improveToken is the environment variable op run fills with the GitHub
// token bud improve opens pull requests with
const improveToken = "BUD_IMPROVE_GITHUB_TOKEN"

// Improve is the self-improvement loop (Suite D47, D125 to D129); see
// docs/internals/improve.md
func Improve() *Command {
	var public, overlay string
	var day string
	var hour int
	return &Command{
		Name:  "improve",
		Usage: "Bud's self-improvement loop: measures, scans models, and asks Jason weekly about proposed changes",
		Subs: []*Command{
			{
				Name:  "run",
				Usage: "one day's round on the home server: measure, scan weekly, follow pull requests, ask the digest when due",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&public, "public", "jgarner9/bud", "the public repo, as owner/name")
					fs.StringVar(&overlay, "overlay", "", "the overlay repo, as owner/name (default: from config overlay.repo)")
					fs.StringVar(&day, "digest-day", "sunday", "the weekday the digest is asked")
					fs.IntVar(&hour, "digest-hour", 10, "the local hour the digest is asked from")
				},
				Run: func(g *Globals, args []string) error { return improveRun(g, args, public, overlay, day, hour) },
			},
			{
				Name:  "list",
				Usage: "list the queued proposals and where each one is",
				Run:   func(g *Globals, args []string) error { return improveList(args) },
			},
			{
				Name:  "show",
				Usage: "print the digest as it would be asked now, or one proposal: show [<id>]",
				Run:   func(g *Globals, args []string) error { return improveShow(args) },
			},
			{
				Name:  "add",
				Usage: "queue a proposal written by hand, from a JSON file or - for stdin: add <proposal.json>",
				Run:   func(g *Globals, args []string) error { return improveAdd(args) },
			},
		},
	}
}

// runImprove is swappable so tests stop before the round
var runImprove = func(ctx context.Context, l *improve.Loop) error { return l.Round(ctx) }

func improveRun(g *Globals, args []string, public, overlay, day string, hour int) error {
	if len(args) > 0 {
		return usageErrorf("improve run takes no arguments")
	}
	wd, ok := weekdays[strings.ToLower(day)]
	if !ok {
		return usageErrorf("--digest-day %q: name a weekday, such as sunday", day)
	}
	if hour < 0 || hour > 23 {
		return usageErrorf("--digest-hour %d: give an hour from 0 to 23", hour)
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if p.ReviewSocket() == "" {
		return fmt.Errorf("improve: XDG_RUNTIME_DIR isn't set, so the Signal reviewer's socket can't be found; run it as a user unit")
	}
	tok := os.Getenv(improveToken)
	if tok == "" {
		return fmt.Errorf("improve: %s is empty; run it under op run with server/improve/improve.env", improveToken)
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return err
	}
	s, err := f.Settings()
	if err != nil {
		return fmt.Errorf("read the machine config: %w", err)
	}
	if overlay == "" {
		overlay = githubRepo(string(syncer.OverlayRepo(s.Overlay.Repo)))
	}
	leaks, err := improve.NewLeaks(os.Getenv("BUD_LEAK_PATTERNS"))
	if err != nil {
		return err
	}
	pin, err := hermesPin()
	if err != nil {
		return err
	}
	gh := &improve.GitHub{Token: tok}
	l := &improve.Loop{
		State:   p.ImproveState(),
		GitHub:  gh,
		Public:  public,
		Overlay: overlay,
		Leaks:   leaks,
		Measure: func(context.Context) improve.Snapshot { return measure(p) },
		List: func(ctx context.Context, _ string, prov models.Provider) ([]models.Listing, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return models.Fetch(ctx, refreshClient, prov)
		},
		Runtimes: []improve.RuntimePin{{
			Name: "Hermes Agent", Pinned: pin,
			Latest: func(ctx context.Context) (string, string, error) {
				return (&improve.GitHub{}).LatestRelease(ctx, "NousResearch/hermes-agent")
			},
		}},
		Channel: func(ctx context.Context) (*content.Channel, error) {
			return syncer.FetchChannel(ctx, s.Content.Mirror)
		},
		Approvals:  approval.Store{Path: p.ApprovalStore(), Audit: openAudit(g, p), Source: audit.SourceImprove},
		Review:     approval.SocketReview(p.ReviewSocket(), p.ReviewSecret(), improve.Scope),
		DigestDay:  wd,
		DigestHour: hour,
		Logf:       func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Noticef("improve: a round for %s (overlay %q)", public, overlay)

	return runImprove(ctx, l)
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// githubRepo reads owner/name from a GitHub git URL, such as
// git@github.com:you/bud-overlay.git; anything else is ""
var githubRepoRe = regexp.MustCompile(`^(?:git@github\.com:|ssh://git@github\.com/|https://github\.com/)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

func githubRepo(u string) string {
	if m := githubRepoRe.FindStringSubmatch(strings.TrimSpace(u)); m != nil {
		return m[1]
	}
	return ""
}

var hermesPinRe = regexp.MustCompile(`(?m)^requires_hermes:\s*"==([0-9][0-9A-Za-z.+-]*)"\s*$`)

// hermesPin is the Hermes release bud-signal's plugin.yaml loads on
func hermesPin() (string, error) {
	b, err := fs.ReadFile(bud.HermesPlugin(), "plugin.yaml")
	if err != nil {
		return "", err
	}
	m := hermesPinRe.FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf(`bud-signal's plugin.yaml has no requires_hermes: "==X.Y.Z" line`)
	}
	return string(m[1]), nil
}

// improveDoctor and improveSpend are what a snapshot reads; tests swap them
var (
	improveDoctor = doctor.Run
	improveSpend  = router.ReadSpend
)

// measure takes today's numbers. Doctor runs quick and offline, as the ops
// tool does
func measure(p *paths.Paths) improve.Snapshot {
	var s improve.Snapshot
	for _, r := range improveDoctor(doctor.Options{Quick: true}) {
		switch r.Status {
		case doctor.Fail:
			s.DoctorFail++
		case doctor.Warn:
			s.DoctorWarn++
		}
	}
	sp, err := improveSpend(p.RouterSpend(), time.Now())
	if err != nil {
		s.NoSpend = true
		return s
	}
	s.Spend, s.TopUp = sp.Spent, sp.TopUp

	return s
}

func improveState() (*improve.State, string, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, "", err
	}
	st, err := improve.Load(p.ImproveState())
	return st, p.ImproveState(), err
}

func improveList(args []string) error {
	if len(args) > 0 {
		return usageErrorf("improve list takes no arguments")
	}
	st, _, err := improveState()
	if err != nil {
		return err
	}
	if len(st.Proposals) == 0 {
		fmt.Println("no proposals yet")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tTARGET\tWHERE\tTITLE")
	for _, p := range st.Proposals {
		where := "-"
		switch {
		case p.Stable != "":
			where = "stable " + p.Stable
		case p.Edge != "":
			where = "edge " + p.Edge
		case p.PRURL != "":
			where = p.PRURL
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Status, p.Target, where, p.Title)
	}

	return w.Flush()
}

func improveShow(args []string) error {
	if len(args) > 1 {
		return usageErrorf("improve show takes at most one proposal ID")
	}
	st, _, err := improveState()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		d := improve.Render(st, "")
		if d.Text == "" {
			fmt.Println("nothing to report")
			return nil
		}
		fmt.Println(d.Text)
		return nil
	}
	p := st.Find(args[0])
	if p == nil {
		return fmt.Errorf("no proposal %s; bud improve list shows them", args[0])
	}
	fmt.Printf("%s: %s\nstatus %s, target %s\n\n%s", p.ID, p.Title, p.Status, p.Target, improve.Body(*p))
	for _, r := range p.Check {
		fmt.Println(r.String())
	}
	if p.Note != "" {
		fmt.Println("note: " + p.Note)
	}

	return nil
}

func improveAdd(args []string) error {
	if len(args) != 1 {
		return usageErrorf("improve add takes one proposal file, or - for stdin")
	}
	var r io.Reader = os.Stdin
	if args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	dec := json.NewDecoder(bufio.NewReader(io.LimitReader(r, 16<<20)))
	dec.DisallowUnknownFields()
	var prop improve.Proposal
	if err := dec.Decode(&prop); err != nil {
		return fmt.Errorf("read the proposal: %w", err)
	}
	prop.Source = "hand"
	leaks, err := improve.NewLeaks(os.Getenv("BUD_LEAK_PATTERNS"))
	if err != nil {
		return err
	}
	_, file, err := improveState()
	if err != nil {
		return err
	}
	var id string
	var added bool
	if err := improve.Update(file, func(st *improve.State) error {
		var err error
		id, added, err = st.Add(prop, leaks, time.Now())
		return err
	}); err != nil {
		return err
	}
	switch {
	case added:
		fmt.Printf("queued %s; it goes in the next weekly digest\n", id)
	case id != "":
		fmt.Printf("the same change is queued already as %s\n", id)
	default:
		fmt.Println("Jason already turned this change down, so it wasn't queued")
	}

	return nil
}
