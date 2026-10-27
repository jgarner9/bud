package cli

import "errors"

func init() {
	RegisterCommand(Chat())
}

// Chat will open full-screen chat with Claude Code (BASE.md 5.5, milestone 7).
// Until then it says so, rather than exiting 0 having done nothing
func Chat() *Command {
	return &Command{
		Name:  "chat",
		Usage: "Opens Bud chat (not built yet)",
		Run:   chat,
	}
}

func chat(g *Globals, args []string) error {
	return errors.New("chat isn't built yet: it comes with the AI layer (milestone 7)")
}
