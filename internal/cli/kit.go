package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Kit())
}

// Kit writes bud's Claude Code setup into Claude Code's user config (BASE.md
// 4.1): plugin marketplaces, plugins, skills, subagents, slash commands and
// output styles
func Kit() *Command {
	var dryRun, yes bool
	var repo string
	return &Command{
		Name:  "kit",
		Usage: "Syncs bud's Claude Code setup (plugin marketplaces, plugins, skills, agents, commands, output styles) into Claude Code",
		Subs: []*Command{
			{
				Name:  "sync",
				Usage: "write the kit into Claude Code's user config; --dry-run only says what it would do",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&dryRun, "dry-run", false, "say what would change, change nothing")
				},
				Run: func(g *Globals, args []string) error { return kitSync(g, args, dryRun) },
			},
			{
				Name:  "unsync",
				Usage: "take everything the kit added back out of Claude Code's config, leaving what's yours",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&dryRun, "dry-run", false, "say what would be removed, change nothing")
					fs.BoolVar(&yes, "yes", false, "remove without asking")
					fs.BoolVar(&yes, "y", false, "short for -yes")
				},
				Run: func(_ *Globals, args []string) error { return kitUnsync(args, dryRun, yes) },
			},
			{Name: "status", Usage: "per item: synced, not synced, changed, or skipped and why", Run: func(g *Globals, args []string) error { return kitStatus(args) }},
			{Name: "ls", Usage: "list what's in the kit", Run: func(_ *Globals, args []string) error { return kitLs(args) }},
			{
				Name:  "add",
				Usage: "scaffold a kit entry in a bud checkout: add skill|agent|command|output-style|plugin <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&repo, "repo", "", "the bud checkout (default: the one bud dev link points at)")
				},
				Run: func(_ *Globals, args []string) error { return kitAdd(args, repo) },
			},
			{
				Name:  "rm",
				Usage: "take an entry out of the kit in a bud checkout; the next sync removes it from Claude Code: rm <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&repo, "repo", "", "the bud checkout (default: the one bud dev link points at)")
				},
				Run: func(_ *Globals, args []string) error { return kitRm(args, repo) },
			},
			{
				Name:  "adopt",
				Usage: "pull a skill, agent or command you set up by hand into the kit: adopt <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&repo, "repo", "", "the bud checkout (default: the one bud dev link points at)")
				},
				Run: func(_ *Globals, args []string) error { return kitAdopt(args, repo) },
			},
			{
				Name:  "enable",
				Usage: "turn a kit entry on for this machine: enable <name>",
				Run:   func(_ *Globals, args []string) error { return kitToggle(args, true) },
			},
			{
				Name:  "disable",
				Usage: "turn a kit entry off for this machine; the next sync removes it from Claude Code: disable <name>",
				Run:   func(_ *Globals, args []string) error { return kitToggle(args, false) },
			},
		},
	}
}

// swappable in tests
var (
	kitFS     = defaultKitFS
	kitClaude = func(args ...string) error {
		bin := claude.Find()
		if bin == "" {
			return fmt.Errorf("Claude Code isn't installed: run bud claude setup")
		}
		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("claude %s: %v: %s", strings.Join(args[:2], " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

// defaultKitFS is the kit this binary ships, or a dev-linked checkout's, so
// a change to kit/ syncs before it's released
func defaultKitFS() fs.FS {
	if p, err := paths.Resolve(); err == nil {
		if checkout, ok := nvim.DevLink(p.Config); ok && isFile(filepath.Join(checkout, "kit", "kit.json")) {
			return os.DirFS(filepath.Join(checkout, "kit"))
		}
	}

	return bud.Kit()
}

// kitRepo is the checkout's kit/ that add, rm, adopt and --profile edit
func kitRepo(repo string) (kit.Repo, error) {
	if repo == "" {
		p, err := paths.Resolve()
		if err != nil {
			return kit.Repo{}, err
		}
		checkout, ok := nvim.DevLink(p.Config)
		if !ok {
			return kit.Repo{}, usageErrorf("the kit lives in bud's repo: give --repo <checkout>, or bud dev link <checkout> first")
		}
		repo = checkout
	}
	dir := filepath.Join(repo, "kit")
	if !isFile(filepath.Join(dir, "kit.json")) {
		return kit.Repo{}, fmt.Errorf("%s isn't a bud checkout with a kit (no kit/kit.json)", repo)
	}

	return kit.Repo{Dir: dir}, nil
}

func kitAdd(args []string, repo string) error {
	if len(args) != 2 {
		return usageErrorf("kit add takes a kind and a name: add skill|agent|command|output-style|plugin <name>")
	}
	kind, name := args[0], args[1]
	if _, ok := kit.Kinds[kind]; !ok {
		return usageErrorf("kit add: %q isn't skill, agent, command, output-style or plugin", kind)
	}
	r, err := kitRepo(repo)
	if err != nil {
		return err
	}
	if kind == "plugin" {
		if err := r.AddPlugin(name); err != nil {
			return err
		}
		fmt.Printf("Added the plugin %s; bud kit sync installs it.\n", name)
		return nil
	}
	path, err := r.Scaffold(kind, name)
	if err != nil {
		return err
	}
	fmt.Printf("Added the %s %s: fill in %s, then bud kit sync.\n", kind, name, path)

	return nil
}

func kitRm(args []string, repo string) error {
	if len(args) != 1 {
		return usageErrorf("kit rm takes one name")
	}
	name := args[0]
	r, err := kitRepo(repo)
	if err != nil {
		return err
	}
	kind, err := r.Remove(name)
	if err != nil {
		return err
	}
	fmt.Printf("Took the %s %s out of the kit; bud kit sync removes it from Claude Code.\n", kind, name)

	return nil
}

func kitAdopt(args []string, repo string) error {
	if len(args) != 1 {
		return usageErrorf("kit adopt takes one name")
	}
	r, err := kitRepo(repo)
	if err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	env, err := kitEnv(p)
	if err != nil {
		return err
	}
	kind, err := r.Adopt(args[0], env.ClaudeDir)
	if err != nil {
		return err
	}
	if err := kit.Own(env.Ledger, env.ClaudeDir, kind, args[0]); err != nil {
		return err
	}
	fmt.Printf("Adopted the %s %s into %s.\n", kind, args[0], r.Dir)

	return nil
}

// kitToggle turns an entry on or off for this machine (config.json's
// kit.disabled)
func kitToggle(args []string, on bool) error {
	if len(args) != 1 {
		return usageErrorf("kit enable and disable take one name")
	}
	name := args[0]
	k, err := kit.Load(kitFS())
	if err != nil {
		return err
	}
	found := false
	for _, e := range k.Entries() {
		found = found || e[1] == name
	}
	if !found {
		return usageErrorf("the kit has nothing called %s", name)
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return err
	}
	disabled := kitDisabled(f)
	if on {
		disabled = slices.DeleteFunc(disabled, func(n string) bool { return n == name })
	} else if !slices.Contains(disabled, name) {
		disabled = append(disabled, name)
	}
	slices.Sort(disabled)
	list := make([]any, len(disabled))
	for i, n := range disabled {
		list[i] = n
	}
	if err := f.Set("kit.disabled", list); err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}
	state := "on"
	if !on {
		state = "off"
	}
	fmt.Printf("%s is %s on this machine; bud kit sync applies it.\n", name, state)

	return nil
}

func kitDisabled(f *config.File) []string {
	var out []string
	if v, ok := f.Get("kit.disabled"); ok {
		list, _ := v.([]any)
		for _, n := range list {
			if s, ok := n.(string); ok {
				out = append(out, s)
			}
		}
	}

	return out
}

// kitMarketplaces lists the plugin marketplaces Claude Code has, by name
var kitMarketplaces = func() ([]string, error) {
	bin := claude.Find()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.Command(bin, "plugin", "marketplace", "list", "--json").Output()
	if err != nil {
		return nil, err
	}

	return kit.ParseMarketplaces(out)
}

// installedPlugins asks claude which plugins are installed
var installedPlugins = func() ([]string, error) {
	bin := claude.Find()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.Command(bin, "plugin", "list", "--json").Output()
	if err != nil {
		return nil, err
	}
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	ids := make([]string, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}

	return ids, nil
}

// kitEnv is where the kit goes on this machine
func kitEnv(p *paths.Paths) (kit.Env, error) {
	dir, err := homeClaudeDir()
	if err != nil {
		return kit.Env{}, err
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dir = d
	}
	var disabled []string
	if f, err := config.Load(p.MachineConfig()); err == nil {
		disabled = kitDisabled(f)
	}

	return kit.Env{
		ClaudeDir:        dir,
		ClaudeJSON:       claudeJSONFor(dir),
		Ledger:           p.KitLedger(),
		Claude:           kitClaude,
		InstalledPlugins: installedPlugins,
		Marketplaces:     kitMarketplaces,
		Disabled:         disabled,
	}, nil
}

func loadKit() (*kit.Kit, kit.Env, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, kit.Env{}, err
	}
	k, err := kit.Load(kitFS())
	if err != nil {
		return nil, kit.Env{}, err
	}
	env, err := kitEnv(p)

	return k, env, err
}

func kitSync(g *Globals, args []string, dryRun bool) error {
	if len(args) > 0 {
		return usageErrorf("kit sync takes no arguments")
	}
	return syncKitWith(g, terminalPrompter(false), dryRun)
}

// syncKitWith syncs the kit, first taking it out of the Claude Code config
// dir it was last synced into when that's another one (moveKit)
func syncKitWith(g *Globals, pr *prompter, dryRun bool) error {
	k, env, err := loadKit()
	if err != nil {
		return err
	}
	if ok, err := moveKit(pr, env, dryRun); err != nil || !ok {
		return err
	}
	if !dryRun {
		if ok, err := confirmShared(pr, k, env); err != nil || !ok {
			return err
		}
	}
	actions, err := kit.Sync(k, env, dryRun)
	for _, a := range actions {
		if a.Do != "ok" || g.Verbose {
			fmt.Println(a)
		}
	}
	if err != nil {
		return err
	}
	changed := 0
	for _, a := range actions {
		if a.Do != "ok" && a.Do != "skip" {
			changed++
		}
	}
	switch {
	case dryRun && changed == 0:
		fmt.Println("Nothing to change.")
	case dryRun:
		fmt.Printf("%d to change; run bud kit sync to do it.\n", changed)
	case changed == 0:
		fmt.Println("Claude Code already has the kit.")
	default:
		fmt.Printf("Synced the kit into Claude Code (%d changed).\n", changed)
	}

	return nil
}

// kitUnsync takes out of the current Claude Code config dir everything the
// ledger says bud put there, then forgets the ledger, so neither a later
// update nor a sync puts it back unasked (a sync into ~/.claude asks again)
func kitUnsync(args []string, dryRun, yes bool) error {
	if len(args) > 0 {
		return usageErrorf("kit unsync takes no arguments")
	}
	_, env, err := loadKit()
	if err != nil {
		return err
	}
	actions, err := kit.Unsync(env, true)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		fmt.Printf("The kit has nothing in %s.\n", env.ClaudeDir)
		return nil
	}
	fmt.Printf("The kit added these to %s:\n", env.ClaudeDir)
	for _, a := range actions {
		fmt.Printf("  %s %s\n", a.Kind, a.Name)
	}
	if dryRun {
		fmt.Println("bud kit unsync removes them.")
		return nil
	}
	if err := confirmOnTerminal("Remove them?", yes, "bud kit unsync --yes"); err != nil {
		return err
	}
	if _, err := kit.Unsync(env, false); err != nil {
		return err
	}
	if err := os.Remove(env.Ledger); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	fmt.Printf("Took the kit out of %s.\n", env.ClaudeDir)

	return nil
}

// confirmShared shows what the first sync into ~/.claude would do and asks
// first (JGA-146): that's the config the claude you run yourself uses too, so
// bud doesn't add to it unseen. Later syncs only keep up what you said yes to.
// With nobody to ask it changes nothing
func confirmShared(pr *prompter, k *kit.Kit, env kit.Env) (bool, error) {
	home, err := homeClaudeDir()
	if err != nil || env.ClaudeDir != home {
		return err == nil, err
	}
	ledger, ok, err := kit.ReadLedger(env.Ledger)
	if err != nil {
		return false, err
	}
	// a ledger from before bud had its own dir ("") was already in ~/.claude
	if ok && (ledger.Dir == "" || ledger.Dir == home) {
		return true, nil
	}
	actions, err := kit.Sync(k, env, true)
	if err != nil {
		return false, err
	}
	var adds, yours []kit.Action
	for _, a := range actions {
		switch a.Do {
		case "add", "update":
			adds = append(adds, a)
		case "skip":
			yours = append(yours, a)
		}
	}
	if len(adds) == 0 {
		return true, nil
	}
	fmt.Println("bud shares ~/.claude, so the claude you run yourself gets the kit too. It would add:")
	for _, a := range adds {
		fmt.Printf("  %s %s\n", a.Kind, a.Name)
	}
	if len(yours) > 0 {
		fmt.Println("and skip:")
		for _, a := range yours {
			fmt.Printf("  %s %s (%s)\n", a.Kind, a.Name, a.Why)
		}
	}
	fmt.Println("'bud kit disable NAME' keeps one out; 'bud kit unsync' takes them all out again.")
	if pr == nil {
		fmt.Println("Run 'bud kit sync' on a terminal to add them.")
		return false, nil
	}
	yes, err := pr.confirm("Add them to ~/.claude?")
	if err != nil || !yes {
		if err == nil {
			fmt.Println("Nothing changed.")
		}
		return false, err
	}

	return true, nil
}

// statusWords say what a dry run's action means for an item
var statusWords = map[string]string{"ok": "synced", "add": "not synced", "update": "changed", "remove": "to remove", "skip": "skipped"}

func kitStatus(args []string) error {
	if len(args) > 0 {
		return usageErrorf("kit status takes no arguments")
	}
	k, env, err := loadKit()
	if err != nil {
		return err
	}
	actions, err := kit.Sync(k, env, true)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		fmt.Println("The kit is empty.")
	}
	for _, a := range actions {
		line := fmt.Sprintf("%-12s %-24s %s", a.Kind, a.Name, statusWords[a.Do])
		if a.Why != "" {
			line += " (" + a.Why + ")"
		}
		fmt.Println(line)
	}

	return nil
}

func kitLs(args []string) error {
	if len(args) > 0 {
		return usageErrorf("kit ls takes no arguments")
	}
	k, err := kit.Load(kitFS())
	if err != nil {
		return err
	}
	for _, e := range k.Entries() {
		fmt.Println(strings.TrimRight(fmt.Sprintf("%-12s %-16s %s", e[0], e[1], e[2]), " "))
	}

	return nil
}

// resyncKit runs the kit sync after an update, but only once you've synced
// it yourself (there's a ledger): bud doesn't write into Claude Code's config
// unasked. It only ever warns
func resyncKit(g *Globals, p *paths.Paths) {
	if _, ok, err := kit.ReadLedger(p.KitLedger()); err != nil || !ok {
		return
	}
	k, env, err := loadKit()
	if err == nil {
		// moving it to another config dir asks, so that waits for bud kit sync
		var ok bool
		if ok, err = moveKit(nil, env, false); err == nil && !ok {
			return
		}
	}
	if err == nil {
		_, err = kit.Sync(k, env, false)
	}
	if err != nil {
		fmt.Printf("The Claude Code kit didn't sync (%v); bud kit sync tries again.\n", err)
		return
	}
	g.Logf("synced the Claude Code kit")
}
