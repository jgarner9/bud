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

func chat(args []string) error {
	return nil
}
