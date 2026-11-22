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

	"jgarner.dev/bud/internal/calendar"
	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/memorymcp"
	"jgarner.dev/bud/internal/paths"
	models "jgarner.dev/bud/internal/registry"
)

func init() {
	RegisterCommand(MemoryMCP())
}

// MemoryMCP runs the home server's memory MCP server (Suite D102, D108);
// see docs/internals/memory-mcp.md
func MemoryMCP() *Command {
	var listen, socket, registryPath string
	var noBackfill bool
	return &Command{
		Name:  "memory-mcp",
		Usage: "Runs the home server's memory MCP server over the working-memory service, behind the MCP gateway",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&listen, "listen", "127.0.0.1:8798", "the loopback host:port to serve MCP on, at /mcp")
			fs.StringVar(&socket, "socket", "", "the memory service's socket (default $XDG_RUNTIME_DIR/bud-memory/memory.sock)")
			fs.StringVar(&registryPath, "registry", "", "a registry.jsonc (default: the synced content's registry, with the overlay's merged over it)")
			fs.BoolVar(&noBackfill, "no-backfill", false, "don't embed approved records in the background")
		},
		Run: func(g *Globals, args []string) error {
			return runMemoryMCP(g, args, listen, socket, registryPath, noBackfill)
		},
	}
}

// memoryTokens are the environment variables op run fills, one memory
// service identity each
var memoryTokens = []string{"BUD_MEMORY_TOKEN_P0", "BUD_MEMORY_TOKEN_P1", "BUD_MEMORY_TOKEN_P2", "BUD_MEMORY_TOKEN_STAGE", "BUD_MEMORY_TOKEN_EMBED"}

// serveMemory is swappable so tests stop before listening
var serveMemory = calendar.Serve

func runMemoryMCP(g *Globals, args []string, listen, socket, registryPath string, noBackfill bool) error {
	if len(args) > 0 {
		return usageErrorf("memory-mcp takes no arguments")
	}
	if err := calendar.CheckListen(listen); err != nil {
		return usageErrorf("%v", strings.ReplaceAll(err.Error(), "the calendar", "memory"))
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
	tok := map[string]string{}
	var missing []string
	for _, name := range memoryTokens {
		if tok[name] = os.Getenv(name); tok[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("memory-mcp: %s %s empty; run it under op run with server/memory-mcp/memory-mcp.env", strings.Join(missing, ", "), map[bool]string{true: "is", false: "are"}[len(missing) == 1])
	}
	reg, err := loadRouterRegistry(registryPath)
	if err != nil {
		return err
	}
	model, prov, err := embeddingModel(reg)
	if err != nil {
		return err
	}
	emb, err := memorymcp.NewHTTPEmbedder(prov.Endpoint, model.Name, model.Dims)
	if err != nil {
		return err
	}

	srv := &memorymcp.Server{
		Search: map[string]memorymcp.Searcher{},
		Stage:  memory.New(socket, tok["BUD_MEMORY_TOKEN_STAGE"]),
		Embed:  emb,
		Model:  model.Name,
	}
	for _, tier := range models.Tiers {
		srv.Search[tier] = memory.New(socket, tok["BUD_MEMORY_TOKEN_"+tier])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !noBackfill {
		b := &memorymcp.Backfill{Vectors: memory.New(socket, tok["BUD_MEMORY_TOKEN_EMBED"]), Embed: emb, Model: model.Name, Logf: func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }}
		go b.Run(ctx)
	}
	g.Logf("memory-mcp: serving %s on http://%s/mcp, embedding with %s at %s", socket, listen, model.Name, prov.Endpoint)
	if err := serveMemory(ctx, listen, srv); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("memory-mcp: %w", err)
	}

	return nil
}

// embeddingModel is the embedding role's first model, which must be on a
// local provider: memory holds P0 text (Suite D52)
func embeddingModel(reg *models.Registry) (models.Model, models.Provider, error) {
	role, ok := reg.Roles["embedding"]
	if !ok || len(role.Members()) == 0 {
		return models.Model{}, models.Provider{}, fmt.Errorf("the registry has no embedding role; add one with a local model (docs/registry.md)")
	}
	id := role.Members()[0]
	m := reg.Models[id]
	prov := reg.Providers[m.Provider]
	if !prov.Local || m.Dims == 0 {
		return models.Model{}, models.Provider{}, fmt.Errorf("the embedding model %s must be on a local provider and give its dims, since memory holds private text", id)
	}

	return m, prov, nil
}
