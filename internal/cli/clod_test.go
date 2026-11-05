package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/paths"
)

// clodEnv is a machine for setupClod: a temp HOME, a PATH holding only bins,
// Claude Code as status, and the package manager and gh login swapped out.
// Running clod's install.sh links the clod command, as the real one does
type clodEnv struct {
	p        *paths.Paths
	ran      *[][]string
	deps     []string
	loggedIn bool
	login    string          // the GitHub account gh is logged in to
	repos    map[string]bool // the repos that exist on GitHub
}

func newClodEnv(t *testing.T, bins ...string) *clodEnv {
	// bash is on every machine bud runs on; the tests choose the rest
	bins = append(bins, "bash")
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	bin := t.TempDir()
	for _, b := range bins {
		if err := os.WriteFile(filepath.Join(bin, b), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}

	e := &clodEnv{p: p, login: "jgarner9", repos: map[string]bool{"jgarner9/clod": true}}
	e.ran = fakeClaude(t, claudeReady)
	run := runOnTerminal
	runOnTerminal = func(argv []string) error {
		switch {
		case strings.HasSuffix(argv[0], "install.sh"):
			os.MkdirAll(p.BinDir, 0o755)
			os.WriteFile(p.ClodBin(), nil, 0o755)
		case len(argv) > 3 && argv[1] == "repo" && argv[2] == "create":
			e.repos[argv[3]] = true
		case len(argv) == 5 && argv[1] == "repo" && argv[2] == "clone":
			os.MkdirAll(argv[4], 0o755)
			os.WriteFile(filepath.Join(argv[4], "install.sh"), []byte("#!/bin/sh\n"), 0o755)
		}
		return run(argv)
	}
	// TestMain turns clod's setup off for every other test
	origSetup, origDeps, origGh, origTerm := clodSetup, installClodDeps, ghLoggedIn, stdinTerminal
	origLogin, origExists, origHas, origWait := ghLogin, ghRepoExists, ghHasInstaller, clodWait
	clodSetup = setupClod
	stdinTerminal = func() bool { return false }
	ghLogin = func() (string, error) { return e.login, nil }
	ghRepoExists = func(repo string) (bool, error) { return e.repos[repo], nil }
	ghHasInstaller = func(repo string) bool { return e.repos[repo] }
	clodWait = 0
	installClodDeps = func(deps []installer.Dep, _ *prompter, _ bool) error {
		for _, d := range deps {
			e.deps = append(e.deps, d.Name)
			os.WriteFile(filepath.Join(bin, d.Bins[0]), []byte("#!/bin/sh\n"), 0o755)
		}
		return nil
	}
	ghLoggedIn = func() bool { return e.loggedIn }
	t.Cleanup(func() {
		clodSetup, installClodDeps, ghLoggedIn, stdinTerminal = origSetup, origDeps, origGh, origTerm
		ghLogin, ghRepoExists, ghHasInstaller, clodWait = origLogin, origExists, origHas, origWait
	})

	return e
}

// checkout puts a clod checkout with an install.sh in ~/clod
func (e *clodEnv) checkout(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(e.p.Clod(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.p.Clod(), "install.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (e *clodEnv) commands() []string {
	var out []string
	for _, argv := range *e.ran {
		out = append(out, strings.Join(argv, " "))
	}
	return out
}

// a fresh machine: Enter says yes, gh and jq go in, gh logs in, clod is
// cloned into ~/clod and its installer runs
func TestSetupClodFresh(t *testing.T) {
	e := newClodEnv(t)
	var err error
	out := captureStdout(t, func() { err = setupClod(e.p, yesPrompter("\n"), false) })
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !slices.Equal(e.deps, []string{"gh", "jq"}) {
		t.Fatalf("deps %q", e.deps)
	}
	want := []string{
		"gh auth login",
		"gh repo clone jgarner9/clod " + e.p.Clod() + ".clone",
		filepath.Join(e.p.Clod(), "install.sh"),
	}
	if !slices.Equal(e.commands(), want) {
		t.Fatalf("ran %q, want %q", e.commands(), want)
	}
	if r, ok := readClodRecord(e.p); !ok || r.Repo != "jgarner9/clod" || !clodInstalled(e.p) {
		t.Fatalf("record %+v (%v), installed %v", r, ok, clodInstalled(e.p))
	}
}

// someone without a clod: theirs is made from the template, then set up
func TestSetupClodCreates(t *testing.T) {
	e := newClodEnv(t, "gh", "jq")
	e.loggedIn, e.login = true, "alice"
	var err error
	out := captureStdout(t, func() { err = setupClod(e.p, yesPrompter("\ny\n"), false) })
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := []string{
		"gh repo create alice/clod --private --template " + clodTemplate,
		"gh repo clone alice/clod " + e.p.Clod() + ".clone",
		filepath.Join(e.p.Clod(), "install.sh"),
	}
	if !slices.Equal(e.commands(), want) {
		t.Fatalf("ran %q, want %q", e.commands(), want)
	}
	if r, _ := readClodRecord(e.p); r.Repo != "alice/clod" {
		t.Fatalf("record %+v", r)
	}

	// Enter is no to making a repo, and a no is a no to clod, so updates stop asking
	e = newClodEnv(t, "gh", "jq")
	e.loggedIn, e.login = true, "bob"
	out = captureStdout(t, func() { err = setupClod(e.p, yesPrompter("\n\n"), false) })
	if err != nil || len(*e.ran) != 0 || !declinedClod(e.p) || !strings.Contains(out, "Nothing created") {
		t.Fatalf("err %v, ran %q, declined %v, out %q", err, e.commands(), declinedClod(e.p), out)
	}

	// --yes (bud bootstrap) never makes a repo on GitHub
	e = newClodEnv(t, "gh", "jq")
	e.loggedIn, e.login = true, "carol"
	err = setupClod(e.p, nil, true)
	if err == nil || !strings.Contains(err.Error(), "bud claude setup") || len(*e.ran) != 0 {
		t.Fatalf("err %v, ran %q", err, e.commands())
	}
}

// clod.repo picks the repo, whoever gh is logged in as
func TestSetupClodRepoSetting(t *testing.T) {
	e := newClodEnv(t, "gh", "jq")
	e.loggedIn, e.login = true, "work-account"
	e.repos["me/claude-setup"] = true
	os.MkdirAll(e.p.State, 0o755)
	os.WriteFile(e.p.MachineConfig(), []byte(`{"clod": {"repo": "me/claude-setup"}}`), 0o644)
	if err := setupClod(e.p, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := e.commands(); len(got) == 0 || got[0] != "gh repo clone me/claude-setup "+e.p.Clod()+".clone" {
		t.Fatalf("ran %q", got)
	}
}

// a repo with no install.sh at its root isn't moved in
func TestSetupClodNoInstaller(t *testing.T) {
	e := newClodEnv(t, "gh", "jq")
	e.loggedIn = true
	run := runOnTerminal
	runOnTerminal = func(argv []string) error {
		if len(argv) == 5 && argv[2] == "clone" {
			return os.MkdirAll(argv[4], 0o755) // an empty repo
		}
		return run(argv)
	}
	err := setupClod(e.p, nil, true)
	if err == nil || !strings.Contains(err.Error(), "no install.sh") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(e.p.Clod()); err == nil {
		t.Fatal("an empty clone was moved into ~/clod")
	}
}

// bud's record counts only while the checkout is there
func TestClodInstalledRecord(t *testing.T) {
	e := newClodEnv(t)
	e.checkout(t)
	if err := writeClodRecord(e.p, "me/clod"); err != nil {
		t.Fatal(err)
	}
	if !clodInstalled(e.p) {
		t.Fatal("record and checkout: not installed")
	}
	os.RemoveAll(e.p.Clod())
	if clodInstalled(e.p) {
		t.Fatal("record without the checkout counts as installed")
	}
}

// with a checkout and --yes: no question, no gh, the installer runs with --yes
func TestSetupClodCheckoutYes(t *testing.T) {
	e := newClodEnv(t, "jq")
	e.checkout(t)
	if err := setupClod(e.p, nil, true); err != nil {
		t.Fatal(err)
	}
	if len(e.deps) != 0 {
		t.Fatalf("installed %q, want nothing", e.deps)
	}
	if want := []string{filepath.Join(e.p.Clod(), "install.sh") + " --yes"}; !slices.Equal(e.commands(), want) {
		t.Fatalf("ran %q, want %q", e.commands(), want)
	}
}

// logged in to GitHub already: straight to the clone
func TestSetupClodLoggedIn(t *testing.T) {
	e := newClodEnv(t, "gh", "jq")
	e.loggedIn = true
	if err := setupClod(e.p, yesPrompter("y\n"), false); err != nil {
		t.Fatal(err)
	}
	if got := e.commands(); len(got) != 2 || !strings.HasPrefix(got[0], "gh repo clone") {
		t.Fatalf("ran %q", got)
	}
}

// already set up, or Claude Code missing: nothing runs, nothing is asked
func TestSetupClodNothingToDo(t *testing.T) {
	e := newClodEnv(t, "clod")
	if err := setupClod(e.p, yesPrompter(""), false); err != nil || len(*e.ran) != 0 {
		t.Fatalf("err %v, ran %q", err, e.commands())
	}

	e = newClodEnv(t)
	fakeClaude(t, claudeMissing)
	out := captureStdout(t, func() {
		if err := setupClod(e.p, yesPrompter(""), false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "once Claude Code is installed") {
		t.Fatalf("out %q", out)
	}
}

// bud update sets clod up when it's missing and you haven't said no
func TestOfferClod(t *testing.T) {
	e := newClodEnv(t, "jq")
	e.checkout(t)
	captureStdout(t, func() { offerClod(e.p, yesPrompter("\n")) })
	if !clodInstalled(e.p) {
		t.Fatalf("ran %q, clod not installed", e.commands())
	}

	// with nobody to ask (a launch finishing an update), it stays quiet
	e = newClodEnv(t, "jq")
	e.checkout(t)
	offerClod(e.p, nil)
	if len(*e.ran) != 0 {
		t.Fatalf("ran %q with nobody to ask", e.commands())
	}
}

// a no is remembered: bud update stops offering, bud install asks again
func TestSetupClodDeclined(t *testing.T) {
	e := newClodEnv(t)
	captureStdout(t, func() {
		if err := setupClod(e.p, yesPrompter("n\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	if !declinedClod(e.p) || len(*e.ran) != 0 {
		t.Fatalf("declined %v, ran %q", declinedClod(e.p), e.commands())
	}

	// offerClod is bud update's: it doesn't even ask
	offerClod(e.p, yesPrompter("y\n"))
	if len(*e.ran) != 0 {
		t.Fatalf("bud update ran %q after a no", e.commands())
	}

	// setupClod is bud install's and bud claude setup's: it asks, and a yes clears the no
	e.loggedIn = true
	captureStdout(t, func() {
		if err := setupClod(e.p, yesPrompter("y\n"), false); err != nil {
			t.Fatal(err)
		}
	})
	if declinedClod(e.p) || !clodInstalled(e.p) {
		t.Fatalf("declined %v, installed %v", declinedClod(e.p), clodInstalled(e.p))
	}
}

// with nobody to ask, nothing runs unless --yes
func TestSetupClodNoTerminal(t *testing.T) {
	e := newClodEnv(t)
	out := captureStdout(t, func() {
		if err := setupClod(e.p, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if len(*e.ran) != 0 || !strings.Contains(out, "bud claude setup") {
		t.Fatalf("ran %q, out %q", e.commands(), out)
	}

	// --yes with nobody to log in to GitHub: says how, clones nothing
	err := setupClod(e.p, nil, true)
	if err == nil || !strings.Contains(err.Error(), "gh auth login") || len(*e.ran) != 0 {
		t.Fatalf("err %v, ran %q", err, e.commands())
	}

	// --yes at a terminal (bud bootstrap's ssh -t): the GitHub login still runs
	stdinTerminal = func() bool { return true }
	captureStdout(t, func() { err = setupClod(e.p, nil, true) })
	if err != nil || len(*e.ran) == 0 || e.commands()[0] != "gh auth login" {
		t.Fatalf("err %v, ran %q", err, e.commands())
	}
}

// something else at ~/clod is left alone, a clone cut short leaves nothing
// behind, and an installer that fails isn't recorded
func TestSetupClodFailures(t *testing.T) {
	e := newClodEnv(t)
	e.loggedIn = true
	os.MkdirAll(e.p.Clod(), 0o755)
	if err := setupClod(e.p, nil, true); err == nil || !strings.Contains(err.Error(), "no install.sh") {
		t.Fatalf("err %v", err)
	}

	e = newClodEnv(t, "gh", "jq")
	e.loggedIn = true
	runOnTerminal = func(argv []string) error {
		os.MkdirAll(argv[len(argv)-1], 0o755) // a partial clone
		return errors.New("connection reset")
	}
	if err := setupClod(e.p, nil, true); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("err %v", err)
	}
	for _, dir := range []string{e.p.Clod(), e.p.Clod() + ".clone"} {
		if _, err := os.Stat(dir); err == nil {
			t.Fatalf("%s left behind by a failed clone", dir)
		}
	}

	// a link left dangling (~/clod deleted) isn't clod set up
	e = newClodEnv(t)
	os.MkdirAll(e.p.BinDir, 0o755)
	os.Symlink(filepath.Join(e.p.Clod(), "install.sh"), e.p.ClodBin())
	if clodInstalled(e.p) {
		t.Fatal("a dangling clod link counts as installed")
	}

	// an installer that fails isn't recorded
	e = newClodEnv(t, "jq")
	e.checkout(t)
	runOnTerminal = func([]string) error { return errors.New("exit status 1") }
	if err := setupClod(e.p, nil, true); err == nil || clodInstalled(e.p) {
		t.Fatalf("err %v, installed %v", err, clodInstalled(e.p))
	}
}
