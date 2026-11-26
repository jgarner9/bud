package cli

import (
	"flag"
	"os"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Chat())
}

// chatAgent is the agent chat runs as, from bud-ide (JGA-165)
const chatAgent = "bud-ide:chat"

// Chat opens OpenCode's own TUI from the shell, in a dir that's no project's
func Chat() *Command {
	var resume bool
	return &Command{
		Name:  "chat",
		Usage: "Chats with OpenCode, outside any project: chat [--resume]",
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
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	dir := p.Chat()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if resume {
		return usageErrorf("--resume is Claude Code's; OpenCode keeps one chat, which opens where it left off")
	}
	return execOpenCodeTUI(g, dir)
}
