package cli

import (
	"os"
	"testing"

	"jgarner.dev/bud/internal/paths"
)

// TestMain turns clod's setup off for every test, since it installs packages
// and talks to GitHub; clod_test.go calls setupClod itself, with those swapped
func TestMain(m *testing.M) {
	clodSetup = func(*paths.Paths, *prompter, bool) error { return nil }
	os.Exit(m.Run())
}
