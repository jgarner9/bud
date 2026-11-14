package cli

import (
	"os"
	"testing"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/plugin"
)

// TestMain turns off, for every test, what would reach outside the test:
// clod's setup installs packages and talks to GitHub, and the bundle refresh
// after bud update would rebuild this machine's real Bud.app; clod_test.go
// and bundle_test.go put the real ones back, with what they touch swapped
func TestMain(m *testing.M) {
	clodSetup = func(*paths.Paths, *prompter, bool) error { return nil }
	refreshBundles = func(*Globals, *paths.Paths) {}
	findBundles = func(*paths.Paths) []bundle.Installed { return nil }
	// the plugin is extracted under the data dir (D142), so no test writes to this machine's
	data, err := os.MkdirTemp("", "bud-cli-data")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_DATA_HOME", data)
	code := m.Run()
	os.RemoveAll(data)
	os.Exit(code)
}

// wantPlugin is the --plugin-dir pair a session the IDE starts gets
func wantPlugin(t *testing.T) []string {
	t.Helper()
	args, err := plugin.Args()
	if err != nil {
		t.Fatal(err)
	}

	return args
}
