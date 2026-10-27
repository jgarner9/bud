package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"jgarner.dev/bud/internal/doctor"
)

func init() {
	RegisterCommand(Doctor())
}

func Doctor() *Command {
	var quick, asJSON bool
	cmd := &Command{
		Name:  "doctor",
		Usage: "Checks that bud's dependencies and config are healthy",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&quick, "quick", false, "skip the checks that take a while (startup timing)")
			fs.BoolVar(&asJSON, "json", false, "print the results as JSON, for the dashboard")
		},
		Run:  func(g *Globals, args []string) error { return runDoctor(args, quick, asJSON) },
		Subs: nil,
	}

	return cmd
}

func runDoctor(args []string, quick, asJSON bool) error {
	if len(args) > 0 {
		return usageErrorf("doctor takes no arguments")
	}

	results := doctor.Run(doctor.Options{Quick: quick})
	if asJSON {
		return reportJSON(results)
	}

	return report(results)
}

// reportJSON prints one object per result, with status as text ("ok", "warn",
// "fail"). It exits 0 whatever the results: the statuses say what's wrong, and
// the dashboard reading them shouldn't have to tell a failing check from bud failing
func reportJSON(results []doctor.Result) error {
	type out struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
		Fix    string `json:"fix,omitempty"`
	}
	list := make([]out, 0, len(results))
	for _, r := range results {
		list = append(list, out{r.Name, strings.ToLower(r.Status.String()), r.Detail, r.Fix})
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	fmt.Println(string(data))

	return nil
}

// report prints doctor results and returns an error when any of them failed
func report(results []doctor.Result) error {
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
