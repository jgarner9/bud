package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/version"
)

func init() {
	RegisterCommand(Bundle())
}

// Bundle builds the app bundle that opens bud from the desktop (BASE.md 5.6)
// and installs it: Bud.app on macOS
func Bundle() *Command {
	var macos, linux bool
	var dest string
	return &Command{
		Name:  "bundle",
		Usage: "Builds and installs the app that opens bud from the desktop (Bud.app on macOS)",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&macos, "macos", false, "build Bud.app (the default on macOS)")
			fs.BoolVar(&linux, "linux", false, "build bud.desktop (the default on Linux)")
			fs.StringVar(&dest, "dest", "", "where to put it (default: /Applications/Bud.app, or ~/Applications when /Applications isn't writable)")
		},
		Run: func(g *Globals, args []string) error { return bundleCmd(g, args, macos, linux, dest) },
	}
}

// swappable in tests
var buildMac = func(m bundle.Mac, dest string) error { return m.Build(dest) }

func bundleCmd(g *Globals, args []string, macos, linux bool, dest string) error {
	if len(args) > 0 {
		return usageErrorf("bundle takes no arguments")
	}
	if macos && linux {
		return usageErrorf("pick one of --macos and --linux")
	}
	if !macos && !linux {
		macos = runtime.GOOS == "darwin"
		linux = !macos
	}
	if linux {
		return errors.New("bud.desktop isn't built yet (JGA-78)")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("Bud.app can only be built on macOS: it needs osacompile")
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	if dest == "" {
		if dest, err = bundle.MacDest(); err != nil {
			return err
		}
	}
	g.Logf("building %s to run %s", dest, self)
	m := bundle.Mac{Bin: self, Path: os.Getenv("PATH"), Version: version.String()}
	if err := buildMac(m, dest); err != nil {
		return err
	}
	fmt.Printf("Installed %s: it opens bud with %s\n", dest, self)

	return nil
}
