package cli

func Update() *Command {
	cmd := &Command{
		Name:  "update",
		Usage: "Updates bud to the latest repo changes",
		Flags: nil,
		Run:   update,
		Subs:  nil,
	}

	return cmd
}

func update(args []string) error {
	return nil
}
