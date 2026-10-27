package cli

func init() {
	RegisterCommand(Chat())
}

func Chat() *Command {
	cmd := &Command{
		Name:  "chat",
		Usage: "Opens Bud chat interface",
		Flags: nil,
		Run:   chat,
		Subs:  nil,
	}

	return cmd
}

func chat(g *Globals, args []string) error {
	return nil
}
