package cli

func Install() *Command {
	cmd := &Command{
		Name:  "open",
		Flags: nil,
		Run:   install,
	}

	return cmd
}

func install(args []string) error {
	return nil
}
