package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Kit())
}

// Kit writes bud's Claude Code setup into Claude Code's user config (BASE.md
// 4.1): MCP servers, skills, subagents, slash commands and instructions
func Kit() *Command {
	var dryRun bool
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
		},
	}
}

// swappable in tests
var (
	kitFS     = bud.Kit
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

// kitEnv is where the kit goes on this machine
func kitEnv(p *paths.Paths) (kit.Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return kit.Env{}, err
	}
	dir, claudeJSON := filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dir, claudeJSON = d, filepath.Join(d, ".claude.json")
	}
	s := config.Defaults()
	if f, err := config.Load(p.MachineConfig()); err == nil {
		if loaded, err := f.Settings(); err == nil {
			s = loaded
		}
	}

	return kit.Env{
		ClaudeDir:  dir,
		ClaudeJSON: claudeJSON,
		Ledger:     p.KitLedger(),
		Profile:    s.Profile,
		Claude:     kitClaude,
		SecretSet:  func(name string) bool { return os.Getenv(name) != "" },
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
	k, env, err := loadKit()
	if err != nil {
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
		_, err = kit.Sync(k, env, false)
	}
	if err != nil {
		fmt.Printf("The Claude Code kit didn't sync (%v); bud kit sync tries again.\n", err)
		return
	}
	g.Logf("synced the Claude Code kit")
}
