package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	backend "jgarner.dev/bud/internal/agent"
	"jgarner.dev/bud/internal/config"
)

func init() {
	RegisterCommand(AI())
}

// AI checks and sets up the backend bud's AI talks to over ACP (D131-D133):
// an OpenClaw Gateway by default (D132), or Claude Code, Codex, OpenCode or
// any ACP agent. Signing in to the models is each backend's own
func AI() *Command {
	var asJSON, yes bool
	return &Command{
		Name:  "ai",
		Usage: "Checks and sets up the AI backend (ai.backend: claude, openclaw, codex, opencode)",
		Subs: []*Command{
			{
				Name:  "status",
				Usage: "show which backend is set, whether it's installed, and for OpenClaw whether the Gateway answers",
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
				Usage: "install the backend's command if it's missing and, for OpenClaw, set the Gateway's URL and token file",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&yes, "yes", false, "install without asking and keep the settings as they are")
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
	Backend   string            `json:"backend"`
	Command   []string          `json:"command"`
	Bin       string            `json:"bin"`
	Version   string            `json:"version"`
	Gateway   string            `json:"gateway,omitempty"`
	Reachable bool              `json:"reachable"`
	Ready     bool              `json:"ready"`
	Problems  []backend.Problem `json:"problems"`
}

func aiStatus(asJSON bool) error {
	_, _, s, err := loadSettings()
	if err != nil {
		return err
	}
	st := aiCheck(s, false)
	if asJSON {
		out := aiStatusJSON{st.Backend, st.Argv, st.Bin, st.Version, st.Gateway, st.Reachable, st.Ready(), st.Problems}
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
	fmt.Printf("Backend: %s, %s\n", paint(out, ansiBold, st.Backend), state)
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
	if st.Gateway != "" {
		g := st.Gateway
		if st.Reachable {
			g += ", " + paint(out, ansiGreen, "answering")
		}
		fmt.Printf("Gateway: %s\n", g)
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

// npmInstall installs a backend's npm package, on the terminal so its
// output and any prompt show; swappable in tests
var npmInstall = func(pkg string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm isn't installed, which installs %s: install Node.js first", pkg)
	}
	return runOnTerminal([]string{"npm", "install", "-g", pkg})
}

func aiSetup(pr *prompter, yes bool) error {
	p, f, s, err := loadSettings()
	if err != nil {
		return err
	}
	if s.AI.Backend == "claude" {
		// Claude Code directly (D140): its own setup does it
		fmt.Println("The claude backend runs Claude Code directly; bud claude setup installs it and signs it in.")
		return nil
	}
	st := aiCheck(s, true)
	if st.Bin == "" && len(st.Argv) > 0 {
		hint := backend.Install[st.Backend]
		pkg := strings.TrimPrefix(hint, "npm install -g ")
		if hint == "" || pkg == hint {
			return fmt.Errorf("%s isn't installed; install it yourself, then run bud ai status", st.Argv[0])
		}
		ok := yes
		if !ok && pr != nil {
			if ok, err = pr.confirmYes(fmt.Sprintf("%s isn't installed. Install it with %s?", st.Argv[0], hint)); err != nil {
				return err
			}
		}
		if !ok {
			return fmt.Errorf("%s isn't installed: %s", st.Argv[0], hint)
		}
		if err := npmInstall(pkg); err != nil {
			return err
		}
	}
	if s.AI.Backend == "openclaw" && pr != nil && !yes {
		changed, err := askOpenClaw(pr, f, s.AI.OpenClaw)
		if err != nil {
			return err
		}
		if changed {
			if err := f.Save(); err != nil {
				return err
			}
			fmt.Printf("Saved to %s\n", p.MachineConfig())
			if s, err = f.Settings(); err != nil {
				return err
			}
		}
	}
	switch s.AI.Backend {
	case "openclaw":
		fmt.Println("Models sign in on the Gateway host: openclaw onboard there (Claude Code through the claude-cli runtime, ChatGPT Plus with --auth-choice openai-codex).")
	case "claude":
		fmt.Println("Claude Code signs in with its own login: run claude once and follow it.")
	case "codex":
		fmt.Println("Codex signs in with its own login (ChatGPT Plus): run codex once and follow it.")
	}
	return aiStatus(false)
}

// askOpenClaw asks for the Gateway's URL and token file, setting what
// changed in f; empty answers keep what's there
func askOpenClaw(pr *prompter, f *config.File, oc config.OpenClaw) (bool, error) {
	changed := false
	u, err := pr.ask("Gateway URL, such as wss://home.your-tailnet.ts.net:18789 (empty: the openclaw CLI's own)", oc.URL)
	if err != nil {
		return false, err
	}
	if u != oc.URL {
		if u != "" {
			parsed, perr := url.Parse(u)
			if perr != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Hostname() == "" {
				return false, fmt.Errorf("%q isn't a ws:// or wss:// URL", u)
			}
		}
		if err := f.Set("ai.openclaw.url", u); err != nil {
			return false, err
		}
		changed = true
	}
	def := oc.TokenFile
	if def == "" {
		def = "~/.config/openclaw/gateway-token"
	}
	tf, err := pr.ask("File holding the Gateway token (chmod 600; bud passes it to openclaw, never on a command line)", def)
	if err != nil {
		return false, err
	}
	if tf != oc.TokenFile {
		if err := f.Set("ai.openclaw.token_file", tf); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
