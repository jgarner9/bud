package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// gh is optional: bud install offers its login on the full profile, at a
// terminal, and otherwise leaves it alone
func TestSetupGitHub(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	origLogged, origRun := ghLoggedIn, runOnTerminal
	t.Cleanup(func() { ghLoggedIn, runOnTerminal = origLogged, origRun })
	var ran [][]string
	runOnTerminal = func(argv []string) error { ran = append(ran, argv); return nil }
	loggedIn := false
	ghLoggedIn = func() bool { return loggedIn }

	// logged out, a yes runs gh's own login
	if err := setupGitHub(yesPrompter("y\n"), false, "full"); err != nil || len(ran) != 1 || !slices.Equal(ran[0], []string{"gh", "auth", "login"}) {
		t.Fatalf("err %v, ran %q", err, ran)
	}

	// a no, nobody to ask, the minimal profile and a login already there all leave it
	ran = nil
	if err := setupGitHub(yesPrompter("n\n"), false, "full"); err != nil {
		t.Fatal(err)
	}
	if err := setupGitHub(nil, true, "full"); err != nil {
		t.Fatal(err)
	}
	if err := setupGitHub(yesPrompter("y\n"), false, "minimal"); err != nil {
		t.Fatal(err)
	}
	loggedIn = true
	if err := setupGitHub(yesPrompter("y\n"), false, "full"); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 0 {
		t.Fatalf("ran %q", ran)
	}
}
