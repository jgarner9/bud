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
	"sort"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Tools())
}

// Tools manages bud's tools (BASE.md 4 part 3): the ones bud ships, synced to
// ~/.config/bud/tools, and yours in user-tools/. bud.tools loads them; this
// keeps the list of disabled ones in config.json, the reason a broken one
// was turned off, and moves one of yours into a bud checkout to ship it
func Tools() *Command {
	var reason, repo string
	var yes bool
	return &Command{
		Name:  "tools",
		Usage: "Lists, enables, disables, removes and promotes bud's tools",
		Subs: []*Command{
			{Name: "ls", Usage: "list the tools, yours and bud's, and whether each is on", Run: func(_ *Globals, args []string) error { return toolsLs(args) }},
			{Name: "enable", Usage: "turn a tool on: enable <name>", Run: func(_ *Globals, args []string) error { return toolsEnable(args) }},
			{
				Name:  "disable",
				Usage: "turn a tool off: disable <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&reason, "reason", "", "why (the editor gives one when a tool fails to load)")
				},
				Run: func(_ *Globals, args []string) error { return toolsDisable(args, reason) },
			},
			{
				Name:  "rm",
				Usage: "delete one of your tools: rm <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&yes, "yes", false, "delete without asking")
				},
				Run: func(_ *Globals, args []string) error { return toolsRm(args, yes) },
			},
			{
				Name:  "promote",
				Usage: "move one of your tools into a bud checkout's tools/, commit and push it: promote <name>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&repo, "repo", "", "the bud checkout (default: the one bud dev link points at)")
					fs.BoolVar(&yes, "yes", false, "commit and push without asking")
				},
				Run: func(_ *Globals, args []string) error { return toolsPromote(args, repo, yes) },
			},
		},
	}
}

// tool is one tool as ls shows it
type tool struct {
	Name, Description, Dir string
	Yours                  bool
}

// toolNames that aren't tools in user-tools/
var reservedTools = []string{"languages"}

// findTools lists bud's synced tools and yours, yours winning on a name clash,
// the same rule bud.tools uses
func findTools(p *paths.Paths) ([]tool, error) {
	byName := map[string]tool{}
	for _, place := range []struct {
		dir   string
		yours bool
	}{{p.Tools(), false}, {p.UserTools(), true}} {
		entries, err := os.ReadDir(place.dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if slices.Contains(reservedTools, name) || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			dir := filepath.Join(place.dir, name)
			t := tool{Name: name, Dir: dir, Yours: place.yours}
			data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil && !isFile(filepath.Join(dir, "init.lua")) {
				continue
			}
			var m struct {
				Description string `json:"description"`
			}
			if json.Unmarshal(data, &m) == nil {
				t.Description = m.Description
			}
			byName[name] = t
		}
	}
	var out []tool
	for _, t := range byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// toolsEnv is what the tools commands share
type toolsEnv struct {
	p        *paths.Paths
	f        *config.File
	disabled []string
	reasons  map[string]string
	tools    []tool
}

func loadToolsEnv() (*toolsEnv, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return nil, err
	}
	e := &toolsEnv{p: p, f: f, reasons: map[string]string{}}
	if v, ok := f.Get("tools.disabled"); ok {
		list, _ := v.([]any)
		for _, name := range list {
			if s, ok := name.(string); ok {
				e.disabled = append(e.disabled, s)
			}
		}
	}
	if data, err := os.ReadFile(p.ToolsState()); err == nil {
		var st struct {
			Broken map[string]string `json:"broken"`
		}
		if json.Unmarshal(data, &st) == nil && st.Broken != nil {
			e.reasons = st.Broken
		}
	}
	if e.tools, err = findTools(p); err != nil {
		return nil, err
	}

	return e, nil
}

func (e *toolsEnv) find(name string) (tool, error) {
	for _, t := range e.tools {
		if t.Name == name {
			return t, nil
		}
	}
	var names []string
	for _, t := range e.tools {
		names = append(names, t.Name)
	}
	if len(names) == 0 {
		return tool{}, usageErrorf("no tool %q (there are none yet)", name)
	}

	return tool{}, usageErrorf("no tool %q (have %s)", name, strings.Join(names, ", "))
}

// save writes the disabled list and the reasons
func (e *toolsEnv) save() error {
	slices.Sort(e.disabled)
	list := make([]any, len(e.disabled))
	for i, n := range e.disabled {
		list[i] = n
	}
	if err := e.f.Set("tools.disabled", list); err != nil {
		return err
	}
	if err := e.f.Save(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"broken": e.reasons}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.p.ToolsState()), 0o755); err != nil {
		return err
	}

	return atomicfile.WriteFile(e.p.ToolsState(), data, 0o644)
}

func oneName(args []string, cmd string) (string, error) {
	if len(args) != 1 {
		return "", usageErrorf("tools %s takes one tool name", cmd)
	}

	return args[0], nil
}

func toolsLs(args []string) error {
	if len(args) > 0 {
		return usageErrorf("tools ls takes no arguments")
	}
	e, err := loadToolsEnv()
	if err != nil {
		return err
	}
	if len(e.tools) == 0 {
		fmt.Println("No tools yet. :BudGen in the editor makes one.")
		return nil
	}
	width := 0
	for _, t := range e.tools {
		width = max(width, len(t.Name))
	}
	for _, t := range e.tools {
		state := "on"
		if slices.Contains(e.disabled, t.Name) {
			state = "off"
		}
		whose := "bud's"
		if t.Yours {
			whose = "yours"
		}
		line := fmt.Sprintf("%-*s  %-3s  %-5s  %s", width, t.Name, state, whose, t.Description)
		if why := e.reasons[t.Name]; why != "" && state == "off" {
			line += "  (broken: " + why + ")"
		}
		fmt.Println(strings.TrimRight(line, " "))
	}

	return nil
}

func toolsEnable(args []string) error {
	name, err := oneName(args, "enable")
	if err != nil {
		return err
	}
	e, err := loadToolsEnv()
	if err != nil {
		return err
	}
	if _, err := e.find(name); err != nil {
		return err
	}
	e.disabled = slices.DeleteFunc(e.disabled, func(n string) bool { return n == name })
	delete(e.reasons, name)
	if err := e.save(); err != nil {
		return err
	}
	fmt.Printf("%s is on; it loads the next time bud starts.\n", name)

	return nil
}

func toolsDisable(args []string, reason string) error {
	name, err := oneName(args, "disable")
	if err != nil {
		return err
	}
	e, err := loadToolsEnv()
	if err != nil {
		return err
	}
	if _, err := e.find(name); err != nil {
		return err
	}
	if !slices.Contains(e.disabled, name) {
		e.disabled = append(e.disabled, name)
	}
	if reason != "" {
		e.reasons[name] = reason
	}
	if err := e.save(); err != nil {
		return err
	}
	fmt.Printf("%s is off.\n", name)

	return nil
}

func toolsRm(args []string, yes bool) error {
	name, err := oneName(args, "rm")
	if err != nil {
		return err
	}
	e, err := loadToolsEnv()
	if err != nil {
		return err
	}
	t, err := e.find(name)
	if err != nil {
		return err
	}
	if !t.Yours {
		return fmt.Errorf("%s ships with bud; turn it off with bud tools disable %s", name, name)
	}
	fmt.Printf("bud tools rm will delete %s\n", t.Dir)
	err = confirmOnTerminal("Continue?", yes, "bud tools rm --yes "+name)
	if errors.Is(err, errNotConfirmed) {
		fmt.Println("Nothing removed.")
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.RemoveAll(t.Dir); err != nil {
		return err
	}
	e.disabled = slices.DeleteFunc(e.disabled, func(n string) bool { return n == name })
	delete(e.reasons, name)
	if err := e.save(); err != nil {
		return err
	}
	fmt.Printf("Removed %s.\n", name)

	return nil
}

// swappable in tests: runs git in dir on the terminal
var runGit = func(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// toolsPromote moves one of your tools into a bud checkout's tools/ and
// commits and pushes it, so it ships in the next release. Only after showing
// what it will do and you saying yes (BASE.md 4: nothing is promoted without
// an explicit accept)
func toolsPromote(args []string, repo string, yes bool) error {
	name, err := oneName(args, "promote")
	if err != nil {
		return err
	}
	e, err := loadToolsEnv()
	if err != nil {
		return err
	}
	t, err := e.find(name)
	if err != nil {
		return err
	}
	if !t.Yours {
		return fmt.Errorf("%s already ships with bud", name)
	}
	if repo == "" {
		checkout, ok := nvim.DevLink(e.p.Config)
		if !ok {
			return usageErrorf("promote needs a bud checkout: --repo <path>, or bud dev link <path> first")
		}
		repo = checkout
	}
	if !isFile(filepath.Join(repo, "embed.go")) {
		return fmt.Errorf("%s isn't a bud checkout (no embed.go)", repo)
	}
	dest := filepath.Join(repo, "tools", name)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}

	msg := fmt.Sprintf("feat: promote the %s tool", name)
	fmt.Printf("bud tools promote will:\n  move %s\n    to %s\n  git commit -m %q\n  git push\n", t.Dir, dest, msg)
	err = confirmOnTerminal("Continue?", yes, "bud tools promote --yes "+name)
	if errors.Is(err, errNotConfirmed) {
		fmt.Println("Nothing changed.")
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.Rename(t.Dir, dest); err != nil {
		return err
	}
	for _, git := range [][]string{{"add", "tools/" + name}, {"commit", "-m", msg, "--", "tools/" + name}, {"push"}} {
		if err := runGit(repo, git...); err != nil {
			return fmt.Errorf("git %s: %w (the tool is in %s now)", strings.Join(git, " "), err, dest)
		}
	}
	fmt.Printf("Promoted %s: it ships with the next release.\n", name)

	return nil
}
