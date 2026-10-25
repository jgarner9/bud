package cli

func Update() *Command {
	cmd := &Command{
		Name:  "open",
		Flags: nil,
		Run:   update,
	}

	return cmd
}

func update(args []string) error {
	return nil
}
