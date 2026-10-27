// Package installer makes sure the system tooling bud depends on is present.
// Mason handles language tooling inside Neovim; this covers everything else,
// through the system package manager (D16). It installs what's missing but never
// upgrades what's there, Neovim included (D19)
package installer

import (
	"fmt"
	"os"
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
	// FullOnly deps are left out of the minimal profile
	FullOnly bool
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
	// only language servers like TypeScript's need node, which a server rarely runs
	{Name: "node", Bins: []string{"node"}, FullOnly: true, Pkgs: map[string][]string{
		"pacman": {"nodejs", "npm"},
		"apt":    {"nodejs", "npm"},
		"dnf":    {"nodejs", "npm"},
	}},
}

// Needed returns the deps a profile installs, in install order
func Needed(profile string) []Dep {
	var deps []Dep
	for _, dep := range Deps {
		if dep.FullOnly && profile == "minimal" {
			continue
		}
		deps = append(deps, dep)
	}

	return deps
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

// Missing returns the profile's deps with no binary on PATH
func Missing(profile string) []Dep {
	var missing []Dep
	for _, dep := range Needed(profile) {
		if !dep.Installed() {
			missing = append(missing, dep)
		}
	}

	return missing
}

// Manager is a system package manager and the command that installs with it
type Manager struct {
	Name string
	Bin  string
	// Install is the command that installs packages without asking again,
	// since bud has already asked before it runs
	Install []string
	Sudo    bool
}

// Command returns the full command line that installs pkgs
func (m Manager) Command(pkgs []string) []string {
	var argv []string
	// root (say, in a container) often has no sudo at all
	if m.Sudo && geteuid() != 0 {
		argv = append(argv, "sudo")
	}
	argv = append(argv, m.Install...)

	return append(argv, pkgs...)
}

// linuxManagers is in preference order (D16)
var linuxManagers = []Manager{
	{Name: "pacman", Bin: "pacman", Install: []string{"pacman", "-S", "--needed", "--noconfirm"}, Sudo: true},
	{Name: "apt", Bin: "apt-get", Install: []string{"apt-get", "install", "-y"}, Sudo: true},
	{Name: "dnf", Bin: "dnf", Install: []string{"dnf", "install", "-y"}, Sudo: true},
}

var brew = Manager{Name: "brew", Bin: "brew", Install: []string{"brew", "install"}}

// swappable in tests
var (
	goos    = runtime.GOOS
	geteuid = os.Geteuid
	// run executes argv attached to the terminal, so sudo can ask for a password
	run = func(argv []string) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
)

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
		fmt.Fprintf(&b, "install them with:\n  %s\n", strings.Join(e.Manager.Command(pkgs), " "))
	} else {
		b.WriteString("install them with your system's package manager\n")
	}
	b.WriteString("then run 'bud install' again")

	return b.String()
}

// Plan is what installing a profile's missing deps takes
type Plan struct {
	Profile string
	Missing []Dep
	Manager Manager
	Found   bool // false when no supported package manager is on PATH
}

func NewPlan(profile string) *Plan {
	m, found := DetectManager()
	return &Plan{Profile: profile, Missing: Missing(profile), Manager: m, Found: found}
}

// Command is the command line Run executes, or nil when there's nothing it can run
func (p *Plan) Command() []string {
	if len(p.Missing) == 0 || !p.Found {
		return nil
	}

	var pkgs []string
	for _, dep := range p.Missing {
		pkgs = append(pkgs, dep.Packages(p.Manager)...)
	}

	return p.Manager.Command(pkgs)
}

// Err is a *MissingError naming what's missing, or nil when nothing is
func (p *Plan) Err() error {
	if len(p.Missing) == 0 {
		return nil
	}

	return &MissingError{Deps: p.Missing, Manager: p.Manager, Found: p.Found}
}

// Run installs the missing deps, then checks again, since a package can
// install without putting the binary bud looks for on PATH
func (p *Plan) Run() error {
	argv := p.Command()
	if argv == nil {
		return p.Err()
	}
	if err := run(argv); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}

	return Check(p.Profile)
}

// Check returns a *MissingError when any of the profile's deps is missing
func Check(profile string) error {
	return NewPlan(profile).Err()
}
