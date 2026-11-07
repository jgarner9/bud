package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/paths"
)

// fakeBundles records what bundle would build, as if running on goos
func fakeBundles(t *testing.T, goos string) (mac, linux *[]string, got *bundle.Mac) {
	t.Helper()
	mac, linux, got = &[]string{}, &[]string{}, &bundle.Mac{}
	origMac, origLinux, origGOOS, origKitty := buildMac, installLinux, bundleGOOS, bundleKitty
	bundleKitty = func(*paths.Paths) string { return "" }
	buildMac = func(m bundle.Mac, dest string) error { *got = m; *mac = append(*mac, dest); return nil }
	installLinux = func(_ bundle.Linux, entry string, icon func(int) string) error {
		*linux = append(*linux, entry, icon(256))
		return nil
	}
	bundleGOOS = goos
	t.Cleanup(func() { buildMac, installLinux, bundleGOOS, bundleKitty = origMac, origLinux, origGOOS, origKitty })

	return mac, linux, got
}

func TestBundle(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		mac, _, got := fakeBundles(t, "darwin")
		var usage *UsageError
		if err := bundleCmd(&Globals{}, nil, true, true, ""); !errors.As(err, &usage) {
			t.Fatalf("both: %v", err)
		}
		if err := bundleCmd(&Globals{}, nil, false, true, ""); err == nil || !strings.Contains(err.Error(), "for Linux") {
			t.Fatalf("--linux on macOS: %v", err)
		}
		var err error
		captureStdout(t, func() { err = bundleCmd(&Globals{}, nil, false, false, "/tmp/x/Bud.app") })
		if err != nil {
			t.Fatal(err)
		}
		if len(*mac) != 1 || (*mac)[0] != "/tmp/x/Bud.app" || got.Path != os.Getenv("PATH") || got.Bin == "" {
			t.Fatalf("built %v with %+v", *mac, *got)
		}
	})

	t.Run("macOS with kitty", func(t *testing.T) {
		_, _, got := fakeBundles(t, "darwin")
		bundleKitty = func(*paths.Paths) string { return "/Applications/kitty.app" }
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("KITTY_CONFIG_DIRECTORY", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		os.MkdirAll(home+"/.config/kitty", 0o755)
		os.WriteFile(home+"/.config/kitty/kitty.conf", []byte("font_size 14\n"), 0o644)
		var err error
		out := captureStdout(t, func() { err = bundleCmd(&Globals{}, nil, false, false, "/tmp/x/Bud.app") })
		if err != nil {
			t.Fatal(err)
		}
		if got.Kitty != "/Applications/kitty.app" || got.Home != home || got.KittyConf != home+"/.config/kitty/kitty.conf" || got.KittyHome != home+"/.config/kitty" {
			t.Fatalf("built %+v", *got)
		}
		// the same command bud launch runs in a window
		if n := len(got.Launch); n < 4 || got.Launch[0] != "/usr/bin/env" || got.Launch[n-2] != got.Bin || got.Launch[n-1] != "open" {
			t.Fatalf("launch %q", got.Launch)
		}
		if !strings.Contains(out, "its own copy of /Applications/kitty.app") {
			t.Fatalf("output %q", out)
		}
	})

	t.Run("Linux", func(t *testing.T) {
		_, linux, _ := fakeBundles(t, "linux")
		t.Setenv("XDG_DATA_HOME", "/data")
		if err := bundleCmd(&Globals{}, nil, true, false, ""); err == nil || !strings.Contains(err.Error(), "only be built on macOS") {
			t.Fatalf("--macos on Linux: %v", err)
		}
		captureStdout(t, func() {
			if err := bundleCmd(&Globals{}, nil, false, false, ""); err != nil {
				t.Fatal(err)
			}
			if err := bundleCmd(&Globals{}, nil, false, true, "/tmp/x/bud.desktop"); err != nil {
				t.Fatal(err)
			}
		})
		want := []string{
			"/data/applications/bud.desktop", "/data/icons/hicolor/256x256/apps/bud.png",
			"/tmp/x/bud.desktop", "/tmp/x/bud-256.png",
		}
		if strings.Join(*linux, " ") != strings.Join(want, " ") {
			t.Fatalf("installed %v", *linux)
		}
	})
}

func TestUserKittyLooksWhereKittyDoes(t *testing.T) {
	home := t.TempDir()
	conf := func(dir string) string {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "kitty.conf"), nil, 0o644)
		return filepath.Join(dir, "kitty.conf")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CONFIG_DIRS", "")
	t.Setenv("KITTY_CONFIG_DIRECTORY", "")
	check := func(what, wantDir, wantConf string) {
		t.Helper()
		if dir, c := userKitty(home); dir != wantDir || c != wantConf {
			t.Errorf("%s: got %q, %q; want %q, %q", what, dir, c, wantDir, wantConf)
		}
	}

	check("none: kitty's default, nothing to include", home+"/.config/kitty", "")
	t.Setenv("XDG_CONFIG_HOME", home+"/xdg")
	check("none, with XDG_CONFIG_HOME: the dir kitty would use", home+"/xdg/kitty", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	prefs := conf(home + "/Library/Preferences/kitty")
	check("macOS's Preferences dir", home+"/Library/Preferences/kitty", prefs)
	dot := conf(home + "/.config/kitty")
	check("~/.config/kitty comes first", home+"/.config/kitty", dot)
	// inside a Bud window, KITTY_CONFIG_DIRECTORY is Bud's own, which would include itself
	t.Setenv("KITTY_CONFIG_DIRECTORY", "/Applications/Bud.app/Contents/Resources/"+bundle.KittyDir)
	check("not Bud's own", home+"/.config/kitty", dot)
	// a KITTY_CONFIG_DIRECTORY without a kitty.conf is passed over
	t.Setenv("KITTY_CONFIG_DIRECTORY", home+"/empty")
	check("one with no kitty.conf", home+"/.config/kitty", dot)
	set := conf(home + "/set")
	t.Setenv("KITTY_CONFIG_DIRECTORY", home+"/set")
	check("yours wins", home+"/set", set)
}

func TestUpdateRebuildsAStaleBundleWhereItIs(t *testing.T) {
	mac, linux, _ := fakeBundles(t, "darwin")
	origFind, origStale, origLinux := findBundles, macStale, linuxVersion
	t.Cleanup(func() { findBundles, macStale, linuxVersion = origFind, origStale, origLinux })
	findBundles = func(*paths.Paths) []bundle.Installed {
		return []bundle.Installed{{Path: "/Users/j/Applications/Bud.app"}}
	}
	p := &paths.Paths{Data: "/data"}

	// up to date: nothing built
	macStale = func(string, string, bool) string { return "" }
	out := captureStdout(t, func() { refreshBundlesFor(&Globals{}, p) })
	if len(*mac) != 0 || out != "" {
		t.Fatalf("rebuilt an up-to-date app: %v %q", *mac, out)
	}

	// stale: built again in the same place, saying why
	var wantKitty bool
	bundleKitty = func(*paths.Paths) string { return "/Applications/kitty.app" }
	macStale = func(_, _ string, kitty bool) string { wantKitty = kitty; return "bud 0.25.2 built it" }
	out = captureStdout(t, func() { refreshBundlesFor(&Globals{}, p) })
	if len(*mac) != 1 || (*mac)[0] != "/Users/j/Applications/Bud.app" || !wantKitty {
		t.Fatalf("built %v (want kitty %v)", *mac, wantKitty)
	}
	if !strings.Contains(out, "Rebuilding /Users/j/Applications/Bud.app (bud 0.25.2 built it)") {
		t.Fatalf("output %q", out)
	}

	// a failed build only says so
	buildMac = func(bundle.Mac, string) error { return errors.New("codesign failed") }
	out = captureStdout(t, func() { refreshBundlesFor(&Globals{}, p) })
	if !strings.Contains(out, "Couldn't rebuild it (codesign failed); 'bud bundle' tries again.") {
		t.Fatalf("output %q", out)
	}

	// never run bud bundle: nothing to do
	findBundles = func(*paths.Paths) []bundle.Installed { return nil }
	macStale = func(string, string, bool) string { t.Fatal("checked a bundle that isn't there"); return "" }
	refreshBundlesFor(&Globals{}, p)

	// Linux: the entry another bud wrote, rewritten with its icons where bud bundle puts them
	bundleGOOS = "linux"
	t.Setenv("XDG_DATA_HOME", "/data")
	entry := filepath.Join("/data", "applications", "bud.desktop")
	findBundles = func(*paths.Paths) []bundle.Installed { return []bundle.Installed{{Path: entry}} }
	linuxVersion = func(string) string { return "0.25.2" }
	lp, _ := paths.Resolve()
	captureStdout(t, func() { refreshBundlesFor(&Globals{}, lp) })
	if len(*linux) != 2 || (*linux)[0] != entry || (*linux)[1] != "/data/icons/hicolor/256x256/apps/bud.png" {
		t.Fatalf("installed %v", *linux)
	}
}
