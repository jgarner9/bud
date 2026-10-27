// Package doctor runs bud's health checks. A Fail means bud is broken on this
// machine, which the updater relies on to decide when to roll back (BASE.md 5.7);
// a Warn is worth knowing about but doesn't stop anything
package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

type Status int

const (
	OK Status = iota
	Warn
	Fail
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	default:
		return "FAIL"
	}
}

type Result struct {
	Name   string
	Status Status
	Detail string // what was found
	Fix    string // what to run about it, when it isn't OK
}

// MinNeovim is the oldest Neovim bud supports (D19)
var MinNeovim = [2]int{0, 11}

// swappable in tests
var (
	nvimVersion = func() (string, error) {
		out, err := exec.Command("nvim", "--version").Output()
		return string(out), err
	}
	embedded = func() fs.FS { return bud.NvimConfig() }
)

// Run runs every check, in the order they're worth reading
func Run() []Result {
	p, err := paths.Resolve()
	if err != nil {
		return []Result{{Name: "paths", Status: Fail, Detail: err.Error()}}
	}

	// a broken config is reported by its own check; the deps fall back to the full profile
	cfg, cfgResult := checkMachineConfig(p)

	results := checkDeps(cfg.Profile)
	if r, ok := checkNeovim(); ok {
		results = append(results, r)
	}

	return append(results, checkConfigSync(p), cfgResult)
}

// Failed counts the results that are Fail
func Failed(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Status == Fail {
			n++
		}
	}

	return n
}

// checkDeps reports the deps the profile installs (installer.Needed)
func checkDeps(profile string) []Result {
	m, found := installer.DetectManager()

	var results []Result
	for _, dep := range installer.Needed(profile) {
		r := Result{Name: dep.Name}
		if !dep.Installed() {
			r.Status = Fail
			r.Detail = "not found on PATH (" + strings.Join(dep.Bins, " or ") + ")"
			if found {
				r.Fix = strings.Join(m.Command(dep.Packages(m)), " ")
			} else {
				r.Fix = "install it with your system's package manager"
			}
		}
		results = append(results, r)
	}

	return results
}

var nvimVersionRe = regexp.MustCompile(`^NVIM v(\d+)\.(\d+)\.(\d+)\S*`)

// checkNeovim reports the Neovim version. It's skipped when nvim isn't on PATH,
// since checkDeps already says so
func checkNeovim() (Result, bool) {
	if _, err := exec.LookPath("nvim"); err != nil {
		return Result{}, false
	}

	r := Result{Name: "neovim version"}
	out, err := nvimVersion()
	if err != nil {
		r.Status = Fail
		r.Detail = "nvim --version: " + err.Error()
		return r, true
	}

	version, major, minor, ok := parseNvimVersion(out)
	if !ok {
		r.Status = Fail
		r.Detail = fmt.Sprintf("can't read the version from %q", firstLine(out))
		return r, true
	}

	r.Detail = version
	if major < MinNeovim[0] || (major == MinNeovim[0] && minor < MinNeovim[1]) {
		r.Status = Fail
		r.Detail += fmt.Sprintf(", bud needs %d.%d or newer", MinNeovim[0], MinNeovim[1])
		r.Fix = "upgrade Neovim with your package manager"
	}

	return r, true
}

func parseNvimVersion(out string) (version string, major, minor int, ok bool) {
	m := nvimVersionRe.FindStringSubmatch(firstLine(out))
	if m == nil {
		return "", 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])

	return strings.TrimPrefix(m[0], "NVIM "), major, minor, true
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// checkConfigSync compares ~/.config/bud with the config embedded in this binary
func checkConfigSync(p *paths.Paths) Result {
	r := Result{Name: "nvim config"}
	if !nvim.HasManifest(p.Config) {
		r.Status = Fail
		r.Detail = "not installed in " + p.Config
		r.Fix = "bud install"
		return r
	}

	res, err := nvim.CheckConfig(embedded(), p.Config)
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		r.Fix = "bud update"
		return r
	}

	var problems []string
	if n := len(res.Added); n > 0 {
		// a missing file can break startup, so it's a failure
		r.Status = Fail
		problems = append(problems, plural(n, "file")+" missing")
	}
	if n := len(res.Updated); n > 0 {
		r.Status = max(r.Status, Warn)
		problems = append(problems, plural(n, "file")+" changed by hand")
	}
	if n := len(res.Removed); n > 0 {
		r.Status = max(r.Status, Warn)
		problems = append(problems, plural(n, "file")+" left over from an older release")
	}

	if len(problems) == 0 {
		r.Detail = "in sync at " + p.Config
		return r
	}
	r.Detail = strings.Join(problems, ", ")
	r.Fix = "bud update"

	return r
}

// checkMachineConfig returns the settings, or the defaults when config.json is broken
func checkMachineConfig(p *paths.Paths) (config.Settings, Result) {
	r := Result{Name: "machine config"}

	s := config.Defaults()
	f, err := config.Load(p.MachineConfig())
	if err == nil {
		s, err = f.Settings()
	}
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		r.Fix = "fix or remove " + p.MachineConfig()
		return config.Defaults(), r
	}

	r.Detail = p.MachineConfig()
	if _, err := os.Stat(p.MachineConfig()); err != nil {
		r.Detail = "defaults (no " + p.MachineConfig() + ")"
	}

	return s, r
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	return fmt.Sprintf("%d %ss", n, word)
}
