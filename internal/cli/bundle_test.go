package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/version"
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
	origFind, origRead, origLinux, origRunning, origSelf, origVersion := findBundles, readMac, linuxVersion, appRunning, installedSelf, bundle.KittyVersion
	t.Cleanup(func() {
		findBundles, readMac, linuxVersion, appRunning, installedSelf, bundle.KittyVersion = origFind, origRead, origLinux, origRunning, origSelf, origVersion
	})
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_DATA_HOME", "/data")
	p, _ := paths.Resolve()
	os.MkdirAll(filepath.Dir(p.MachineConfig()), 0o755)
	os.WriteFile(p.MachineConfig(), []byte(`{"terminal": "kitty"}`), 0o644)
	app := "/Users/j/Applications/Bud.app"
	findBundles = func(*paths.Paths) []bundle.Installed { return []bundle.Installed{{Path: app}} }
	bundleKitty = func(*paths.Paths) string { return "/Applications/kitty.app" }
	installedSelf = func(*paths.Paths) (string, bool) { return "/u/bin/bud", true }
	appRunning = func(string) bool { return false }
	bundle.KittyVersion = func(string) (string, error) { return "0.50.0", nil }
	current := bundle.MacInfo{Version: version.String(), Bin: "/u/bin/bud", Kitty: "/Applications/kitty.app", KittyVersion: "0.50.0", ConfigDir: bundle.ConfigDirFor(app)}
	info := current
	readMac = func(string) (bundle.MacInfo, error) { return info, nil }
	refresh := func() string { return captureStdout(t, func() { refreshBundlesFor(&Globals{}, p) }) }

	// up to date: nothing built
	if out := refresh(); len(*mac) != 0 || out != "" {
		t.Fatalf("rebuilt an up-to-date app: %v %q", *mac, out)
	}

	// another bud built it: built again in the same place, saying why
	info.Version = "0.25.2"
	if out := refresh(); len(*mac) != 1 || (*mac)[0] != app || !strings.Contains(out, "Rebuilding "+app+" (bud 0.25.2 built it)") {
		t.Fatalf("built %v, said %q", *mac, out)
	}

	// open: left alone, saying what to do
	appRunning = func(string) bool { return true }
	if out := refresh(); len(*mac) != 1 || !strings.Contains(out, "but it's open: quit Bud, then run 'bud bundle'") {
		t.Fatalf("rebuilt an open app: %v %q", *mac, out)
	}
	appRunning = func(string) bool { return false }

	// kitty is the terminal but its app can't be found: a kitty copy is kept, not made the droplet
	bundleKitty = func(*paths.Paths) string { return "" }
	if out := refresh(); len(*mac) != 1 || !strings.Contains(out, "bud can't find kitty.app now, so it's left as it is") {
		t.Fatalf("replaced the kitty copy: %v %q", *mac, out)
	}
	bundleKitty = func(*paths.Paths) string { return "/Applications/kitty.app" }

	// a failed build only says so
	buildMac = func(bundle.Mac, string) error { return errors.New("codesign failed") }
	if out := refresh(); !strings.Contains(out, "Couldn't rebuild it (codesign failed); 'bud bundle' tries again.") {
		t.Fatalf("output %q", out)
	}

	// not the bud install.sh installed (a dev build, go run): never rebuilds
	installedSelf = func(*paths.Paths) (string, bool) { return "", false }
	readMac = func(string) (bundle.MacInfo, error) { t.Fatal("a dev build looked at the app"); return info, nil }
	refresh()
	installedSelf = func(*paths.Paths) (string, bool) { return "/u/bin/bud", true }

	// never run bud bundle: nothing to do
	findBundles = func(*paths.Paths) []bundle.Installed { return nil }
	refresh()

	// Linux: the entry another bud wrote, written again in its default place
	*mac = (*mac)[:0]
	bundleGOOS = "linux"
	entry := filepath.Join("/data", "applications", "bud.desktop")
	findBundles = func(*paths.Paths) []bundle.Installed { return []bundle.Installed{{Path: entry}} }
	linuxVersion = func(string) string { return "" }
	if out := refresh(); len(*linux) != 2 || (*linux)[0] != entry || (*linux)[1] != "/data/icons/hicolor/256x256/apps/bud.png" || !strings.Contains(out, "(an older bud wrote it)") {
		t.Fatalf("installed %v, said %q", *linux, out)
	}
}
