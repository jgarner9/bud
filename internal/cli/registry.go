package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
	models "jgarner.dev/bud/internal/registry"
)

// refreshClient fetches a provider's model list; tests swap it
var refreshClient = &http.Client{Timeout: 30 * time.Second}

func init() {
	RegisterCommand(Registry())
}

// Registry checks the provider and model registry (Suite D46, D58), the file
// bud sync renders every runtime's config from
func Registry() *Command {
	var write bool
	return &Command{
		Name:  "registry",
		Usage: "Checks the provider and model registry",
		Subs: []*Command{
			{
				Name:  "check",
				Usage: "validate a registry, with an overlay merged on top: check <registry.jsonc> [overlay.jsonc]",
				Run:   func(_ *Globals, args []string) error { return registryCheck(args) },
			},
			{
				Name:  "refresh",
				Usage: "recheck a provider's models against its model list: refresh [--write] <registry.jsonc> <provider>",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&write, "write", false, "rewrite the drifted facts (inputs, caps, context, output, price) in the file")
				},
				Run: func(_ *Globals, args []string) error { return registryRefresh(args, write) },
			},
		},
	}
}

func registryCheck(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return usageErrorf("registry check takes the registry file and, optionally, an overlay's")
	}
	r, err := models.Read(args[0])
	if err != nil {
		return err
	}
	name := args[0]
	if len(args) == 2 {
		overlay, err := models.Read(args[1])
		if err != nil {
			return err
		}
		if r, err = models.Merge(r, overlay); err != nil {
			return err
		}
		name += " with " + args[1] + " over it"
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	fmt.Printf("%s: %d providers, %d models, all %d roles filled\n", name, len(r.Providers), len(r.Models), len(models.Roles))
	return nil
}

// registryRefresh rechecks one provider's models against the provider's own
// model list (Suite D69): it reports facts that drifted, models the provider
// no longer lists and ones the registry doesn't have. With write it rewrites
// the drifted facts in place, keeping comments and every hand-kept field.
// It never adds or drops a model: that is Jason's call in review
func registryRefresh(args []string, write bool) error {
	if len(args) != 2 {
		return usageErrorf("registry refresh takes the registry file and a provider ID, e.g. refresh content/env/registry.jsonc go")
	}
	path, provider := args[0], args[1]
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the registry: %w", err)
	}
	r, err := models.Parse(src)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	p, ok := r.Providers[provider]
	if !ok {
		return usageErrorf("%s has no provider %q", path, provider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := models.Fetch(ctx, refreshClient, p)
	if err != nil {
		return fmt.Errorf("%s: %w", provider, err)
	}

	rep := models.Compare(r, provider, list)
	fmt.Printf("%s: %d registry models checked against %d listed\n", provider, len(rep.Checked), rep.Listed)
	for _, d := range rep.Drift {
		was := d.Was
		if was == "" {
			was = "unset"
		}
		fmt.Printf("  %s.%s: %s -> %s\n", d.Model, d.Field, was, d.Now)
	}
	if len(rep.Gone) > 0 {
		fmt.Printf("  gone: %s (no longer listed; take them out of their pools and models)\n", strings.Join(rep.Gone, ", "))
	}
	if len(rep.New) > 0 {
		fmt.Printf("  not in the registry: %s\n", strings.Join(rep.New, ", "))
	}
	if len(rep.Drift) == 0 {
		fmt.Println("  every fact matches")
		return nil
	}
	if !write {
		fmt.Println("  run with --write to rewrite these facts")
		return nil
	}

	changed := map[string]models.Model{}
	byName := map[string]models.Listing{}
	for _, l := range list {
		byName[l.ID] = l
	}
	for _, d := range rep.Drift {
		m := r.Models[d.Model]
		changed[d.Model] = models.ApplyFacts(m, *byName[m.Name].Facts)
	}
	out, err := models.PatchFacts(src, changed)
	if err != nil {
		return err
	}
	next, err := models.Parse(out)
	if err != nil {
		return fmt.Errorf("the rewritten registry doesn't parse, so nothing was written: %w", err)
	}
	if err := next.Validate(); err != nil {
		return fmt.Errorf("the new facts break the registry, so nothing was written; fix the pools first: %w", err)
	}
	if err := atomicfile.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write the registry: %w", err)
	}
	fmt.Printf("  rewrote %d drifted facts in %s\n", len(rep.Drift), path)

	return nil
}
