package cli

func Install() *Command {
	cmd := &Command{
		Name:  "install",
		Usage: "Installs the base items needed for Bud",
		Flags: nil,
		Run:   install,
		Subs:  nil,
	}

	return cmd
}

func install(args []string) error {
	return nil
}
