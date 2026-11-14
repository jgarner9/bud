package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"

	"jgarner.dev/bud/internal/installer"
)

// the GitHub CLI is optional: the dashboard's open-PRs panel uses it
// (nvim/lua/bud/dashboard), and nothing else in bud needs it
var ghDep = installer.Dep{Name: "gh", Bins: []string{"gh"}, Pkgs: map[string][]string{"pacman": {"github-cli"}}}

// swappable in tests
var (
	// githubSetup is setupGitHub; TestMain turns it off for every other cli
	// test, so none reaches a package manager or GitHub
	githubSetup = setupGitHub
	// ghLoggedIn says whether gh can reach github.com as you; plain gh auth
	// status fails when any other account or host it knows has a problem
	ghLoggedIn = func() bool {
		return exec.Command("gh", "auth", "status", "--hostname", "github.com").Run() == nil
	}
)

// setupGitHub installs gh when it's missing, after showing the command, and
// offers to log in to it (gh's own login, at a terminal). It is for the full
// profile only, and only warns on failure: the editor works without gh
func setupGitHub(pr *prompter, yes bool, profile string) error {
	if profile == "minimal" {
		return nil
	}
	if missing := installer.Missing([]installer.Dep{ghDep}); len(missing) > 0 {
		if err := installDepsShown(missing, pr, yes); err != nil {
			return err
		}
		if installer.Missing([]installer.Dep{ghDep}) != nil {
			return errors.New("gh is still missing")
		}
	}
	if ghLoggedIn() || pr == nil {
		return nil
	}
	ok, err := pr.confirmYes("Log in to GitHub with gh (the dashboard's open pull requests need it)?")
	// Ctrl-D (the end of input) is a no
	if errors.Is(err, io.EOF) {
		ok, err = false, nil
	}
	if err != nil || !ok {
		return err
	}
	if err := runOnTerminal([]string{"gh", "auth", "login"}); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}
	return nil
}
