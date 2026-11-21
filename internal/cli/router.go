package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
	models "jgarner.dev/bud/internal/registry"
	"jgarner.dev/bud/internal/router"
	"jgarner.dev/bud/internal/syncer"
	"jgarner.dev/bud/internal/tailnet"
)

func init() {
	RegisterCommand(Router())
}

// Router runs the home server's OpenAI-compatible router (Suite D70, D71);
// see docs/internals/router.md
func Router() *Command {
	var listen, registryPath string
	return &Command{
		Name:  "router",
		Usage: "Runs the home server's OpenAI-compatible router over the registry's providers",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&listen, "listen", "127.0.0.1:8795", "the address to serve on; a loopback or tailnet IP")
			fs.StringVar(&registryPath, "registry", "", "a registry.jsonc (default: the synced content's registry, with the overlay's merged over it)")
		},
		Run: func(g *Globals, args []string) error { return runRouter(g, args, listen, registryPath) },
	}
}

// serveRouter is swappable so tests stop before listening
var serveRouter = router.Serve

func runRouter(g *Globals, args []string, listen, registryPath string) error {
	if len(args) > 0 {
		return usageErrorf("router takes no arguments")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return usageErrorf("--listen %q isn't host:port", listen)
	}
	if !tailnet.Inside(host) || host == "localhost" {
		return usageErrorf("--listen %q must be a loopback or tailnet IP address, so the router is never reachable from outside the tailnet (Suite D72)", host)
	}
	reg, err := loadRouterRegistry(registryPath)
	if err != nil {
		return err
	}
	rt, err := router.New(reg, os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("router: serving %d models on %s", len(reg.Models), listen)
	return serveRouter(ctx, listen, rt)
}

// loadRouterRegistry reads --registry, or else the registry the last sync
// rendered from, the way bud doctor finds it
func loadRouterRegistry(path string) (*models.Registry, error) {
	if path != "" {
		return models.Load(path)
	}
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}
	led, err := syncer.LoadLedger(p.SyncLedger())
	if err != nil {
		return nil, err
	}
	if led.Content == "" {
		return nil, errors.New("no synced content to read a registry from: run bud sync, or pass --registry")
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return nil, err
	}
	s, err := f.Settings()
	if err != nil {
		return nil, fmt.Errorf("read the machine config: %w", err)
	}
	overlayDir := syncer.OverlayOnDisk(p, syncer.OverlayRepo(s.Overlay.Repo))
	return syncer.LoadRegistry(filepath.Join(p.Content(), led.Content), overlayDir)
}
