package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	backend "jgarner.dev/bud/internal/agent"
)

func init() {
	RegisterCommand(AI())
}

// AI checks and sets up OpenCode, the one agent bud's AI talks to over ACP
// (Suite D139). Signing in to the models is OpenCode's own
func AI() *Command {
	var asJSON, yes bool
	return &Command{
		Name:  "ai",
		Usage: "Checks and sets up OpenCode, the AI agent bud runs",
		Subs: []*Command{
			{
				Name:  "status",
				Usage: "show whether OpenCode is installed and which version",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&asJSON, "json", false, "print the status as JSON")
				},
				Run: func(_ *Globals, args []string) error {
					if len(args) > 0 {
						return usageErrorf("ai status takes no arguments")
					}
					return aiStatus(asJSON)
				},
			},
			{
				Name:  "setup",
				Usage: "install OpenCode if it's missing",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&yes, "yes", false, "install without asking")
					fs.BoolVar(&yes, "y", false, "short for -yes")
				},
				Run: func(_ *Globals, args []string) error {
					if len(args) > 0 {
						return usageErrorf("ai setup takes no arguments")
					}
					return aiSetup(terminalPrompter(yes), yes)
				},
			},
		},
	}
}

// aiCheck is swappable in tests
var aiCheck = backend.Check

// aiStatusJSON is what bud ai status --json prints
type aiStatusJSON struct {
	Command  []string          `json:"command"`
	Bin      string            `json:"bin"`
	Version  string            `json:"version"`
	Ready    bool              `json:"ready"`
	Problems []backend.Problem `json:"problems"`
}

func aiStatus(asJSON bool) error {
	st := aiCheck(false)
	if asJSON {
		out := aiStatusJSON{st.Argv, st.Bin, st.Version, st.Ready(), st.Problems}
		if out.Command == nil {
			out.Command = []string{}
		}
		if out.Problems == nil {
			out.Problems = []backend.Problem{}
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	out := os.Stdout
	state := paint(out, ansiGreen, "ready")
	if !st.Ready() {
		state = paint(out, ansiRed, "not ready")
	} else if len(st.Problems) > 0 {
		state = paint(out, ansiYellow, "ready, with warnings")
	}
	fmt.Printf("OpenCode: %s\n", state)
	if len(st.Argv) > 0 {
		fmt.Printf("Runs: %s\n", paint(out, ansiDim, strings.Join(st.Argv, " ")))
	}
	if st.Bin != "" {
		v := st.Version
		if v == "" {
			v = "version unknown"
		}
		fmt.Printf("Command: %s (%s)\n", v, paint(out, ansiDim, st.Bin))
	}
	for _, p := range st.Problems {
		color := ansiYellow
		if p.Hard {
			color = ansiRed
		}
		fmt.Printf("%s %s\n  %s\n", paint(out, color, "✗"), p.What, paint(out, ansiDim, "fix: "+p.Fix))
	}
	return nil
}

// npmInstall installs an npm package, on the terminal so its
// output and any prompt show; swappable in tests
var npmInstall = func(pkg string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm isn't installed, which installs %s: install Node.js first", pkg)
	}
	argv := []string{"npm", "install", "-g"}
	if pkg == backend.OpenCodePackage {
		// npm 12 blocks global lifecycle scripts unless explicitly allowed.
		argv = append(argv, "--allow-scripts=@opencode/cli")
	}
	return runOnTerminal(append(argv, pkg))
}

func aiSetup(pr *prompter, yes bool) error {
	if st := aiCheck(true); st.Bin == "" {
		ok := yes
		if !ok && pr != nil {
			var err error
			if ok, err = pr.confirmYes(fmt.Sprintf("OpenCode isn't installed. Install it with %s?", backend.InstallCommand)); err != nil {
				return err
			}
		}
		if !ok {
			return fmt.Errorf("opencode isn't installed: %s", backend.InstallCommand)
		}
		if err := npmInstall(backend.OpenCodePackage); err != nil {
			return err
		}
	}
	return aiStatus(false)
}
