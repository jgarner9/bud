package cli

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/updater"
	"jgarner.dev/bud/internal/version"
)

func init() {
	RegisterCommand(Bootstrap())
}

// Bootstrap sets up bud on another machine over SSH (BASE.md 5.3): it runs
// the one-liner there, so nothing but ssh and curl (or wget) is needed
func Bootstrap() *Command {
	var withConfig bool
	return &Command{
		Name:  "bootstrap",
		Usage: "Installs bud on another machine over SSH: bud bootstrap [--config] user@host",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&withConfig, "config", false, "copy this machine's config.json across first (it holds no secrets)")
		},
		Run: func(g *Globals, args []string) error { return bootstrap(g, args, withConfig) },
	}
}

// swappable in tests: runs ssh with args on the terminal, stdin from in
var runSSH = func(in []byte, args ...string) error {
	cmd := exec.Command("ssh", args...)
	if in != nil {
		cmd.Stdin = strings.NewReader(string(in))
	} else {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr

	return cmd.Run()
}

const installURL = "https://raw.githubusercontent.com/" + updater.Repo + "/main/install.sh"

func bootstrap(g *Globals, args []string, withConfig bool) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageErrorf("bootstrap takes one host, like user@host")
	}
	host := args[0]
	// the profile defaults to minimal: a remote box is usually a server (5.3)
	profile := g.Profile
	if profile == "" {
		profile = "minimal"
	}

	if withConfig {
		p, err := paths.Resolve()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p.MachineConfig())
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("there's no %s to copy (every setting is the default)", p.MachineConfig())
		}
		if err != nil {
			return err
		}
		fmt.Printf("Copying %s to %s...\n", p.MachineConfig(), host)
		// the remote's own XDG_STATE_HOME, as install.sh reads it
		copyCmd := `d="${XDG_STATE_HOME:-$HOME/.local/state}/bud" && mkdir -p "$d" && cat >"$d/config.json"`
		if err := runSSH(data, host, "sh", "-c", shellQuote(copyCmd)); err != nil {
			return fmt.Errorf("copying config.json to %s: %w", host, err)
		}
	}

	// the same release as this bud, so both machines run the same config
	install := fmt.Sprintf("(curl -fsSL %s || wget -qO- %s) | sh -s -- --profile %s --yes", installURL, installURL, profile)
	if v := releaseVersion(); v != "" {
		install += " --version " + v
	}
	g.Logf("running on %s: %s", host, install)
	fmt.Printf("Installing bud on %s (%s profile)...\n", host, profile)
	// -t: the package manager may ask for a sudo password
	if err := runSSH(nil, "-t", host, "sh", "-c", shellQuote(install)); err != nil {
		return fmt.Errorf("installing on %s: %w", host, err)
	}

	return nil
}

// releaseVersion is install.sh's --version for this bud: its tag, "nightly"
// for a nightly, or "" (latest) for a development build
func releaseVersion() string {
	v, ok := version.Parse(version.String())
	switch {
	case !ok:
		return ""
	case len(v.Pre) > 0 && v.Pre[0] == "nightly":
		return updater.NightlyTag
	default:
		return "v" + strings.TrimPrefix(version.String(), "v")
	}
}

// shellQuote quotes s as one word for the remote shell
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
