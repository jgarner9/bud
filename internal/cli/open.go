package cli

func Open() *Command {
	cmd := &Command{
		Name:  "open",
		Usage: "Opens Bud to the dashboard",
		Flags: nil,
		Run:   open,
		Subs:  nil,
	}

	return cmd
}

func open(args []string) error {
	return nil
}
