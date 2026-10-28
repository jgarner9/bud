package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
	"jgarner.dev/bud/internal/secrets"
)

func init() {
	RegisterCommand(Kit())
}

// Kit writes bud's Claude Code setup into Claude Code's user config (BASE.md
// 4.1): MCP servers, skills, subagents, slash commands and instructions
func Kit() *Command {
	var dryRun bool
	var url, cmd, repo, profiles string
	return &Command{
		Name:  "kit",
		Usage: "Syncs bud's Claude Code setup (MCP servers, skills, agents, commands) into Claude Code",
		Subs: []*Command{
			{
				Name:  "sync",
				Usage: "write the kit into Claude Code's user config; --dry-run only says what it would do",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&dryRun, "dry-run", false, "say what would change, change nothing")
				},
				Run: func(g *Globals, args []string) error { return kitSync(g, args, dryRun) },
			},
			{Name: "status", Usage: "per item: synced, not synced, changed, or skipped and why", Run: func(g *Globals, args []string) error { return kitStatus(args) }},
			{Name: "ls", Usage: "list what's in the kit", Run: func(_ *Globals, args []string) error { return kitLs(args) }},
			{
				Name:  "secret",
				Usage: "store a kit secret in the keychain (set NAME, asks for the value), or delete it (rm NAME)",
				Run:   func(_ *Globals, args []string) error { return kitSecret(args) },
			},
			{
				Name:  "add",
				Usage: "scaffold a kit entry in a bud checkout: add mcp --url URL|--cmd CMD <name>, or add skill|agent|command <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&url, "url", "", "an MCP server over HTTP at this URL")
					fs.StringVar(&cmd, "cmd", "", "an MCP server that runs this command (stdio)")
					fs.StringVar(&repo, "repo", "", "the bud checkout (default: the one bud dev link points at)")
				},
				Run: func(_ *Globals, args []string) error { return kitAdd(args, url, cmd, repo) },
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
				Usage: "pull an MCP server, skill, agent or command you set up by hand into the kit: adopt <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&repo, "repo", "", "the bud checkout (default: the one bud dev link points at)")
				},
				Run: func(_ *Globals, args []string) error { return kitAdopt(args, repo) },
			},
			{
				Name:  "enable",
				Usage: "turn a kit entry on for this machine: enable <name>; --profile sets which profiles an MCP entry is for, in the checkout",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&profiles, "profile", "", "comma-separated profiles (full,minimal) for the entry, written to the checkout")
					fs.StringVar(&repo, "repo", "", "the bud checkout, for --profile")
				},
				Run: func(_ *Globals, args []string) error { return kitToggle(args, true, profiles, repo) },
			},
			{
				Name:  "disable",
				Usage: "turn a kit entry off for this machine; the next sync removes it from Claude Code: disable <name>",
				Run:   func(_ *Globals, args []string) error { return kitToggle(args, false, "", "") },
			},
			{
				Name:  "env",
				Usage: "what a Claude Code session bud starts needs, as JSON: the kit's keychain secrets, and with --root the project's own MCP servers",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&repo, "root", "", "the project the session runs in")
				},
				Run: func(_ *Globals, args []string) error { return kitEnvCmd(args, repo) },
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

func kitAdd(args []string, url, cmd, repo string) error {
	if len(args) != 2 {
		return usageErrorf("kit add takes a kind and a name: add mcp|skill|agent|command|plugin <name>")
	}
	kind, name := args[0], args[1]
	if _, ok := kit.Kinds[kind]; !ok {
		return usageErrorf("kit add: %q isn't mcp, skill, agent, command or plugin", kind)
	}
	r, err := kitRepo(repo)
	if err != nil {
		return err
	}
	var path string
	if kind == "plugin" {
		if err := r.AddPlugin(name); err != nil {
			return err
		}
		fmt.Printf("Added the plugin %s; bud kit sync installs it.\n", name)
		return nil
	}
	if kind == "mcp" {
		path, err = r.AddMCP(name, url, cmd)
	} else {
		if url != "" || cmd != "" {
			return usageErrorf("--url and --cmd are for MCP servers")
		}
		path, err = r.Scaffold(kind, name)
	}
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
	kind, needed, err := r.Adopt(args[0], env.ClaudeDir, env.ClaudeJSON)
	if err != nil {
		return err
	}
	if err := kit.Own(env.Ledger, env.ClaudeDir, kind, args[0]); err != nil {
		return err
	}
	fmt.Printf("Adopted the %s %s into %s.\n", kind, args[0], r.Dir)
	if len(needed) > 0 {
		fmt.Printf("Its values became secrets, so none are in the repo. Store each before syncing:\n")
		for _, n := range needed {
			fmt.Printf("  bud kit secret set %s\n", n)
		}
	}

	return nil
}

// kitToggle turns an entry on or off for this machine (config.json's
// kit.disabled), or with profiles, sets the entry's profiles in the checkout
func kitToggle(args []string, on bool, profiles, repo string) error {
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
	if profiles != "" {
		r, err := kitRepo(repo)
		if err != nil {
			return err
		}
		list := strings.Split(profiles, ",")
		for _, pr := range list {
			if !slices.Contains(config.Profiles, pr) {
				return usageErrorf("--profile %q isn't one of %s", pr, strings.Join(config.Profiles, ", "))
			}
		}
		if err := r.SetProfiles(name, list); err != nil {
			return err
		}
		fmt.Printf("%s is for %s now, in %s.\n", name, strings.Join(list, " and "), r.Dir)
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

// installedPlugins asks claude which plugins are installed
// kitConnectors lists the claude.ai connectors Claude Code has, so sync can
// leave out the kit's entries they already provide (JGA-128). claude mcp list
// checks each server's health, so it takes a few seconds
var kitConnectors = func() ([]kit.Connector, error) {
	bin := claude.Find()
	if bin == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "mcp", "list").Output()
	if err != nil {
		return nil, err
	}

	return kit.ParseConnectors(string(out)), nil
}

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
	s := config.Defaults()
	var disabled []string
	if f, err := config.Load(p.MachineConfig()); err == nil {
		if loaded, err := f.Settings(); err == nil {
			s = loaded
		}
		disabled = kitDisabled(f)
	}

	return kit.Env{
		ClaudeDir:        dir,
		ClaudeJSON:       claudeJSONFor(dir),
		Ledger:           p.KitLedger(),
		Profile:          s.Profile,
		Claude:           kitClaude,
		SecretSet:        secrets.Has,
		InstalledPlugins: installedPlugins,
		Disabled:         disabled,
		Connectors:       kitConnectors,
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

// secretNames lists the $secret names the kit's entries for this profile
// use; required ones are those without optional_secrets
func secretNames(k *kit.Kit, profile string) (all, required []string) {
	seen := map[string]bool{}
	for _, m := range k.MCP {
		if !m.For(profile) {
			continue
		}
		add := func(v string, needed bool) {
			for _, name := range kit.SecretRefs(v) {
				if seen[name] {
					continue
				}
				seen[name] = true
				all = append(all, name)
				if needed {
					required = append(required, name)
				}
			}
		}
		// a secret in the URL is needed even when the entry's others are optional
		add(m.URL, true)
		for _, vals := range []map[string]string{m.Headers, m.Env} {
			for _, v := range vals {
				add(v, !m.OptionalSecrets)
			}
		}
	}
	slices.Sort(all)
	slices.Sort(required)

	return all, required
}

func kitSecret(args []string) error {
	if len(args) != 2 || (args[0] != "set" && args[0] != "rm") {
		return usageErrorf("kit secret takes set NAME or rm NAME")
	}
	name := args[1]
	if args[0] == "rm" {
		if err := secrets.Delete(name); err != nil {
			return fmt.Errorf("deleting %s from the keychain: %w", name, err)
		}
		fmt.Printf("Deleted %s from the keychain.\n", name)
		return nil
	}
	if !isTerminal(os.Stdin) {
		return fmt.Errorf("bud kit secret set asks for the value, so it needs a terminal")
	}
	value, err := readSecret(name)
	if err != nil {
		return err
	}
	if value == "" {
		fmt.Println("Nothing stored.")
		return nil
	}
	if err := secrets.Set(name, value); err != nil {
		return fmt.Errorf("storing %s: %w", name, err)
	}
	fmt.Printf("Stored %s in the keychain; bud kit sync picks it up.\n", name)

	return nil
}

// swappable in tests: asks for a secret on the terminal without echoing it
var readSecret = func(name string) (string, error) {
	fmt.Printf("%s (typing won't show): ", name)
	stty := func(arg string) {
		cmd := exec.Command("stty", arg)
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
	}
	stty("-echo")
	defer func() {
		stty("echo")
		fmt.Println()
	}()
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n == 0 || err != nil || buf[0] == '\n' || buf[0] == '\r' {
			break
		}
		line = append(line, buf[0])
	}

	return strings.TrimSpace(string(line)), nil
}

// keychainEnv is the kit's secrets that are in the keychain but not the
// environment: what a Claude Code process bud starts needs handed to it
func keychainEnv() (map[string]string, error) {
	k, env, err := loadKit()
	if err != nil {
		return nil, err
	}
	all, _ := secretNames(k, env.Profile)
	out := map[string]string{}
	for _, name := range all {
		if secrets.FromEnv(name) {
			continue
		}
		if v, ok := secrets.Get(name); ok {
			out[name] = v
		}
	}

	return out, nil
}

func kitEnvCmd(args []string, root string) error {
	if len(args) > 0 {
		return usageErrorf("kit env takes no arguments")
	}
	vars, err := keychainEnv()
	if err != nil {
		return err
	}
	out := map[string]any{"env": vars}
	if root != "" {
		if cfg, err := sessionMCP(root); err == nil && cfg != nil {
			out["mcp_config"] = string(cfg)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(data))

	return nil
}

// sizeMaxAge is how old a project's measured size may be before it's measured again
const sizeMaxAge = 24 * time.Hour

// projectSize is root's size from the registry, measured again when it's
// missing or a day old (and saved, when the project is registered)
func projectSize(root string) projects.Size {
	p, err := paths.Resolve()
	if err != nil {
		return projects.Size{}
	}
	r, err := projects.Load(p.Projects())
	if err == nil {
		if pr, ok := r.Get(root); ok && pr.Size != nil && time.Since(pr.Size.Measured) < sizeMaxAge {
			return *pr.Size
		}
	}
	size, _ := projects.Measure(root)
	if err == nil {
		if _, err := r.SetSize(root, size); err == nil {
			_ = r.Save()
		}
	}

	return size
}

// sessionMCP is the --mcp-config for kit entries that only load in some
// projects (large ones, JGA-117; web ones, JGA-131) when root is one; nil
// when there are none
func sessionMCP(root string) ([]byte, error) {
	k, env, err := loadKit()
	if err != nil {
		return nil, err
	}
	hasConditional := false
	for _, m := range k.MCP {
		hasConditional = hasConditional || m.When != nil
	}
	if !hasConditional {
		return nil, nil
	}
	s := config.Defaults()
	if p, err := paths.Resolve(); err == nil {
		if f, err := config.Load(p.MachineConfig()); err == nil {
			if loaded, err := f.Settings(); err == nil {
				s = loaded
			}
		}
	}
	size := projectSize(root)

	return k.SessionServers(env.Profile, kit.Project{Root: root, Files: size.Files, LOC: size.LOC}, s.Kit.LargeFiles, s.Kit.LargeLOC, env.Disabled, secrets.Has, func(bin string) bool {
		_, err := exec.LookPath(bin)
		return err == nil
	})
}

// askKitSecrets asks once for each required kit secret that isn't set and
// stores it in the keychain (bud install). Skipping one is fine: sync skips
// that server and says why
func askKitSecrets(pr *prompter) error {
	if pr == nil {
		return nil
	}
	k, env, err := loadKit()
	if err != nil {
		return err
	}
	// a claude.ai connector already gives Claude Code those entries
	if env.Connectors != nil {
		if connectors, err := env.Connectors(); err == nil {
			k.MCP = slices.DeleteFunc(slices.Clone(k.MCP), func(m kit.MCP) bool {
				_, provided := m.ProvidedBy(connectors)
				return provided
			})
		}
	}
	_, required := secretNames(k, env.Profile)
	for _, name := range required {
		if secrets.Has(name) {
			continue
		}
		ok, err := pr.confirm(fmt.Sprintf("The kit needs %s. Store it in the keychain now?", name))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		value, err := readSecret(name)
		if err != nil {
			return err
		}
		if value != "" {
			if err := secrets.Set(name, value); err != nil {
				fmt.Printf("Couldn't store %s: %v\n", name, err)
			}
		}
	}

	return nil
}
