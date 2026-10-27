package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"jgarner.dev/bud/internal/doctor"
)

func init() {
	RegisterCommand(Doctor())
}

func Doctor() *Command {
	var quick, asJSON bool
	return &Command{
		Name:  "doctor",
		Usage: "Checks that bud's dependencies and config are healthy",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&quick, "quick", false, "skip the checks that take a while (startup timing)")
			fs.BoolVar(&asJSON, "json", false, "print the results as JSON, for the dashboard")
		},
		Run: func(g *Globals, args []string) error { return runDoctor(args, quick, asJSON) },
	}
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
	w := os.Stdout
	for _, r := range results {
		// pad before painting, so the escape codes don't break the columns
		status := fmt.Sprintf("%-4s", r.Status)
		fmt.Fprintf(w, "%s  %-15s %s\n", paint(w, statusStyle(strings.ToLower(r.Status.String())), status), r.Name, r.Detail)
		if r.Fix != "" {
			fmt.Fprintf(w, "      %-15s %s\n", "", paint(w, ansiDim, "fix: "+r.Fix))
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
