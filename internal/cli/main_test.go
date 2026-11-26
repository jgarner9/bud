package cli

import (
	"os"
	"testing"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/paths"
)

// TestMain turns off, for every test, what would reach outside the test:
// the gh setup would reach a package manager and GitHub, and the bundle
// refresh after bud update would rebuild this machine's real Bud.app;
// github_test.go and bundle_test.go put the real ones back, with what they touch swapped
func TestMain(m *testing.M) {
	githubSetup = func(*prompter, bool, string) error { return nil }
	refreshBundles = func(*Globals, *paths.Paths) {}
	findBundles = func(*paths.Paths) []bundle.Installed { return nil }
	os.Exit(m.Run())
}
