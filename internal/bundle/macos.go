// Package bundle builds the app bundles that open bud from the desktop
// (BASE.md 5.6). On macOS Bud.app is its own copy of kitty when kitty is the
// terminal (JGA-185, D97), so macOS, the Dock and window managers see an app
// called Bud, and Cmd+Q quits only it; with another terminal it's a droplet
// that runs bud launch
package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"

	"jgarner.dev/bud"
)

// MacID is Bud.app's bundle identifier, which is also how bud knows a
// Bud.app is one it built
const MacID = "dev.jgarner.bud"

// Mac is what goes into Bud.app
type Mac struct {
	Bin     string // the bud it runs
	Path    string // the PATH it runs bud with
	Version string

	// Kitty is the kitty.app Bud.app is a copy of; "" builds the droplet
	Kitty string
	// Launch is the command a Bud window runs, bud open; a dropped file or
	// folder goes after it
	Launch []string
	// Home is where a Bud window starts
	Home string
	// KittyConf is your own kitty.conf, which Bud's includes; "" when there's none
	KittyConf string
	// KittyHome is your kitty config dir, which programs run inside Bud get
	// as KITTY_CONFIG_DIRECTORY (kitten themes, kitten ssh), not Bud's own
	KittyHome string
}

// Keys bud adds to a kitty copy's Info.plist: the kitty.app it was copied
// from and that kitty's version, so a newer kitty shows as a stale copy
const (
	kittyKey        = "BudKitty"
	kittyVersionKey = "BudKittyVersion"
	// KittyDir is the name of Bud's own kitty config dir inside the app
	KittyDir = "bud-kitty"
)

// documentTypes lets Finder and the Dock drop any file or folder on the icon;
// osacompile's own default only takes files
const documentTypes = `[{"CFBundleTypeName":"Folder or file","CFBundleTypeRole":"Viewer","LSItemContentTypes":["public.item"]}]`

// MacDest is where Bud.app goes: /Applications when this user can write
// there, else ~/Applications
func MacDest() (string, error) {
	if syscall.Access("/Applications", 2 /* W_OK */) == nil {
		return "/Applications/Bud.app", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "Applications", "Bud.app"), nil
}

// Build writes Bud.app at dest: a copy of m.Kitty when that's set, else the
// droplet. It replaces a Bud.app bud built before, and refuses to touch any
// other app there
func (m Mac) Build(dest string) error {
	// the copy's config dir is named for dest, and Launch Services starts it in /
	dest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := ownedOrMissing(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// built beside dest, so it can be renamed into place
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".Bud.app-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	app := filepath.Join(tmp, "Bud.app")
	build := m.droplet
	if m.Kitty != "" {
		build = m.kitty
	}
	if err := build(tmp, app, dest); err != nil {
		return err
	}
	// editing the bundle broke its signature, and Apple silicon needs an
	// ad-hoc one to run it at all
	if err := run("codesign", "--force", "--deep", "--sign", "-", app); err != nil {
		return err
	}

	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, filepath.Join(tmp, "old.app")); err != nil {
			return err
		}
	}
	if err := os.Rename(app, dest); err != nil {
		return err
	}
	// so Finder and the Dock pick up the new icon and Launch Services the new
	// Info.plist; it's only cosmetic
	_ = exec.Command(lsregister, "-f", dest).Run()

	return nil
}

// droplet compiles the AppleScript droplet that runs bud launch into app
func (m Mac) droplet(tmp, app, _ string) error {
	src, err := m.script()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(tmp, "main.applescript")
	if err := os.WriteFile(scriptPath, src, 0o644); err != nil {
		return err
	}
	if err := run("osacompile", "-o", app, scriptPath); err != nil {
		return err
	}

	plist := filepath.Join(app, "Contents", "Info.plist")
	for _, args := range [][]string{
		{"-replace", "CFBundleIdentifier", "-string", MacID},
		{"-replace", "CFBundleShortVersionString", "-string", m.Version},
		{"-replace", "CFBundleDocumentTypes", "-json", documentTypes},
		// the asset catalog's icon would win over droplet.icns
		{"-remove", "CFBundleIconName"},
	} {
		if err := run("plutil", append(args, plist)...); err != nil {
			return err
		}
	}
	resources := filepath.Join(app, "Contents", "Resources")
	if err := os.Remove(filepath.Join(resources, "Assets.car")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(filepath.Join(resources, "droplet.icns"), Icon(), 0o644)
}

// kitty makes app a copy of m.Kitty that is its own app: Bud's name, bundle
// id and icon, and its own kitty config dir (kittyFiles) that starts bud and
// opens what's dropped on the icon in it. The copy is an APFS clone where
// the volume allows, so it takes next to no disk. dest is where the app is
// going, which its config dir is named for
func (m Mac) kitty(_, app, dest string) error {
	// a linked kitty.app (nix-darwin's, home-manager's) would be copied as the
	// link, and every edit below would land in your kitty
	src, err := filepath.EvalSymlinks(m.Kitty)
	if err != nil {
		return err
	}
	if err := run("cp", "-c", "-R", src, app); err != nil {
		os.RemoveAll(app)
		if err := run("cp", "-R", src, app); err != nil {
			return err
		}
	}
	if info, err := os.Lstat(app); err != nil || !info.IsDir() {
		return fmt.Errorf("copying %s didn't make an app at %s", m.Kitty, app)
	}
	version, err := plistString(m.Kitty, "CFBundleShortVersionString")
	if err != nil {
		return fmt.Errorf("reading %s's version: %w", m.Kitty, err)
	}
	resources := filepath.Join(app, "Contents", "Resources")
	conf := filepath.Join(resources, KittyDir)
	if err := os.MkdirAll(conf, 0o755); err != nil {
		return err
	}
	for name, text := range m.kittyFiles() {
		if err := os.WriteFile(filepath.Join(conf, name), []byte(text), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(resources, "bud.icns"), Icon(), 0o644); err != nil {
		return err
	}

	plist := filepath.Join(app, "Contents", "Info.plist")
	for _, args := range [][]string{
		{"-replace", "CFBundleName", "-string", "Bud"},
		{"-replace", "CFBundleDisplayName", "-string", "Bud"},
		{"-replace", "CFBundleIdentifier", "-string", MacID},
		{"-replace", "CFBundleShortVersionString", "-string", m.Version},
		{"-replace", "CFBundleIconFile", "-string", "bud.icns"},
		{"-replace", "CFBundleDocumentTypes", "-json", documentTypes},
		// kitty's own LSEnvironment (KITTY_LAUNCHED_BY_LAUNCH_SERVICES) stays
		{"-replace", "LSEnvironment.KITTY_CONFIG_DIRECTORY", "-string", ConfigDirFor(dest)},
		{"-replace", kittyKey, "-string", m.Kitty},
		{"-replace", kittyVersionKey, "-string", version},
	} {
		if err := run("plutil", append(args, plist)...); err != nil {
			return err
		}
	}
	// the asset catalog's icon would win over bud.icns, and kitty's URL
	// schemes and Services would show up twice and could open Bud for kitty
	for _, key := range []string{"CFBundleIconName", "CFBundleURLTypes", "NSServices", "UTExportedTypeDeclarations", "UTImportedTypeDeclarations"} {
		_ = run("plutil", "-remove", key, plist)
	}

	return nil
}

// ConfigDirFor is the kitty config dir of the Bud.app at app
func ConfigDirFor(app string) string {
	return filepath.Join(app, "Contents", "Resources", KittyDir)
}

// kittyFiles is Bud's kitty config dir: kitty.conf, which includes yours and
// quits Bud with its last window; macos-launch-services-cmdline, what kitty
// runs when the app is opened from the Dock or Finder; and launch-actions.conf,
// which opens a file or folder dropped on the icon in a new Bud window
func (m Mac) kittyFiles() map[string]string {
	conf := "# Bud's kitty settings, written by bud bundle: yours first, then Bud's\n"
	if m.KittyConf != "" {
		conf += "include " + m.KittyConf + "\n"
	}
	conf += "# closing Bud's last window quits Bud\nmacos_quit_when_last_window_closed yes\n"
	if m.KittyHome != "" {
		conf += "# programs in Bud (kitten themes, kitten ssh) use your kitty config dir, not Bud's\n" +
			"env KITTY_CONFIG_DIRECTORY=" + m.KittyHome + "\n"
	}

	run := shellJoin(m.Launch)
	return map[string]string{
		"kitty.conf":                    conf,
		"macos-launch-services-cmdline": "--directory=" + shellQuote(m.Home) + " --title=bud " + run + "\n",
		"launch-actions.conf": "# a file or folder dropped on Bud's icon opens in bud (written by bud bundle)\n" +
			"protocol file\n" +
			"action launch --type=os-window --cwd=" + shellQuote(m.Home) + " " + run + " ${FILE_PATH}\n",
	}
}

// shellJoin quotes each word for a shell and joins them
func shellJoin(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = shellQuote(w)
	}
	return strings.Join(quoted, " ")
}

// shellQuote quotes s as one shell word when it needs it
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-~+=:,@%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// MacInfo is what a Bud.app records about the kitty it's a copy of; all ""
// for the droplet
type MacInfo struct {
	Version             string // the bud that built it
	Kitty, KittyVersion string // the kitty.app copied, and its version then
	ConfigDir           string // the kitty config dir its LSEnvironment names
}

// ReadMac reads the Bud.app at path's Info.plist, in one plutil call
func ReadMac(path string) (MacInfo, error) {
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", filepath.Join(path, "Contents", "Info.plist")).Output()
	if err != nil {
		return MacInfo{}, err
	}
	var plist struct {
		Bud     string            `json:"CFBundleShortVersionString"`
		Kitty   string            `json:"BudKitty"`
		Version string            `json:"BudKittyVersion"`
		Env     map[string]string `json:"LSEnvironment"`
	}
	if err := json.Unmarshal(out, &plist); err != nil {
		return MacInfo{}, err
	}
	return MacInfo{Version: plist.Bud, Kitty: plist.Kitty, KittyVersion: plist.Version, ConfigDir: plist.Env["KITTY_CONFIG_DIRECTORY"]}, nil
}

// MacStale says why the Bud.app at path needs building again, "" when it
// doesn't: another bud built it (its baked-in paths and files may be old),
// the kitty it's a copy of has been updated or removed, or it was moved, so
// the kitty config dir it names isn't its own. wantKitty is whether it
// should be a copy of kitty now, rather than the droplet
func MacStale(path, version string, wantKitty bool) string {
	info, err := ReadMac(path)
	switch {
	case err != nil:
		return "can't read its Info.plist: " + err.Error()
	case info.Version != version:
		return "bud " + info.Version + " built it"
	case wantKitty != (info.Kitty != ""):
		if wantKitty {
			return "kitty is the terminal now"
		}
		return "kitty isn't the terminal any more"
	case info.Kitty == "":
		return ""
	}
	now, err := KittyVersion(info.Kitty)
	switch {
	case err != nil:
		return info.Kitty + " isn't there any more"
	case now != info.KittyVersion:
		return "kitty is " + now + " now"
	case !SameDir(info.ConfigDir, ConfigDirFor(path)):
		return "it was moved"
	}
	return ""
}

// SameDir says whether a and b name one dir, links resolved where they can be
func SameDir(a, b string) bool {
	resolve := func(d string) string {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			return r
		}
		return filepath.Clean(d)
	}
	return resolve(a) == resolve(b)
}

// KittyVersion is the version of the kitty.app at path
func KittyVersion(path string) (string, error) {
	return plistString(path, "CFBundleShortVersionString")
}

// plistString reads one string key from the Info.plist of the app at path
func plistString(path, key string) (string, error) {
	out, err := exec.Command("plutil", "-extract", key, "raw", "-o", "-", filepath.Join(path, "Contents", "Info.plist")).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// ownedOrMissing is nil when nothing is at dest, or a Bud.app bud built
func ownedOrMissing(dest string) error {
	if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if id, _ := MacBundleID(dest); id != MacID {
		return fmt.Errorf("%s is already there and bud didn't build it; move it away first", dest)
	}

	return nil
}

// MacBundleID reads the bundle identifier of the app at path
func MacBundleID(path string) (string, error) {
	return plistString(path, "CFBundleIdentifier")
}

// script is the droplet's AppleScript with this bud and PATH filled in
func (m Mac) script() ([]byte, error) {
	src, err := fs.ReadFile(bud.Bundles(), "macos/main.applescript")
	if err != nil {
		return nil, err
	}
	t, err := template.New("main").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]string{"Bin": appleScriptString(m.Bin), "Path": appleScriptString(m.Path)})

	return buf.Bytes(), err
}

// appleScriptString escapes s to go between an AppleScript string's quotes
func appleScriptString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// run runs a macOS tool, with its output in the error when it fails
func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}

	return nil
}
