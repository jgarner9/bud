package installer

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Terminal is a terminal emulator bud can launch (D11)
type Terminal struct {
	Name string
	Bin  string
	App  string // macOS app bundle, which doesn't put the binary on PATH
}

// Terminals is in default order: Kitty first when both are there (D11)
var Terminals = []Terminal{
	{Name: "kitty", Bin: "kitty", App: "kitty.app"},
	{Name: "ghostty", Bin: "ghostty", App: "Ghostty.app"},
}

// appDirs are where macOS app bundles live, swappable in tests
var appDirs = func() []string {
	home, _ := os.UserHomeDir()
	return []string{"/Applications", filepath.Join(home, "Applications")}
}

// DetectTerminals returns the names of the installed terminals
func DetectTerminals() []string {
	var found []string
	for _, t := range Terminals {
		if t.installed() {
			found = append(found, t.Name)
		}
	}

	return found
}

func (t Terminal) installed() bool {
	bin, app := t.Locate()
	return bin != "" || app != ""
}

// Lookup returns the terminal called name
func Lookup(name string) (Terminal, bool) {
	for _, t := range Terminals {
		if t.Name == name {
			return t, true
		}
	}

	return Terminal{}, false
}

// Locate finds the terminal's binary on PATH and, on macOS, its app bundle.
// Either is "" when it isn't there
func (t Terminal) Locate() (bin, app string) {
	bin, _ = exec.LookPath(t.Bin)
	if goos != "darwin" {
		return bin, ""
	}
	for _, dir := range appDirs() {
		if path := filepath.Join(dir, t.App); isDir(path) {
			return bin, path
		}
	}

	return bin, ""
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
