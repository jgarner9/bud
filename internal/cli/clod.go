package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/paths"
)

// clod is your private Claude Code setup, a GitHub repo of your own (D89,
// D90): the MCP servers, settings and rules bud's sessions rely on, which bud
// leaves to Claude Code's own config (D77). It's the clod.repo setting, else
// <your gh login>/clod; one that doesn't exist yet is made from clodTemplate.
// bud only knows where it goes (~/clod) and that an install.sh at its root,
// which takes --yes, sets it up; what it sets up is the repo's business.
const clodTemplate = "jgarner9/clod-template"

// clodRecord is what bud writes once a clod's installer has run here
type clodRecord struct {
	Repo        string    `json:"repo,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

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
	// ghLoggedIn says whether gh can reach github.com as you; plain gh auth
	// status fails when any other account or host it knows has a problem
	ghLoggedIn = func() bool {
		return exec.Command("gh", "auth", "status", "--hostname", "github.com").Run() == nil
	}
	// ghLogin is the GitHub account gh is logged in to
	ghLogin = func() (string, error) {
		out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
		return strings.TrimSpace(string(out)), err
	}
	// ghRepoExists says whether repo exists and you can see it
	ghRepoExists = func(repo string) (bool, error) {
		out, err := exec.Command("gh", "repo", "view", repo, "--json", "name").CombinedOutput()
		if err == nil {
			return true, nil
		}
		if strings.Contains(string(out), "Could not resolve to a Repository") {
			return false, nil
		}
		return false, fmt.Errorf("gh repo view %s: %s", repo, strings.TrimSpace(string(out)))
	}
	// ghHasInstaller says whether repo has an install.sh at its root yet:
	// GitHub fills a repo made from a template a few seconds after making it
	ghHasInstaller = func(repo string) bool {
		return exec.Command("gh", "api", "repos/"+repo+"/contents/install.sh", "--silent").Run() == nil
	}
	// clodWait is how long to wait between looks at a repo just made
	clodWait = 2 * time.Second
	// installClodDeps installs what's missing of deps, after showing the command
	installClodDeps = func(deps []installer.Dep, pr *prompter, yes bool) error {
		return installDeps(installer.NewPlan(deps), pr, yes)
	}
)

// clodInstalled says whether a clod's installer has run here: bud's record
// with the checkout still there, or, from before bud kept one, the clod
// command that Jason's clod links into ~/.local/bin
func clodInstalled(p *paths.Paths) bool {
	if _, ok := readClodRecord(p); ok {
		if _, err := os.Stat(filepath.Join(p.Clod(), "install.sh")); err == nil {
			return true
		}
	}
	// Stat follows the link, so one left dangling (~/clod deleted) doesn't count
	if _, err := os.Stat(p.ClodBin()); err == nil {
		return true
	}
	_, err := lookPath("clod")
	return err == nil
}

// readClodRecord reads bud's record of the clod installed here
func readClodRecord(p *paths.Paths) (clodRecord, bool) {
	var r clodRecord
	data, err := os.ReadFile(p.ClodRecord())
	if err != nil || json.Unmarshal(data, &r) != nil {
		return r, false
	}
	return r, true
}

// writeClodRecord records that repo's installer ran, at the checkout's commit.
// git is asked only when ~/clod is a repo itself, or it would answer for one
// around it (a home directory kept in git)
func writeClodRecord(p *paths.Paths, repo string) error {
	r := clodRecord{Repo: repo, InstalledAt: time.Now().UTC()}
	if _, err := os.Stat(filepath.Join(p.Clod(), ".git")); err == nil {
		if repo == "" {
			// a checkout bud didn't clone: name it after where it came from
			if out, err := exec.Command("git", "-C", p.Clod(), "config", "--get", "remote.origin.url").Output(); err == nil {
				r.Repo = githubName(strings.TrimSpace(string(out)))
			}
		}
		if out, err := exec.Command("git", "-C", p.Clod(), "rev-parse", "--short", "HEAD").Output(); err == nil {
			r.Commit = strings.TrimSpace(string(out))
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(p.ClodRecord(), append(data, '\n'), 0o644)
}

// githubName is owner/name for a GitHub remote URL (https or ssh), so the
// record reads the same whether bud cloned the repo or you did; any other
// remote is kept as it is
func githubName(url string) string {
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:", "ssh://git@github.com/"} {
		if rest, ok := strings.CutPrefix(url, prefix); ok {
			return strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
		}
	}
	return url
}

// clodRepoFor is your clod: the clod.repo setting, else <gh login>/clod.
// gh has to be logged in for the second
func clodRepoFor(p *paths.Paths) (string, error) {
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return "", err
	}
	s, err := f.Settings()
	if err != nil {
		return "", err
	}
	if s.Clod.Repo != "" {
		return s.Clod.Repo, nil
	}
	login, err := ghLogin()
	if err != nil {
		return "", fmt.Errorf("couldn't tell which GitHub account gh is logged in to (%v); set clod.repo", err)
	}
	if login == "" {
		return "", errors.New("gh didn't say which GitHub account it's logged in to; set clod.repo")
	}
	return login + "/clod", nil
}

// offerClod sets clod up when it's missing and you haven't said no to it, for
// bud update. With nobody to ask it does nothing, and with Claude Code turned
// off (ai.claude.autostart), as bud install does, it leaves clod alone too
func offerClod(p *paths.Paths, pr *prompter) {
	if pr == nil || clodInstalled(p) || declinedClod(p) {
		return
	}
	if f, err := config.Load(p.MachineConfig()); err == nil {
		if s, err := f.Settings(); err == nil && !s.AI.Claude.Autostart {
			return
		}
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
		ok, err := pr.confirmYes("Set up clod, your Claude Code setup from GitHub?")
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

	repo := ""
	if cloning {
		if _, err := os.Stat(dir); err == nil {
			return fmt.Errorf("%s is there but has no install.sh; move it aside and run 'bud claude setup'", dir)
		}
		if !ghLoggedIn() {
			if pr == nil && !stdinTerminal() {
				return errors.New("clod is a private repo, so gh needs a GitHub login: run 'gh auth login' at a terminal, then 'bud claude setup'")
			}
			fmt.Println("clod is a private repo, so gh needs a GitHub login; handing over to it.")
			if err := runOnTerminal([]string{"gh", "auth", "login"}); err != nil {
				return fmt.Errorf("gh auth login: %w", err)
			}
		}
		if repo, err = clodRepoFor(p); err != nil {
			return err
		}
		exists, err := ghRepoExists(repo)
		if err != nil {
			return err
		}
		if !exists {
			if created, err := createClod(p, repo, pr); err != nil || !created {
				return err
			}
		}
		// cloned beside it and moved in whole, so a clone cut short never
		// leaves a ~/clod that the next run has to refuse
		part := dir + ".clone"
		os.RemoveAll(part)
		fmt.Printf("Cloning %s into %s.\n", repo, dir)
		if err := runOnTerminal([]string{"gh", "repo", "clone", repo, part}); err != nil {
			os.RemoveAll(part)
			return fmt.Errorf("gh repo clone %s: %w", repo, err)
		}
		if _, err := os.Stat(filepath.Join(part, "install.sh")); err != nil {
			os.RemoveAll(part)
			return fmt.Errorf("%s has no install.sh at its root, which is how bud sets a clod up (%s has one to start from)", repo, clodTemplate)
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
	os.Remove(p.ClodDeclined())
	if err := writeClodRecord(p, repo); err != nil {
		// clod is set up; only bud's note of it is missing, so it may ask again
		fmt.Printf("clod is set up, but bud couldn't note it (%v), so it may offer it again.\n", err)
	}

	return nil
}

// createClod makes repo, private, from clodTemplate. Making something on
// GitHub takes a yes typed at a terminal: Enter is no, and --yes never makes
// one, so bud bootstrap can't fill a work account. A no is a no to clod, so
// bud update stops asking. It says whether it made one
func createClod(p *paths.Paths, repo string, pr *prompter) (bool, error) {
	if pr == nil {
		return false, fmt.Errorf("%s doesn't exist or this GitHub account can't see it; run 'bud claude setup' at a terminal to create it from %s, or set clod.repo", repo, clodTemplate)
	}
	ok, err := pr.confirm(fmt.Sprintf("%s doesn't exist, or this GitHub account can't see it. Create it, private, from the %s starter?", repo, clodTemplate))
	if errors.Is(err, io.EOF) {
		ok, err = false, nil
	}
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Println("Nothing created; set clod.repo to use a repo you have.")
		declineClod(p)
		return false, nil
	}
	argv := []string{"gh", "repo", "create", repo, "--private", "--template", clodTemplate}
	fmt.Printf("Creating %s from %s.\n", repo, clodTemplate)
	if err := runOnTerminal(argv); err != nil {
		return false, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	fmt.Println("Waiting for GitHub to fill it from the template (a few seconds).")
	for range 15 {
		if ghHasInstaller(repo) {
			return true, nil
		}
		time.Sleep(clodWait)
	}
	return false, fmt.Errorf("made %s, but GitHub hasn't filled it from %s yet; run 'bud claude setup' in a minute", repo, clodTemplate)
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
