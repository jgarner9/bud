package cli

import (
	"flag"
	"os"

	"jgarner.dev/bud/internal/nvim"
)

func init() {
	RegisterCommand(Chat())
}

// Chat opens full-screen chat with Claude Code (BASE.md 5.5): bud's Neovim,
// straight into bud.chat instead of the dashboard
func Chat() *Command {
	var resume string
	return &Command{
		Name:  "chat",
		Usage: "Opens Bud chat, full screen: chat [--resume <title>]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&resume, "resume", "", "carry on the chat whose title contains this (default: the last one)")
		},
		Run: func(g *Globals, args []string) error { return chat(g, args, resume) },
	}
}

// swappable in tests
var launchNvim = nvim.Launch

func chat(g *Globals, args []string, resume string) error {
	if len(args) > 0 {
		return usageErrorf("chat takes no arguments; --resume <title> picks a chat")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// nvim.Launch hands this environment on to nvim
	os.Setenv("BUD_OPEN", "chat")
	if resume != "" {
		os.Setenv("BUD_CHAT_RESUME", resume)
	}
	g.Logf("opening chat (resume %q)", resume)

	return launchNvim(home, nil, "", false)
}
