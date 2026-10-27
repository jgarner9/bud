// Package installer makes sure the system tooling bud depends on is present.
// Mason handles language tooling inside Neovim; this covers everything else.
// It only checks for now: installing through the package manager comes later (D16)
package installer

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type Dep struct {
	Name string
	// any one of these on PATH counts, since apt ships fd as fdfind
	Bins []string
	// package names per manager; a manager with no entry uses Name
	Pkgs map[string][]string
}

// Deps lists every system dependency in the order it should be installed.
// nvim and git come first since the config sync and plugin restore need them
var Deps = []Dep{
	{Name: "neovim", Bins: []string{"nvim"}},
	{Name: "git", Bins: []string{"git"}},
	{Name: "ripgrep", Bins: []string{"rg"}},
	{Name: "fd", Bins: []string{"fd", "fdfind"}, Pkgs: map[string][]string{
		"apt": {"fd-find"},
		"dnf": {"fd-find"},
	}},
	{Name: "node", Bins: []string{"node"}, Pkgs: map[string][]string{
		"pacman": {"nodejs", "npm"},
		"apt":    {"nodejs", "npm"},
		"dnf":    {"nodejs", "npm"},
	}},
}

// Packages returns what to ask m for to get d
func (d Dep) Packages(m Manager) []string {
	if p, ok := d.Pkgs[m.Name]; ok {
		return p
	}

	return []string{d.Name}
}

func (d Dep) Installed() bool {
	for _, bin := range d.Bins {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}

	return false
}

// Missing returns the deps with no binary on PATH
func Missing() []Dep {
	var missing []Dep
	for _, dep := range Deps {
		if !dep.Installed() {
			missing = append(missing, dep)
		}
	}

	return missing
}

// Manager is a system package manager and the command that installs with it
type Manager struct {
	Name    string
	Bin     string
	Install string
}

// linuxManagers is in preference order (D16)
var linuxManagers = []Manager{
	{Name: "pacman", Bin: "pacman", Install: "sudo pacman -S --needed"},
	{Name: "apt", Bin: "apt-get", Install: "sudo apt install"},
	{Name: "dnf", Bin: "dnf", Install: "sudo dnf install"},
}

var brew = Manager{Name: "brew", Bin: "brew", Install: "brew install"}

// goos is runtime.GOOS, swappable in tests
var goos = runtime.GOOS

// DetectManager returns the package manager to suggest, or false when there's none on PATH
func DetectManager() (Manager, bool) {
	candidates := linuxManagers
	if goos == "darwin" {
		candidates = []Manager{brew}
	}

	for _, m := range candidates {
		if _, err := exec.LookPath(m.Bin); err == nil {
			return m, true
		}
	}

	return Manager{}, false
}

// MissingError lists the deps that aren't installed and how to get them
type MissingError struct {
	Deps    []Dep
	Manager Manager
	Found   bool // false when no supported package manager is on PATH
}

func (e *MissingError) Error() string {
	var names, pkgs []string
	for _, dep := range e.Deps {
		names = append(names, dep.Name)
		pkgs = append(pkgs, dep.Packages(e.Manager)...)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "missing system dependencies: %s\n", strings.Join(names, ", "))
	if e.Found {
		fmt.Fprintf(&b, "install them with:\n  %s %s\n", e.Manager.Install, strings.Join(pkgs, " "))
	} else {
		b.WriteString("install them with your system's package manager\n")
	}
	b.WriteString("then run 'bud install' again")

	return b.String()
}

// Check returns a *MissingError when any dep is missing
func Check() error {
	missing := Missing()
	if len(missing) == 0 {
		return nil
	}

	m, found := DetectManager()
	return &MissingError{Deps: missing, Manager: m, Found: found}
}
