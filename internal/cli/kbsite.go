package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	bud "jgarner.dev/bud"
	"jgarner.dev/bud/internal/kb"
	"jgarner.dev/bud/internal/kbsite"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/tailnet"
)

func init() {
	RegisterCommand(KBSite())
}

// KBSite builds bud-kb's pages with Hugo and serves them on the tailnet
// (Suite D53, D122); see docs/internals/kb.md
func KBSite() *Command {
	var o kbSiteOpts
	return &Command{
		Name:  "kb-site",
		Usage: "Builds the KB site from bud-kb's commits with Hugo and serves it on the tailnet",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&o.listen, "listen", "127.0.0.1:8799", "the address to serve on; a loopback or tailnet IP")
			fs.StringVar(&o.kbDir, "kb", "", "the bud-kb clone (default ~/.local/share/bud/kb)")
			fs.StringVar(&o.hugo, "hugo", "hugo", "the hugo binary, 0.146 or newer")
			fs.DurationVar(&o.interval, "interval", time.Minute, "how often to pull and look for a new commit")
			fs.BoolVar(&o.noPull, "no-pull", false, "build what the clone has, without pulling from its origin")
			fs.BoolVar(&o.once, "once", false, "build HEAD once, even if it was built already, and exit without serving")
		},
		Run: func(g *Globals, args []string) error { return runKBSite(g, args, o) },
	}
}

type kbSiteOpts struct {
	listen, kbDir, hugo string
	interval            time.Duration
	noPull, once        bool
}

// serveKBSite is swappable so tests stop before listening
var serveKBSite = func(ctx context.Context, listen string, b *kbsite.Builder, interval time.Duration, pull bool, logf func(string, ...any)) error {
	srv := &http.Server{Addr: listen, Handler: b.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	go b.Watch(ctx, interval, pull, logf)
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runKBSite(g *Globals, args []string, o kbSiteOpts) error {
	if len(args) > 0 {
		return usageErrorf("kb-site takes no arguments")
	}
	host, _, err := net.SplitHostPort(o.listen)
	if err != nil {
		return usageErrorf("--listen %q isn't host:port", o.listen)
	}
	if !tailnet.Inside(host) || host == "localhost" {
		return usageErrorf("--listen %q must be a loopback or tailnet IP address, so the KB is never reachable from outside the tailnet (Suite D72)", host)
	}
	if o.interval < 10*time.Second {
		return usageErrorf("--interval %s is too short: use 10s or more", o.interval)
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if o.kbDir == "" {
		o.kbDir = p.KB()
	}
	b := &kbsite.Builder{Repo: kb.Repo{Dir: o.kbDir}, Site: bud.KBSite(), Hugo: kbsite.ExecHugo(o.hugo), Dir: p.KBSite()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if o.once {
		if !o.noPull {
			unlock, err := b.Repo.Lock()
			if err != nil {
				return err
			}
			err = b.Repo.Pull(ctx)
			unlock()
			if err != nil {
				return err
			}
		}
		if _, err := b.Build(ctx, true); err != nil {
			return err
		}
		st, _ := b.ReadStatus()
		var w io.Writer = os.Stdout
		if g.stdout != nil {
			w = g.stdout
		}
		fmt.Fprintf(w, "kb-site: built %s into %s\n", st.Commit, b.Current())
		return nil
	}

	g.Noticef("kb-site: serving %s from %s on %s", o.kbDir, b.Current(), o.listen)
	return serveKBSite(ctx, o.listen, b, o.interval, !o.noPull, func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) })
}
