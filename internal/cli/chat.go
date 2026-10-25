package cli

func Chat() *Command {
	cmd := &Command{
		Name:  "open",
		Flags: nil,
		Run:   chat,
	}

	return cmd
}

func chat(args []string) error {
	return nil
}
