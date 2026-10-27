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
	if _, err := exec.LookPath(t.Bin); err == nil {
		return true
	}
	if goos != "darwin" {
		return false
	}
	for _, dir := range appDirs() {
		if _, err := os.Stat(filepath.Join(dir, t.App)); err == nil {
			return true
		}
	}

	return false
}
