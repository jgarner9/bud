package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/bundle"
)

// fakeBundles records what bundle would build, as if running on goos
func fakeBundles(t *testing.T, goos string) (mac, linux *[]string, got *bundle.Mac) {
	t.Helper()
	mac, linux, got = &[]string{}, &[]string{}, &bundle.Mac{}
	origMac, origLinux, origGOOS := buildMac, installLinux, bundleGOOS
	buildMac = func(m bundle.Mac, dest string) error { *got = m; *mac = append(*mac, dest); return nil }
	installLinux = func(_ bundle.Linux, entry string, icon func(int) string) error {
		*linux = append(*linux, entry, icon(256))
		return nil
	}
	bundleGOOS = goos
	t.Cleanup(func() { buildMac, installLinux, bundleGOOS = origMac, origLinux, origGOOS })

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
