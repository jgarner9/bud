// Package installer makes sure the system tooling bud depends on is present.
// Mason handles language tooling inside Neovim; this covers everything else,
// through the system package manager (D16). It installs what's missing but never
// upgrades what's there. The exception is Linux, where a Neovim or tree-sitter
// the package manager has too old (or none) is downloaded instead (D107)
package installer

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/paths"
)

type Dep struct {
	Name string
	// any one of these on PATH counts, since apt ships fd as fdfind
	Bins []string
	// package names per manager; a manager with no entry uses Name
	Pkgs map[string][]string
	// FullOnly deps are left out of the minimal profile
	FullOnly bool
	// Also lists binaries that must all be on PATH too, for a dep whose
	// package is split in two: node without npm can't install Mason's npm
	// packages (JGA-233)
	Also []string
	// Probe, when set, must pass for the dep to count as installed, for what
	// a binary on PATH doesn't show (python3 without venv support)
	Probe func() bool
	// Tool, when set, is what bud downloads on Linux in place of the package
	// manager's, which may be too old (D107)
	Tool *Tool
}

// Deps lists every system dependency in the order it should be installed.
// nvim and git come first since the config sync and plugin restore need them
var Deps = []Dep{
	{Name: "neovim", Bins: []string{"nvim"}, Tool: &neovimTool},
	{Name: "git", Bins: []string{"git"}},
	{Name: "ripgrep", Bins: []string{"rg"}},
	{Name: "fd", Bins: []string{"fd", "fdfind"}, Pkgs: map[string][]string{
		"apt": {"fd-find"},
		"dnf": {"fd-find"},
	}},
	// fzf-lua's matcher (D28)
	{Name: "fzf", Bins: []string{"fzf"}},
	// nvim-treesitter builds parsers with these two (D29); doctor checks the
	// CLI's version, since some distros ship one too old for it
	{Name: "tree-sitter-cli", Bins: []string{"tree-sitter"}, Tool: &treeSitterTool},
	// macOS always has /usr/bin/cc (it offers to install the command line tools
	// on first use, and Homebrew needs them anyway), so only Linux installs this
	{Name: "gcc", Bins: []string{"cc", "gcc", "clang"}},
}

// LanguageDeps are the system deps a language spec can ask for by binary (its
// system field), with their package names. Anything else a spec asks for is
// taken to be a package named after the binary (ForBins)
var LanguageDeps = []Dep{
	{Name: "go", Bins: []string{"go"}, Pkgs: map[string][]string{
		"apt": {"golang-go"},
		"dnf": {"golang"},
	}},
	// venv too, which Mason installs PyPI packages (ty, ruff) into; apt splits it out
	{Name: "python3", Bins: []string{"python3"}, Probe: func() bool { return pythonCanVenv() }, Pkgs: map[string][]string{
		"brew":   {"python"},
		"pacman": {"python"},
		"apt":    {"python3", "python3-venv"},
	}},
	// npm too, which Mason installs npm packages (tsc, prettier) with
	{Name: "node", Bins: []string{"node"}, Also: []string{"npm"}, Pkgs: map[string][]string{
		"pacman": {"nodejs", "npm"},
		"apt":    {"nodejs", "npm"},
		"dnf":    {"nodejs", "npm"},
	}},
}

// ForBins returns the deps that provide bins, leaving out the ones Deps already
// has (a spec's "cc" is the base C compiler). A binary no dep lists becomes a
// dep of its own, installed as a package of the same name
func ForBins(bins []string) []Dep {
	var out []Dep
	have := func(name string) bool {
		return slices.ContainsFunc(out, func(d Dep) bool { return d.Name == name })
	}
	for _, bin := range bins {
		provides := func(d Dep) bool { return slices.Contains(d.Bins, bin) }
		if slices.ContainsFunc(Deps, provides) {
			continue
		}
		dep := Dep{Name: bin, Bins: []string{bin}}
		if i := slices.IndexFunc(LanguageDeps, provides); i >= 0 {
			dep = LanguageDeps[i]
		}
		if !have(dep.Name) {
			out = append(out, dep)
		}
	}

	return out
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

// Installed says whether the dep is on PATH. A downloaded one (Managed) also
// has to be new enough, since a package manager's can be a release behind
func (d Dep) Installed() bool {
	if t, ok := d.managed(); ok {
		return t.newEnough()
	}

	if !d.OnPath() {
		return false
	}
	for _, bin := range d.Also {
		if _, err := exec.LookPath(bin); err != nil {
			return false
		}
	}

	return d.Probe == nil || d.Probe()
}

// pythonCanVenv says whether python3 can make a virtual environment, which
// Mason installs its PyPI packages into. Debian splits that out as python3-venv
// and ships python3 without it. It's swappable in tests
var pythonCanVenv = func() bool {
	return exec.Command("python3", "-c", "import ensurepip, venv").Run() == nil
}

// OnPath says whether any of the dep's binaries is on PATH, of whatever version
func (d Dep) OnPath() bool {
	for _, bin := range d.Bins {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}

	return false
}

// Missing returns the deps with no binary on PATH
func Missing(deps []Dep) []Dep {
	var missing []Dep
	for _, dep := range deps {
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

// Plan is what installing the missing ones of some deps takes
type Plan struct {
	Missing []Dep
	Manager Manager
	Found   bool // false when no supported package manager is on PATH
}

func NewPlan(deps []Dep) *Plan {
	m, found := DetectManager()
	return &Plan{Missing: Missing(deps), Manager: m, Found: found}
}

// Command is the command line Run executes, or nil when there's nothing it can run
func (p *Plan) Command() []string {
	if len(p.Missing) == 0 || !p.Found {
		return nil
	}

	var pkgs []string
	for _, dep := range p.Missing {
		if dep.Managed() {
			continue
		}
		pkgs = append(pkgs, dep.Packages(p.Manager)...)
	}
	if len(pkgs) == 0 {
		return nil
	}

	return p.Manager.Command(pkgs)
}

// Downloads are the missing deps bud fetches itself instead of installing
// with the package manager (D107)
func (p *Plan) Downloads() []Tool {
	var tools []Tool
	for _, dep := range p.Missing {
		if t, ok := dep.managed(); ok {
			tools = append(tools, t)
		}
	}

	return tools
}

// Err is a *MissingError naming what's missing, or nil when nothing is
func (p *Plan) Err() error {
	if len(p.Missing) == 0 {
		return nil
	}

	return &MissingError{Deps: p.Missing, Manager: p.Manager, Found: p.Found}
}

// Run downloads what bud fetches itself, installs the rest of the missing deps
// with the package manager, then checks again, since a package can install
// without putting the binary bud looks for on PATH
func (p *Plan) Run() error {
	if tools := p.Downloads(); len(tools) > 0 {
		pp, err := paths.Resolve()
		if err != nil {
			return err
		}
		for _, t := range tools {
			if err := t.Download(pp.Data); err != nil {
				return err
			}
		}
		UseManagedBin(pp.Data)
	}
	argv := p.Command()
	if argv == nil {
		return NewPlan(p.Missing).Err()
	}
	if err := run(argv); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}

	return NewPlan(p.Missing).Err()
}
