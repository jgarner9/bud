package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Chat())
}

// chatAgent is the agent chat runs as, from bud-ide (JGA-165)
const chatAgent = "bud-ide:chat"

// Chat opens Claude Code's own TUI in chat mode from the shell, as the
// dashboard's c does in the editor (JGA-165): bud-ide's chat agent, in a dir
// that's no project's
func Chat() *Command {
	var resume bool
	return &Command{
		Name:  "chat",
		Usage: "Chats with Claude Code, in chat mode: chat [--resume]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&resume, "resume", false, "pick an earlier chat to carry on")
		},
		Run: func(g *Globals, args []string) error { return chat(g, args, resume) },
	}
}

// chatEnv is this environment without BUD_BIN, so bud-ide's SessionStart
// hook leaves out its notes on working inside bud (chat isn't a project)
func chatEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "BUD_BIN=") })
}

func chat(g *Globals, args []string, resume bool) error {
	if len(args) > 0 {
		return usageErrorf("chat takes no arguments; --resume picks an earlier chat")
	}
	bin := claude.Find()
	if bin == "" {
		return errors.New("Claude Code isn't installed: run bud claude setup")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	dir := p.Chat()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var flags []string
	if claude.PluginFile("bud-ide", "agents/chat.md") != "" {
		flags = append(flags, "--agent", chatAgent)
	} else {
		w := g.stderr
		if w == nil {
			w = os.Stderr
		}
		fmt.Fprintln(w, paint(w, ansiYellow, "bud: Claude Code doesn't have bud-ide's chat agent yet, so this is plain Claude Code: run bud kit sync"))
	}
	if resume {
		flags = append(flags, "--resume")
	}
	argv, err := tuiArgv(machineSettings(), bin, flags)
	if err != nil {
		return err
	}
	g.Logf("running %q in %s", argv, dir)

	return execClaude(dir, argv, chatEnv())
}
