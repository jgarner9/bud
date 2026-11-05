package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/paths"
)

// clod is Jason's private Claude Code setup (D89): the MCP servers, settings
// and rules bud's sessions rely on, which bud leaves to Claude Code's own
// config (D77). bud only knows where clod lives and how to start its
// installer; what it sets up, and how, is clod's business.
const clodRepo = "jgarner9/clod"

// what fetching and running clod needs beyond bud's own deps (git among
// them): gh to clone a private repo and log in to GitHub for it, and bash and
// jq for its installer
var (
	ghDep       = installer.Dep{Name: "gh", Bins: []string{"gh"}, Pkgs: map[string][]string{"pacman": {"github-cli"}}}
	clodRunDeps = []installer.Dep{{Name: "bash", Bins: []string{"bash"}}, {Name: "jq", Bins: []string{"jq"}}}
)

// swappable in tests
var (
	// clodSetup is setupClod; every cli test but clod's own turns it off
	// (TestMain), so none of them can reach a package manager or GitHub
	clodSetup = setupClod
	// stdinTerminal says whether someone is at a terminal to log in to
	// GitHub: --yes skips bud's questions, not a login (bud bootstrap's ssh -t)
	stdinTerminal = func() bool { return isTerminal(os.Stdin) }
	// ghLoggedIn says whether gh can reach GitHub as you
	ghLoggedIn = func() bool {
		return exec.Command("gh", "auth", "status").Run() == nil
	}
	// installClodDeps installs what's missing of deps, after showing the command
	installClodDeps = func(deps []installer.Dep, pr *prompter, yes bool) error {
		return installDeps(installer.NewPlan(deps), pr, yes)
	}
)

// clodInstalled says whether clod's installer has run here: it links the
// clod command into ~/.local/bin
func clodInstalled(p *paths.Paths) bool {
	// Stat follows the link, so one left dangling (~/clod deleted) doesn't count
	if _, err := os.Stat(p.ClodBin()); err == nil {
		return true
	}
	_, err := lookPath("clod")
	return err == nil
}

// offerClod sets clod up when it's missing and you haven't said no to it, for
// bud update. With nobody to ask it does nothing
func offerClod(p *paths.Paths, pr *prompter) {
	if pr == nil || clodInstalled(p) || declinedClod(p) {
		return
	}
	if err := clodSetup(p, pr, false); err != nil {
		clodWarn(err)
	}
}

// setupClod clones clod and runs its installer when it isn't set up here,
// after asking (Enter is yes; --yes skips it). It waits for Claude Code to be
// installed, since clod's installer adds its servers through claude. A no is
// remembered, so bud update stops asking; bud install and bud claude setup,
// which are setting things up, ask again
func setupClod(p *paths.Paths, pr *prompter, yes bool) error {
	if clodInstalled(p) {
		return nil
	}
	if s, err := checkClaude(); err != nil {
		return err
	} else if s.Bin == "" {
		fmt.Println("clod sets up once Claude Code is installed: run 'bud claude setup' then.")
		return nil
	}

	if !yes {
		if pr == nil {
			fmt.Println("clod, your Claude Code setup, isn't here; run 'bud claude setup' at a terminal.")
			return nil
		}
		ok, err := pr.confirmYes(fmt.Sprintf("Set up clod (%s), your Claude Code setup?", clodRepo))
		// Ctrl-D (the end of input) is a no, not a failure to ask again
		if errors.Is(err, io.EOF) {
			ok, err = false, nil
		}
		if err != nil {
			return err
		}
		if !ok {
			declineClod(p)
			return nil
		}
	}

	dir := p.Clod()
	script := filepath.Join(dir, "install.sh")
	_, err := os.Stat(script)
	cloning := err != nil
	deps := clodRunDeps
	if cloning {
		deps = append([]installer.Dep{ghDep}, clodRunDeps...)
	}
	if missing := installer.Missing(deps); len(missing) > 0 {
		if err := installClodDeps(missing, pr, yes); err != nil {
			return err
		}
		if still := installer.Missing(deps); len(still) > 0 {
			return installer.NewPlan(still).Err()
		}
	}

	if cloning {
		if _, err := os.Stat(dir); err == nil {
			return fmt.Errorf("%s is there but has no install.sh; move it aside and run 'bud claude setup'", dir)
		}
		if !ghLoggedIn() {
			if pr == nil && !stdinTerminal() {
				return errors.New("clod is private, so gh needs a GitHub login: run 'gh auth login' at a terminal, then 'bud claude setup'")
			}
			fmt.Println("clod is private, so gh needs a GitHub login; handing over to it.")
			if err := runOnTerminal([]string{"gh", "auth", "login"}); err != nil {
				return fmt.Errorf("gh auth login: %w", err)
			}
		}
		// cloned beside it and moved in whole, so a clone cut short never
		// leaves a ~/clod that the next run has to refuse
		part := dir + ".clone"
		os.RemoveAll(part)
		fmt.Printf("Cloning %s into %s.\n", clodRepo, dir)
		if err := runOnTerminal([]string{"gh", "repo", "clone", clodRepo, part}); err != nil {
			os.RemoveAll(part)
			return fmt.Errorf("gh repo clone %s: %w", clodRepo, err)
		}
		if err := os.Rename(part, dir); err != nil {
			os.RemoveAll(part)
			return err
		}
	}

	argv := []string{script}
	if yes {
		argv = append(argv, "--yes")
	}
	fmt.Println("Handing over to clod's installer.")
	if err := runOnTerminal(argv); err != nil {
		return fmt.Errorf("%s: %w", script, err)
	}
	if !clodInstalled(p) {
		return fmt.Errorf("clod's installer finished but %s isn't there", p.ClodBin())
	}
	os.Remove(p.ClodDeclined())

	return nil
}

// declinedClod says whether you said no to setting clod up
func declinedClod(p *paths.Paths) bool { return hasMarker(p.ClodDeclined()) }

// declineClod records a no, so updates stop offering clod
func declineClod(p *paths.Paths) {
	if setMarker(p.ClodDeclined()) != nil {
		fmt.Println("Left clod out; bud claude setup sets it up.")
		return
	}
	fmt.Println("Left clod out; bud update won't ask again, and bud claude setup sets it up.")
}

// clodWarn says clod isn't set up and how to try again; bud works without it
func clodWarn(err error) {
	fmt.Printf("clod isn't set up (%v); 'bud claude setup' tries again.\n", err)
}
