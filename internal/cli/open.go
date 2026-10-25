package cli

func Open() *Command {
	cmd := &Command{
		Name:  "open",
		Flags: nil,
		Run:   open,
	}

	return cmd
}

func open(args []string) error {
	return nil
}
