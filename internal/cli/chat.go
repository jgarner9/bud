package cli

func Chat() *Command {
	cmd := &Command{
		Name:  "chat",
		Usage: "Opens bud to the chat interface",
		Flags: nil,
		Run:   chat,
		Subs:  nil,
	}

	return cmd
}

func chat(args []string) error {
	return nil
}
