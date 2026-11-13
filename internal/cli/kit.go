package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
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
// 4.1, D77): the plugin marketplace bud's plugins come from and the plugins
func Kit() *Command {
	var dryRun, yes, pasted bool
	var repo string
	return &Command{
		Name:  "kit",
		Usage: "Syncs bud's Claude Code setup (its plugins and their marketplace) into Claude Code",
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
				Name:  "core",
				Usage: "print bud-plugins' core.md, to paste into claude.ai's personal preferences (bud kit core | pbcopy); --pasted records that you did",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&pasted, "pasted", false, "record the current core.md as pasted, which quiets the reminder")
				},
				Run: func(_ *Globals, args []string) error { return kitCore(args, pasted) },
			},
			{
				Name:  "add",
				Usage: "list a plugin or marketplace in a bud checkout's kit: add plugin <name@marketplace>, or add marketplace <name> <source>",
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

// kitRepo is the checkout's kit/ that add and rm edit
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
	usage := "kit add takes add plugin <name@marketplace>, or add marketplace <name> <source>"
	if len(args) < 2 || !slices.Contains(kit.Kinds, args[0]) {
		return usageErrorf("%s", usage)
	}
	r, err := kitRepo(repo)
	if err != nil {
		return err
	}
	switch {
	case args[0] == "plugin" && len(args) == 2:
		if err := r.AddPlugin(args[1]); err != nil {
			return err
		}
		fmt.Printf("Added the plugin %s; bud kit sync installs it.\n", args[1])
	case args[0] == "marketplace" && len(args) == 3:
		if err := r.AddMarketplace(args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("Added the marketplace %s; bud kit sync adds it before installing its plugins.\n", args[1])
	default:
		return usageErrorf("%s", usage)
	}

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
		// also one naming the plugin from a marketplace the kit moved away from
		disabled = slices.DeleteFunc(disabled, func(n string) bool { return kit.Turns(n, name) })
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
var installedPlugins = func() ([]kit.Plugin, error) {
	bin := claude.Find()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.Command(bin, "plugin", "list", "--json").Output()
	if err != nil {
		return nil, err
	}

	return kit.ParsePlugins(out)
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
	if !dryRun {
		if p, err := paths.Resolve(); err == nil {
			os.Remove(p.KitDeclined())
		}
		remindCore(k, env)
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
	// so a later bud update doesn't offer it back
	if p, err := paths.Resolve(); err == nil {
		_ = markKitDeclined(p)
	}

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

// kitStatusStyle is the colour of a status word: synced is green, skipped
// dim, and anything a sync would change yellow
func kitStatusStyle(do string) string {
	switch do {
	case "ok":
		return ansiGreen
	case "skip":
		return ansiDim
	}

	return ansiYellow
}

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
	out := os.Stdout
	for _, a := range actions {
		line := cell(out, ansiDim, a.Kind, 12) + " " + cell(out, ansiBold, a.Name, 24) + " " + paint(out, kitStatusStyle(a.Do), statusWords[a.Do])
		if a.Why != "" {
			line += " " + paint(out, ansiDim, "("+a.Why+")")
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
	out := os.Stdout
	for _, e := range k.Entries() {
		line := cell(out, ansiDim, e[0], 12) + " " + cell(out, ansiBold, e[1], 16) + " " + paint(out, ansiDim, e[2])
		fmt.Println(strings.TrimRight(line, " "))
	}

	return nil
}

// resyncKit runs the kit sync after an update. A kit you've synced (there's a
// ledger) keeps up with the release; one you never synced is only offered on
// a terminal (pr), since bud doesn't write into Claude Code's config unasked,
// and not again once you've said no or taken it out (KitDeclined). With pr
// nil (a launch finishing a background update) only a synced kit is kept up,
// and nothing is moved to another config dir without asking. It only warns
func resyncKit(g *Globals, p *paths.Paths, pr *prompter) {
	_, synced, err := kit.ReadLedger(p.KitLedger())
	if err != nil {
		kitWarn(err)
		return
	}
	if !synced {
		offerKit(g, p, pr)
		return
	}
	if pr != nil {
		if err := syncKit(g, pr, false); err != nil {
			kitWarn(err)
		}
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
		kitWarn(err)
		return
	}
	g.Logf("synced the Claude Code kit")
	remindCore(k, env)
}

// kitWarn says the kit didn't sync after an update or install, which only warns
func kitWarn(err error) {
	fmt.Printf("The Claude Code kit didn't sync (%v); bud kit sync tries again.\n", err)
}

// offerKit asks to sync a kit that isn't synced, once Claude Code is logged
// in (D72). Into ~/.claude the sync itself lists what it adds and asks
// (confirmShared); into another dir it asks here. A no, either way, is
// remembered, so the next update doesn't ask again
func offerKit(g *Globals, p *paths.Paths, pr *prompter) {
	if pr == nil || declinedKit(p) {
		return
	}
	ready, err := claudeLoggedIn()
	if err != nil || !ready {
		if err != nil {
			kitWarn(err)
		}
		return
	}
	_, env, err := loadKit()
	if err != nil {
		kitWarn(err)
		return
	}
	if home, err := homeClaudeDir(); err != nil || env.ClaudeDir != home {
		ok, err := pr.confirm(fmt.Sprintf("The Claude Code kit isn't synced. Sync it into %s?", env.ClaudeDir))
		// Ctrl-D (the end of input) is a no, not a failure to ask again
		if errors.Is(err, io.EOF) {
			ok, err = false, nil
		}
		if err != nil {
			kitWarn(err)
			return
		}
		if !ok {
			declineKit(p)
			return
		}
	}
	if err := syncOffered(g, p, pr); err != nil {
		kitWarn(err)
	}
}

// syncOffered syncs a kit you were offered (an update's or install's), and
// remembers a no to confirmShared, which syncs nothing and leaves no ledger
func syncOffered(g *Globals, p *paths.Paths, pr *prompter) error {
	if err := syncKit(g, pr, false); err != nil {
		return err
	}
	if _, synced, err := kit.ReadLedger(p.KitLedger()); err == nil && !synced && pr != nil {
		declineKit(p)
	}

	return nil
}

// claudeLoggedIn says whether Claude Code is installed and logged in, which a
// kit sync needs (D72), and says when it will sync if not. An error is one
// checking it, not being logged out
func claudeLoggedIn() (bool, error) {
	s, err := checkClaude()
	if err != nil {
		return false, err
	}
	if s.Bin == "" || !s.LoggedIn {
		fmt.Println("The Claude Code kit syncs once Claude Code is logged in: run 'bud kit sync' then.")
		return false, nil
	}

	return true, nil
}

// declinedKit says whether you turned the kit down or took it out
func declinedKit(p *paths.Paths) bool { return hasMarker(p.KitDeclined()) }

// declineKit records a no, so updates stop offering the kit
func declineKit(p *paths.Paths) {
	if markKitDeclined(p) != nil {
		fmt.Println("Left the kit out; bud kit sync adds it.")
		return
	}
	fmt.Println("Left the kit out; bud update won't ask again, and bud kit sync adds it.")
}

// markKitDeclined writes the KitDeclined marker
func markKitDeclined(p *paths.Paths) error { return setMarker(p.KitDeclined()) }

// hasMarker says whether the empty marker file at path exists (a no that
// bud remembers, like KitDeclined and ClodDeclined)
func hasMarker(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// setMarker writes the empty marker file at path
func setMarker(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, nil, 0o644)
}

// remindCore says when bud-plugins' core.md changed since you last pasted it
// into claude.ai (JGA-179). A sync is when the checkout it reads moves on
func remindCore(k *kit.Kit, env kit.Env) {
	p, err := paths.Resolve()
	if err != nil || k.Account.Instructions == "" {
		return
	}
	if c, err := kit.ReadCore(k.Account.Instructions, env.ClaudeDir, p.CorePasted()); err == nil && c.Path != "" && !c.Pasted {
		fmt.Println(kit.CoreReminder + ".")
	}
}

// kitCore prints core.md, or with pasted records it as pasted (JGA-179)
func kitCore(args []string, pasted bool) error {
	if len(args) > 0 {
		return usageErrorf("kit core takes no arguments")
	}
	k, env, err := loadKit()
	if err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	c, err := kit.ReadCore(k.Account.Instructions, env.ClaudeDir, p.CorePasted())
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("Claude Code has no %s yet: run bud kit sync", k.Account.Instructions)
	}
	if !pasted {
		fmt.Print(c.Text)
		return nil
	}
	if err := kit.MarkCorePasted(p.CorePasted(), c); err != nil {
		return err
	}
	fmt.Println("Recorded core.md as pasted; bud says when it changes again.")

	return nil
}
