package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/installer"
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
	// the kitty.app Bud.app copies: kitty's app when it's the terminal bud
	// launches, else "" for the droplet
	bundleKitty = func(p *paths.Paths) string {
		name, err := pickTerminal(p, "")
		if err != nil || name != "kitty" {
			return ""
		}
		t, _ := installer.Lookup("kitty")
		_, app := t.Locate()
		return app
	}
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
	self, err := bundleBin()
	if err != nil {
		return err
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
	m := bundle.Mac{Bin: self, Path: os.Getenv("PATH"), Version: version.String()}
	if p, err := paths.Resolve(); err == nil {
		m.Kitty = bundleKitty(p)
	}
	what := "a droplet that runs bud launch"
	if m.Kitty != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		m.Home, m.Launch = home, launchArgv(self, &Globals{}, "", nil)
		m.KittyHome, m.KittyConf = userKitty(home)
		what = "its own copy of " + m.Kitty
	}
	g.Logf("building %s, %s, to run %s", dest, what, self)
	if err := buildMac(m, dest); err != nil {
		return err
	}
	fmt.Printf("Installed %s (%s): it opens bud with %s\n", dest, what, self)

	return nil
}

// userKitty is your kitty config dir and the kitty.conf in it, which Bud's
// own config includes so Bud looks like your kitty. It looks where kitty
// does, in its order: KITTY_CONFIG_DIRECTORY (not Bud's own, inside a Bud
// window), $XDG_CONFIG_HOME/kitty, ~/.config/kitty, each of $XDG_CONFIG_DIRS,
// then ~/Library/Preferences/kitty; the first with a kitty.conf wins. With
// none, conf is "" and dir is the first of them, the one kitty would use
func userKitty(home string) (dir, conf string) {
	var dirs []string
	if d := os.Getenv("KITTY_CONFIG_DIRECTORY"); d != "" && filepath.Base(d) != bundle.KittyDir {
		dirs = append(dirs, d)
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		dirs = append(dirs, filepath.Join(x, "kitty"))
	}
	dirs = append(dirs, filepath.Join(home, ".config", "kitty"))
	for _, x := range filepath.SplitList(os.Getenv("XDG_CONFIG_DIRS")) {
		dirs = append(dirs, filepath.Join(x, "kitty"))
	}
	dirs = append(dirs, filepath.Join(home, "Library", "Preferences", "kitty"))
	for _, d := range dirs {
		if c := filepath.Join(d, "kitty.conf"); isFile(c) {
			return d, c
		}
	}
	return dirs[0], ""
}

// refreshBundles builds the app bud bundle made again, in place, when it's
// out of date (JGA-187): Bud.app (bundle.MacInfo.Stale) or bud.desktop built
// by another bud, running another bud, the wrong kind for the terminal, or
// copying a kitty that has moved on. Only the default locations bundle.Find
// looks in are refreshed, not a --dest elsewhere. Nothing to do when bud
// bundle never ran, and a failure only says so: the app still opens bud, and
// bud bundle tries again. TestMain turns it off, so no test rebuilds this
// machine's app
var refreshBundles = refreshBundlesFor

// swappable in tests
var (
	readMac      = bundle.ReadMac
	linuxVersion = bundle.LinuxVersion
	// appRunning says whether the app at path has a process running from it
	appRunning = func(path string) bool {
		return exec.Command("pgrep", "-f", filepath.Join(path, "Contents", "MacOS")+"/").Run() == nil
	}
	// installedSelf is this bud's path when it's the one install.sh
	// installed: a rebuild bakes this bud into the app, so a dev build (a go
	// run temp binary, say) must never do one
	installedSelf = func(p *paths.Paths) (string, bool) {
		r, err := installer.ReadReceipt(p.InstallReceipt())
		self, serr := os.Executable()
		if err != nil || r == nil || serr != nil || !version.Released() || !paths.SameDir(r.Path, self) {
			return "", false
		}
		return r.Path, true
	}
)

func refreshBundlesFor(g *Globals, p *paths.Paths) {
	found := findBundles(p)
	if len(found) == 0 {
		return
	}
	self, ok := installedSelf(p)
	if !ok {
		g.Logf("not rebuilding the app: this bud isn't the release install.sh installed")
		return
	}
	want := bundle.Want{Version: version.String(), Bin: self}
	// kitty is the terminal, but its app isn't where bud looks: a kitty copy
	// is left as it is rather than rebuilt as the droplet
	lostKitty := false
	if bundleGOOS == "darwin" {
		name, err := pickTerminal(p, "")
		if err != nil {
			g.Logf("not rebuilding the app: %v", err)
			return
		}
		want.Kind = true
		if name == "kitty" {
			want.Kitty = bundleKitty(p)
			lostKitty = want.Kitty == ""
		}
	}
	for _, b := range found {
		why := ""
		switch bundleGOOS {
		case "darwin":
			info, err := readMac(b.Path)
			if err != nil {
				g.Logf("can't read %s: %v", b.Path, err)
				continue
			}
			w := want
			if lostKitty {
				if info.Kitty != "" {
					fmt.Printf("%s is a copy of kitty, and bud can't find kitty.app now, so it's left as it is.\n", b.Path)
					continue
				}
				w.Kind = false
			}
			if why = info.Stale(b.Path, w); why != "" && appRunning(b.Path) {
				fmt.Printf("%s is out of date (%s), but it's open: quit Bud, then run 'bud bundle'.\n", b.Path, why)
				continue
			}
		case "linux":
			if v := linuxVersion(b.Path); v == "" {
				why = "an older bud wrote it"
			} else if v != want.Version {
				why = "bud " + v + " wrote it"
			}
		}
		if why == "" {
			continue
		}
		fmt.Printf("Rebuilding %s (%s)...\n", b.Path, why)
		dest := b.Path
		if bundleGOOS == "linux" {
			// bud.desktop in its default place, with the icons in the icon theme
			dest = ""
		}
		if err := bundleCmd(g, nil, false, false, dest); err != nil {
			fmt.Printf("Couldn't rebuild it (%v); 'bud bundle' tries again.\n", err)
		}
	}
}

// bundleBin is the bud the app runs: the one install.sh put in place, whose
// path a self-update keeps, else this one
func bundleBin() (string, error) {
	if p, err := paths.Resolve(); err == nil {
		if r, err := installer.ReadReceipt(p.InstallReceipt()); err == nil && r != nil && isFile(r.Path) {
			return r.Path, nil
		}
	}

	return os.Executable()
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
