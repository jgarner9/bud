package cli

import (
	"errors"
	"os"
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
		if got.Kitty != "/Applications/kitty.app" || got.Home != home || got.KittyConf != home+"/.config/kitty/kitty.conf" {
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

func TestUserKittyConfSkipsBudsOwn(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(home+"/.config/kitty", 0o755)
	os.WriteFile(home+"/.config/kitty/kitty.conf", nil, 0o644)
	t.Setenv("XDG_CONFIG_HOME", "")
	// inside a Bud window, KITTY_CONFIG_DIRECTORY is Bud's own config dir
	t.Setenv("KITTY_CONFIG_DIRECTORY", "/Applications/Bud.app/Contents/Resources/"+bundle.KittyDir)
	if got := userKittyConf(home); got != home+"/.config/kitty/kitty.conf" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("KITTY_CONFIG_DIRECTORY", "")
	os.Remove(home + "/.config/kitty/kitty.conf")
	if got := userKittyConf(home); got != "" {
		t.Fatalf("no kitty.conf: got %q", got)
	}
}
