// Package bundle builds the app bundles that open bud from the desktop
// (BASE.md 5.6): Bud.app on macOS, a droplet that runs bud launch
package bundle

import (
	"bytes"
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
}

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

// Build writes Bud.app at dest. It replaces a Bud.app bud built before, and
// refuses to touch any other app there
func (m Mac) Build(dest string) error {
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

	src, err := m.script()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(tmp, "main.applescript")
	if err := os.WriteFile(scriptPath, src, 0o644); err != nil {
		return err
	}
	app := filepath.Join(tmp, "Bud.app")
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
	if err := os.WriteFile(filepath.Join(resources, "droplet.icns"), Icon(), 0o644); err != nil {
		return err
	}
	// editing the bundle broke osacompile's ad-hoc signature, which Apple
	// silicon needs to run it at all
	if err := run("codesign", "--force", "--sign", "-", app); err != nil {
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
	// so Finder and the Dock pick up the new icon; it's only cosmetic
	_ = exec.Command(lsregister, "-f", dest).Run()

	return nil
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
	out, err := exec.Command("plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", filepath.Join(path, "Contents", "Info.plist")).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
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
