package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/version"
)

func init() {
	RegisterCommand(Bundle())
}

// Bundle builds the app bundle that opens bud from the desktop (BASE.md 5.6)
// and installs it: Bud.app on macOS, bud.desktop on Linux
func Bundle() *Command {
	var macos, linux bool
	var dest string
	return &Command{
		Name:  "bundle",
		Usage: "Builds and installs the app that opens bud from the desktop (Bud.app on macOS, bud.desktop on Linux)",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&macos, "macos", false, "build Bud.app (the default on macOS)")
			fs.BoolVar(&linux, "linux", false, "write bud.desktop and its icon (the default on Linux)")
			fs.StringVar(&dest, "dest", "", "where to put it (default: /Applications/Bud.app, or ~/Applications when /Applications isn't writable; ~/.local/share/applications/bud.desktop on Linux)")
		},
		Run: func(g *Globals, args []string) error { return bundleCmd(g, args, macos, linux, dest) },
	}
}

// swappable in tests
var (
	buildMac     = func(m bundle.Mac, dest string) error { return m.Build(dest) }
	bundleGOOS   = runtime.GOOS
	installLinux = func(l bundle.Linux, entry string, icon func(int) string) error { return l.Install(entry, icon) }
)

func bundleCmd(g *Globals, args []string, macos, linux bool, dest string) error {
	if len(args) > 0 {
		return usageErrorf("bundle takes no arguments")
	}
	if macos && linux {
		return usageErrorf("pick one of --macos and --linux")
	}
	if !macos && !linux {
		macos = bundleGOOS == "darwin"
		linux = !macos
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	if linux {
		return bundleLinux(g, self, dest)
	}
	if bundleGOOS != "darwin" {
		return errors.New("Bud.app can only be built on macOS: it needs osacompile")
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

// bundleLinux writes bud.desktop and its icons under ~/.local/share, or the
// entry at dest with the icons beside it
func bundleLinux(g *Globals, self, dest string) error {
	if bundleGOOS != "linux" {
		return errors.New("bud.desktop is for Linux desktops")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	icon := p.DesktopIcon
	if dest == "" {
		dest = p.DesktopEntry()
	} else {
		dir := filepath.Dir(dest)
		icon = func(size int) string { return filepath.Join(dir, fmt.Sprintf("bud-%d.png", size)) }
	}
	g.Logf("writing %s to run %s", dest, self)
	l := bundle.Linux{Bin: self, Path: os.Getenv("PATH"), Version: version.String()}
	if err := installLinux(l, dest, icon); err != nil {
		return err
	}
	fmt.Printf("Installed %s: it opens bud with %s\n", dest, self)

	return nil
}
