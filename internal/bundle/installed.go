package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"

	"jgarner.dev/bud/internal/paths"
)

// Installed is one app bundle bud put on this machine: Path is what to show,
// Files everything to remove with it
type Installed struct {
	Path, What string
	Files      []string
}

// swappable in tests
var goos = runtime.GOOS

// Find returns the bundles bud built that are still here: a Bud.app it owns
// in either Applications dir, or its bud.desktop and icons
func Find(p *paths.Paths) []Installed {
	var found []Installed
	switch goos {
	case "darwin":
		home, _ := os.UserHomeDir()
		for _, app := range []string{"/Applications/Bud.app", filepath.Join(home, "Applications", "Bud.app")} {
			if id, _ := MacBundleID(app); id == MacID {
				found = append(found, Installed{app, "the Bud app (bud bundle)", []string{app}})
			}
		}
	case "linux":
		entry := p.DesktopEntry()
		if data, err := os.ReadFile(entry); err == nil && bytes.Contains(data, []byte(linuxMarker)) {
			files := []string{entry}
			for _, size := range IconSizes {
				if _, err := os.Stat(p.DesktopIcon(size)); err == nil {
					files = append(files, p.DesktopIcon(size))
				}
			}
			found = append(found, Installed{entry, "the desktop entry and its icons (bud bundle)", files})
		}
	}

	return found
}

// Remove deletes the bundle's files
func (i Installed) Remove() error {
	for _, f := range i.Files {
		if err := os.RemoveAll(f); err != nil {
			return err
		}
	}

	return nil
}
