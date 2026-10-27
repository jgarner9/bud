package cli

import (
	"errors"
	"fmt"

	"jgarner.dev/bud/internal/doctor"
)

func init() {
	RegisterCommand(Doctor())
}

func Doctor() *Command {
	cmd := &Command{
		Name:  "doctor",
		Usage: "Checks that bud's dependencies and config are healthy",
		Flags: nil,
		Run:   runDoctor,
		Subs:  nil,
	}

	return cmd
}

func runDoctor(args []string) error {
	if len(args) > 0 {
		return usageErrorf("doctor takes no arguments")
	}

	results := doctor.Run()
	for _, r := range results {
		fmt.Printf("%-4s  %-15s %s\n", r.Status, r.Name, r.Detail)
		if r.Fix != "" {
			fmt.Printf("      %-15s fix: %s\n", "", r.Fix)
		}
	}

	switch n := doctor.Failed(results); n {
	case 0:
		return nil
	case 1:
		return errors.New("doctor found 1 problem")
	default:
		return fmt.Errorf("doctor found %d problems", n)
	}
}
