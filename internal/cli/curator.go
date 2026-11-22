package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/curator"
	"jgarner.dev/bud/internal/kb"
	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/paths"
	models "jgarner.dev/bud/internal/registry"
)

func init() {
	RegisterCommand(Curator())
}

// curatorToken is the environment variable op run fills with the memory
// service's curator token, which may only export
const curatorToken = "BUD_MEMORY_TOKEN_CURATOR"

// Curator promotes working memory into bud-kb (Suite D123, D124); see
// docs/internals/kb.md
func Curator() *Command {
	var kbDir, socket, registryPath, tiers string
	kbFlag := func(fs *flag.FlagSet) {
		fs.StringVar(&kbDir, "kb", "", "the bud-kb clone (default ~/.local/share/bud/kb)")
	}
	return &Command{
		Name:  "curator",
		Usage: "Promotes approved working memory into bud-kb's pages and keeps them current",
		Subs: []*Command{
			{
				Name:  "run",
				Usage: "the nightly run: read new memory, promote, queue contradictions, flag and archive stale sections, push",
				Flags: func(fs *flag.FlagSet) {
					kbFlag(fs)
					fs.StringVar(&socket, "socket", "", "the memory service's socket (default $XDG_RUNTIME_DIR/bud-memory/memory.sock)")
					fs.StringVar(&registryPath, "registry", "", "a registry.jsonc (default: the synced content's registry, with the overlay's merged over it)")
					fs.StringVar(&tiers, "tiers", "P1,P2", "the privacy tiers to promote; add P0 only once bud-kb's remote lives at home")
				},
				Run: func(g *Globals, args []string) error {
					return curatorRun(g, args, kbDir, socket, registryPath, tiers)
				},
			},
			{
				Name:  "promote",
				Flags: kbFlag,
				Usage: "promote a memory record's facts on the next run without a second record: promote <record id>",
				Run:   func(g *Globals, args []string) error { return curatorPromote(g, args, kbDir) },
			},
			{
				Name:  "resolve",
				Flags: kbFlag,
				Usage: "answer a review item: resolve <id> keep|take|confirm",
				Run:   func(g *Globals, args []string) error { return curatorResolve(g, args, kbDir) },
			},
			{
				Name:  "restore",
				Flags: kbFlag,
				Usage: "move an archived section back onto its page: restore <projects|systems>/<page>#<section>",
				Run:   func(g *Globals, args []string) error { return curatorRestore(g, args, kbDir) },
			},
		},
	}
}

// newCurator builds a curator for the KB clone, without memory or a model
func newCurator(kbDir string) (*curator.Curator, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}
	if kbDir == "" {
		kbDir = p.KB()
	}
	return &curator.Curator{
		Repo:  kb.Repo{Dir: kbDir},
		State: p.CuratorState(),
		Now:   time.Now,
		Logf:  func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	}, nil
}

// curatorLoadRegistry and runCurator are swappable for tests
var (
	curatorLoadRegistry = loadRouterRegistry
	runCurator          = func(ctx context.Context, c *curator.Curator) (curator.Summary, error) { return c.Run(ctx) }
)

func curatorRun(g *Globals, args []string, kbDir, socket, registryPath, tierList string) error {
	if len(args) > 0 {
		return usageErrorf("curator run takes no arguments")
	}
	var tiers []string
	for t := range strings.SplitSeq(tierList, ",") {
		t = strings.TrimSpace(t)
		if !slices.Contains(models.Tiers, t) {
			return usageErrorf("--tiers %q: list tiers from P0, P1 and P2, such as P1,P2", tierList)
		}
		tiers = append(tiers, t)
	}
	c, err := newCurator(kbDir)
	if err != nil {
		return err
	}
	if socket == "" {
		p, err := paths.Resolve()
		if err != nil {
			return err
		}
		if socket = p.MemorySocket(); socket == "" {
			return usageErrorf("XDG_RUNTIME_DIR isn't set, so the memory service's socket can't be found; pass --socket")
		}
	}
	tok := os.Getenv(curatorToken)
	if tok == "" {
		return fmt.Errorf("curator: %s is empty; run it under op run with server/curator/curator.env", curatorToken)
	}
	reg, err := curatorLoadRegistry(registryPath)
	if err != nil {
		return err
	}
	endpoints, err := curator.LocalModels(reg)
	if err != nil {
		return err
	}
	c.Memory = memory.New(socket, tok)
	c.Extract = curator.ChatExtractor(endpoints, nil)
	c.Tiers = tiers

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Noticef("curator: %s from %s, tiers %s", c.Repo.Dir, socket, strings.Join(tiers, ","))
	sum, err := runCurator(ctx, c)
	if err != nil {
		return err
	}
	fmt.Fprintf(curatorOut(g), "curator: %s\n", sum)

	return nil
}

func curatorOut(g *Globals) io.Writer {
	if g.stdout != nil {
		return g.stdout
	}
	return os.Stdout
}

func curatorPromote(g *Globals, args []string, kbDir string) error {
	if len(args) != 1 {
		return usageErrorf("curator promote takes one memory record id")
	}
	id, err := curator.ParseRecordID(args[0])
	if err != nil {
		return usageErrorf("%v", err)
	}
	c, err := newCurator(kbDir)
	if err != nil {
		return err
	}
	if err := c.Promote(id); err != nil {
		return err
	}
	fmt.Fprintf(curatorOut(g), "curator: record %d's facts are promoted on the next run\n", id)

	return nil
}

func curatorResolve(g *Globals, args []string, kbDir string) error {
	if len(args) != 2 || !slices.Contains([]string{curator.Keep, curator.Take, curator.Confirm}, args[1]) {
		return usageErrorf("curator resolve takes an item id and keep, take or confirm")
	}
	c, err := newCurator(kbDir)
	if err != nil {
		return err
	}
	if err := c.Resolve(context.Background(), args[0], args[1]); err != nil {
		return err
	}
	fmt.Fprintf(curatorOut(g), "curator: %s answered %s and committed\n", args[0], args[1])

	return nil
}

func curatorRestore(g *Globals, args []string, kbDir string) error {
	if len(args) != 1 {
		return usageErrorf("curator restore takes one <projects|systems>/<page>#<section>")
	}
	c, err := newCurator(kbDir)
	if err != nil {
		return err
	}
	if err := c.Restore(context.Background(), args[0]); err != nil {
		return err
	}
	fmt.Fprintf(curatorOut(g), "curator: restored %s\n", args[0])

	return nil
}
