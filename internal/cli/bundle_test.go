package cli

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/bundle"
)

func TestBundle(t *testing.T) {
	var built []string
	var got bundle.Mac
	orig := buildMac
	buildMac = func(m bundle.Mac, dest string) error { got = m; built = append(built, dest); return nil }
	t.Cleanup(func() { buildMac = orig })

	var usage *UsageError
	if err := bundleCmd(&Globals{}, nil, true, true, ""); !errors.As(err, &usage) {
		t.Fatalf("both: %v", err)
	}
	if err := bundleCmd(&Globals{}, nil, false, true, ""); err == nil || !strings.Contains(err.Error(), "isn't built yet") {
		t.Fatalf("--linux: %v", err)
	}

	err := error(nil)
	captureStdout(t, func() { err = bundleCmd(&Globals{}, nil, true, false, "/tmp/x/Bud.app") })
	if runtime.GOOS != "darwin" {
		if err == nil || !strings.Contains(err.Error(), "only be built on macOS") {
			t.Fatalf("--macos off macOS: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 1 || built[0] != "/tmp/x/Bud.app" || got.Path != os.Getenv("PATH") || got.Bin == "" {
		t.Fatalf("built %v with %+v", built, got)
	}
}
