package cli

import (
	"flag"
	"os"

	backend "jgarner.dev/bud/internal/agent"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Chat())
}

// chatAgent is the synced agent chat runs as (content/agents/chat.md): it
// reads, searches and looks things up, but edits nothing
const chatAgent = "chat"

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
		return usageErrorf("--resume isn't supported: OpenCode keeps one chat, which opens where it left off")
	}
	return execOpenCode(g, backend.ChatTUI(dir, chatAgent), dir)
}
