// Package doctor runs bud's health checks. A Fail means bud is broken on this
// machine, which the updater relies on to decide when to roll back (BASE.md 5.7);
// a Warn is worth knowing about but doesn't stop anything
package doctor

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/lang"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
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
var MinNeovim = [2]int{0, 12}

// StartupBudget is how long bud may take to show its first screen, empty or on
// a file of any enabled language (principle 1, D35)
var StartupBudget = 50 * time.Millisecond

// MinTreeSitter is the oldest tree-sitter CLI nvim-treesitter's main branch builds parsers with
var MinTreeSitter = [3]int{0, 26, 1}

// swappable in tests
var (
	nvimVersion = func() (string, error) {
		out, err := exec.Command("nvim", "--version").Output()
		return string(out), err
	}
	treeSitterVersion = func() (string, error) {
		out, err := exec.Command("tree-sitter", "--version").Output()
		return string(out), err
	}
	embedded      = func() fs.FS { return bud.Config() }
	loadLanguages = lang.Load
	startupTime   = nvim.StartupTime
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
	langResults, enabled := checkLanguages(p, cfg)
	results = append(results, checkLanguageDeps(installer.ForBins(lang.System(enabled)))...)
	if r, ok := checkNeovim(); ok {
		results = append(results, r)
	}
	if r, ok := checkTreeSitter(); ok {
		results = append(results, r)
	}
	results = append(results, checkConfigSync(p))
	results = append(results, langResults...)
	if r, ok := checkStartup(p, enabled); ok {
		results = append(results, r)
	}

	return append(results, cfgResult, checkProjects(p))
}

// checkProjects reads the project registry. A broken one only warns: bud still
// opens, it just can't list or register projects
func checkProjects(p *paths.Paths) Result {
	r := Result{Name: "projects"}
	reg, err := projects.Load(p.Projects())
	if err != nil {
		r.Status = Warn
		r.Detail = err.Error()
		r.Fix = "fix or remove " + p.Projects()
		return r
	}

	missing := 0
	for _, proj := range reg.Projects {
		if proj.Missing() {
			missing++
		}
	}
	r.Detail = plural(len(reg.Projects), "project") + " registered"
	if missing > 0 {
		r.Detail += fmt.Sprintf(", %d missing (bud projects rm forgets one)", missing)
	}

	return r
}

// checkStartup times bud's first screen: empty, and on a buffer of each enabled
// language. Past StartupBudget only warns, since slow isn't broken. Skipped when
// nvim is missing or the config isn't synced, which other checks report
func checkStartup(p *paths.Paths, enabled []lang.Spec) (Result, bool) {
	if !nvim.HasManifest(p.Config) {
		return Result{}, false
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		return Result{}, false
	}

	r := Result{Name: "startup"}
	// the median of three, so one slow run doesn't count
	median := func(args ...string) (time.Duration, error) {
		var runs []time.Duration
		for range 3 {
			d, err := startupTime(args...)
			if err != nil {
				return 0, err
			}
			runs = append(runs, d)
		}
		slices.Sort(runs)
		return runs[1], nil
	}

	boot, err := median()
	if err != nil {
		r.Status = Warn
		r.Detail = err.Error()
		return r, true
	}

	// one run per language is enough unless it's over budget
	slowName, slow := "", time.Duration(0)
	for _, spec := range enabled {
		args := []string{"-c", "setfiletype " + spec.Filetypes[0]}
		d, err := startupTime(args...)
		if err == nil && d > StartupBudget {
			d, err = median(args...)
		}
		if err != nil {
			r.Status = Warn
			r.Detail = spec.Name + ": " + err.Error()
			return r, true
		}
		if d > slow {
			slowName, slow = spec.Name, d
		}
	}

	ms := func(d time.Duration) string { return fmt.Sprintf("%d ms", d.Round(time.Millisecond).Milliseconds()) }
	r.Detail = ms(boot)
	if slowName != "" {
		r.Detail += ", slowest language " + slowName + " " + ms(slow)
	}
	if boot > StartupBudget || slow > StartupBudget {
		r.Status = Warn
		r.Detail += ", over the " + ms(StartupBudget) + " budget"
		r.Fix = "NVIM_APPNAME=bud nvim --startuptime startup.log shows where the time goes"
	}

	return r, true
}

// checkLanguages reads the synced specs and reports the ones that don't load and
// the enabled names with no spec. A broken spec bud shipped is a Fail (a bad
// release, which an update should roll back); one the user added, or a name
// with no spec, only warns, since the rest of the editor still works (D33). It
// also returns the enabled specs. Skipped, with no results, until the config is
// synced or when nvim is missing, which other checks report
func checkLanguages(p *paths.Paths, cfg config.Settings) ([]Result, []lang.Spec) {
	if !nvim.HasManifest(p.Config) {
		return nil, nil
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		return nil, nil
	}

	dir := filepath.Join(p.Config, "languages")
	userDir := filepath.Join(p.UserTools(), "languages")
	specs, err := loadLanguages(false, dir, userDir)
	if err != nil {
		return []Result{{Name: "languages", Status: Fail, Detail: err.Error(), Fix: "bud update"}}, nil
	}
	managed, err := nvim.ManagedFiles(p.Config)
	if err != nil {
		return []Result{{Name: "languages", Status: Fail, Detail: err.Error(), Fix: "bud update"}}, nil
	}

	var results []Result
	for _, name := range slices.Sorted(maps.Keys(specs.Errors)) {
		r := Result{Name: "language " + name, Detail: specs.Errors[name]}
		if slices.Contains(managed, lang.File(name)) {
			r.Status = Fail
			r.Fix = "bud update"
		} else {
			r.Status = Warn
			file := filepath.Join(userDir, name+".lua")
			if _, err := os.Stat(file); err != nil {
				file = filepath.Join(dir, name+".lua")
			}
			r.Fix = "fix or remove " + file
		}
		results = append(results, r)
	}

	enabled, unknown := specs.Enabled(cfg.Languages, cfg.Profile)
	if len(unknown) > 0 {
		results = append(results, Result{
			Name:   "languages",
			Status: Warn,
			Detail: "no spec for " + strings.Join(unknown, ", "),
			Fix:    "remove them from languages in " + p.MachineConfig(),
		})
	}

	var names []string
	for _, spec := range enabled {
		names = append(names, spec.Name)
	}
	detail := "none enabled"
	if len(names) > 0 {
		detail = strings.Join(names, ", ")
	}
	results = append([]Result{{Name: "languages", Detail: detail}}, results...)

	return results, enabled
}

// checkLanguageDeps reports the enabled languages' system deps. A missing one
// warns: only that language's tooling is affected
func checkLanguageDeps(deps []installer.Dep) []Result {
	m, found := installer.DetectManager()

	var results []Result
	for _, dep := range deps {
		r := Result{Name: dep.Name}
		if !dep.Installed() {
			r.Status = Warn
			r.Detail = "not found on PATH (" + strings.Join(dep.Bins, " or ") + "), a language needs it"
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

var treeSitterVersionRe = regexp.MustCompile(`^tree-sitter (\d+)\.(\d+)\.(\d+)`)

// checkTreeSitter reports the tree-sitter CLI's version. Too old only warns:
// the editor still works, but new parsers won't build. It's skipped when the
// CLI isn't on PATH, since checkDeps already says so
func checkTreeSitter() (Result, bool) {
	if _, err := exec.LookPath("tree-sitter"); err != nil {
		return Result{}, false
	}

	r := Result{Name: "tree-sitter version"}
	out, err := treeSitterVersion()
	if err != nil {
		r.Status = Warn
		r.Detail = "tree-sitter --version: " + err.Error()
		return r, true
	}

	m := treeSitterVersionRe.FindStringSubmatch(firstLine(out))
	if m == nil {
		r.Status = Warn
		r.Detail = fmt.Sprintf("can't read the version from %q", firstLine(out))
		return r, true
	}

	var have [3]int
	for i := range have {
		have[i], _ = strconv.Atoi(m[i+1])
	}
	r.Detail = strings.TrimPrefix(m[0], "tree-sitter ")
	if slices.Compare(have[:], MinTreeSitter[:]) < 0 {
		r.Status = Warn
		r.Detail += fmt.Sprintf(", parsers need %d.%d.%d or newer", MinTreeSitter[0], MinTreeSitter[1], MinTreeSitter[2])
		r.Fix = "upgrade tree-sitter-cli with your package manager, or install a newer release from GitHub"
	}

	return r, true
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
