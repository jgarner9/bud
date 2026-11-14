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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/agent"
	"jgarner.dev/bud/internal/bundle"
	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/lang"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
	"jgarner.dev/bud/internal/version"
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
var MinNeovim = installer.MinNeovim

// StartupBudget is how long bud may take to show its first screen, empty or on
// a file of any enabled language (principle 1, D35)
var StartupBudget = 50 * time.Millisecond

// MinTreeSitter is the oldest tree-sitter CLI nvim-treesitter's main branch builds parsers with
var MinTreeSitter = installer.MinTreeSitter

// swappable in tests
var (
	nvimVersion = func() (string, error) {
		cmd := exec.Command("nvim", "--version")
		// as bud: Neovim writes its log even for --version, into the state
		// dir its name picks, and uninstall removes bud's
		cmd.Env = append(os.Environ(), "NVIM_APPNAME="+paths.AppName)
		out, err := cmd.Output()
		return string(out), err
	}
	treeSitterVersion = func() (string, error) {
		out, err := exec.Command("tree-sitter", "--version").Output()
		return string(out), err
	}
	embedded      = func() fs.FS { return bud.Config() }
	loadLanguages = lang.Load
	startupTime   = nvim.StartupTime
	claudeCheck   = claude.Check
	lookPath      = exec.LookPath
	kitFS         = func() fs.FS { return bud.Kit() }
	// claude mcp list's output: it checks every server, so it takes seconds
	mcpList = func() (string, error) {
		bin := claude.Find()
		if bin == "" {
			return "", nil
		}
		out, err := exec.Command(bin, "mcp", "list").Output()
		return string(out), err
	}
)

// pluginList is what claude plugin list --json shows
var pluginList = func() ([]kit.Plugin, error) {
	bin := claude.Find()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.Command(bin, "plugin", "list", "--json").Output()
	if err != nil {
		return nil, err
	}
	return kit.ParsePlugins(out)
}

// Options picks which checks Run does
type Options struct {
	// Quick skips the slow ones (startup timing runs nvim several times), for
	// the dashboard, which runs doctor in the background every time it opens
	Quick bool
}

// Run runs every check, in the order they're worth reading
func Run(opts Options) []Result {
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
	if !opts.Quick {
		if r, ok := checkStartup(p, enabled); ok {
			results = append(results, r)
		}
	}

	// the claude backend still runs Claude Code directly (until 1.1 task 6),
	// which the claude code check covers
	if cfg.AI.Backend != "claude" {
		results = append(results, checkAgent(cfg, opts.Quick))
	}

	cr, status := checkClaude(cfg)
	results = append(results, cr)
	if r, ok := checkClaudeCommand(cfg, status); ok {
		results = append(results, r)
	}
	if r, ok := checkCore(p, claudeDir()); ok {
		results = append(results, r)
	}
	if !opts.Quick && status.LoggedIn {
		// claude.ai's connectors only show up through claude mcp list
		if r, ok := checkClaudeSetup(claudeDir()); ok {
			results = append(results, r)
		}
		if r, ok := checkSyncedPlugins(p, cfg, claudeDir()); ok {
			results = append(results, r)
		}
	}

	if r, ok := checkBudApp(p); ok {
		results = append(results, r)
	}

	return append(results, cfgResult, checkProjects(p))
}

// swappable in tests
var (
	findBundles = bundle.Find
	readMac     = bundle.ReadMac
	doctorGOOS  = runtime.GOOS
)

// checkBudApp warns when the Bud.app bud bundle made is out of date, by the
// same rules bud update rebuilds it by (bundle.MacInfo.Stale, JGA-185,
// JGA-187): another bud built it (a dev build doesn't count, being no
// release), or as a copy of kitty, kitty was updated or removed since, or
// the app was moved. bud update rebuilds it, but not when a launch finishes
// an update, which is what this catches; bud bundle does it by hand. Nothing
// to say with no Bud.app, or off macOS
func checkBudApp(p *paths.Paths) (Result, bool) {
	if doctorGOOS != "darwin" {
		return Result{}, false
	}
	for _, b := range findBundles(p) {
		info, err := readMac(b.Path)
		if err != nil {
			continue
		}
		w := bundle.Want{Version: version.String()}
		if !version.Released() {
			w.Version = info.Version
		}
		r := Result{Name: "Bud app"}
		if why := info.Stale(b.Path, w); why != "" {
			r.Status, r.Detail, r.Fix = Warn, b.Path+" is out of date: "+why, "bud bundle"
		} else if info.Kitty != "" {
			r.Detail = b.Path + " is up to date, running kitty " + info.KittyVersion
		} else {
			r.Detail = b.Path + " is up to date"
		}
		return r, true
	}
	return Result{}, false
}

// claudeDir is the Claude Code config dir in use: CLAUDE_CONFIG_DIR, which
// main sets for bud's own, else ~/.claude
func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// checkCore warns when bud-plugins' core.md changed since you last pasted it
// into claude.ai (JGA-179), which the dashboard shows, and when the kit's
// manifest doesn't load at all. Nothing to say without a checkout of core.md
func checkCore(p *paths.Paths, dir string) (Result, bool) {
	k, err := kit.Load(kitFS())
	if err != nil {
		// the one place a manifest that doesn't load is reported: the other
		// kit checks stay quiet about it, so it's said once
		return Result{Name: "kit", Status: Warn, Detail: err.Error(), Fix: "fix kit/kit.json in the dev-linked checkout (docs/internals/kit-manifest.md has the format)"}, true
	}
	if k.Account.Instructions == "" {
		return Result{}, false
	}
	c, err := kit.ReadCore(k.Account.Instructions, dir, p.CorePasted())
	switch {
	case err != nil:
		return Result{Name: "core.md", Status: Warn, Detail: err.Error()}, true
	case c.Path == "":
		return Result{}, false
	case !c.Pasted:
		return Result{Name: "core.md", Status: Warn, Detail: "changed since you last pasted it into claude.ai", Fix: "bud kit core | pbcopy, paste it, then bud kit core --pasted"}, true
	}

	return Result{Name: "core.md", Detail: "the copy in claude.ai is current"}, true
}

// checkClaudeSetup warns when Claude Code's config lacks what bud's sessions
// rely on it for since D77 (JGA-175): the MCP servers kit.json expects, from
// its config or a claude.ai connector, and the core.md rule. All of it comes
// from the user's own ~/.claude (or a claude.ai connector); bud sets none of it
// up. Without them bud still works, with fewer tools and none of the always-on
// instructions. A listed server only counts once it connects (JGA-180)
func checkClaudeSetup(dir string) (Result, bool) {
	k, err := kit.Load(kitFS())
	if err != nil || (len(k.ExpectedServers()) == 0 && k.Account.Instructions == "") {
		return Result{}, false
	}
	r := Result{Name: "claude setup"}
	out, err := mcpList()
	if err != nil {
		r.Status, r.Detail = Warn, "couldn't list Claude Code's MCP servers: "+err.Error()
		return r, true
	}
	servers, multi := parseMCPList(out)

	var missing, failed, auth []string
	for _, name := range k.ExpectedServers() {
		statuses, ok := servers[mcpKey(name)]
		if !ok {
			missing = append(missing, name)
			continue
		}
		// any connected entry (a server and a connector, say) gives the tools
		if slices.ContainsFunc(statuses, mcpConnected) {
			continue
		}
		status := statuses[0]
		problem := name + " (" + status + ")"
		if multi[mcpKey(name)] {
			problem += ", defined in more than one scope, and the one that wins may be the broken one"
		}
		if strings.Contains(strings.ToLower(status), "needs authentication") {
			auth = append(auth, problem)
		} else {
			failed = append(failed, problem)
		}
	}
	if k.Account.Instructions != "" && !hasCoreRule(dir, k.Account.Instructions) {
		missing = append(missing, "the core.md rule")
	}
	if len(missing)+len(failed)+len(auth) == 0 {
		r.Detail = "the MCP servers and core.md rule bud's sessions use are there and connect"
		return r, true
	}

	r.Status = Warn
	var details, fixes []string
	if len(missing) > 0 {
		details = append(details, "Claude Code's config has no "+strings.Join(missing, ", ")+", so bud's sessions go without")
		fixes = append(fixes, "add the servers with claude mcp add or connect them in claude.ai; put the core.md rule in a .md file under ~/.claude/rules/ or in ~/.claude/CLAUDE.md (bud kit core prints it)")
	}
	if len(failed) > 0 {
		details = append(details, "can't connect to "+strings.Join(failed, "; "))
		fixes = append(fixes, "run the server's command by hand to see the error (claude mcp get NAME shows it)")
	}
	if len(auth) > 0 {
		details = append(details, "not logged in to "+strings.Join(auth, "; "))
		fixes = append(fixes, "log in once via /mcp in bud agent")
	}
	r.Detail = strings.Join(details, "; ")
	r.Fix = strings.Join(fixes, "; ")

	return r, true
}

// checkSyncedPlugins warns when another copy of a kit plugin claude.ai syncs
// is installed in dir's Claude Code (JGA-182): Claude Code loads that one
// instead, so the synced one's updates (new skills) never show up. A copy of
// yours is yours to uninstall, since kit sync leaves it alone; bud's own (its
// ledger's, when the ledger is for dir) goes at the next bud kit sync.
// Plugins turned off with kit.disabled and a list claude can't give (the
// claude code check covers a broken claude) leave nothing to say, as does
// claude.ai syncing none of them
func checkSyncedPlugins(p *paths.Paths, cfg config.Settings, dir string) (Result, bool) {
	k, err := kit.Load(kitFS())
	if err != nil {
		return Result{}, false
	}
	k = k.Without(cfg.Kit.Disabled)
	if len(k.Plugins) == 0 {
		return Result{}, false
	}
	installed, err := pluginList()
	if err != nil || !kit.SyncsAny(k, installed) {
		return Result{}, false
	}
	ledger, _, _ := kit.ReadLedger(p.KitLedger())
	if ledger != nil && !paths.SameDir(ledgerDir(ledger), dir) {
		// written for another config dir: nothing in this one is bud's
		ledger = nil
	}
	r := Result{Name: "synced plugins"}
	var yours, buds []kit.Plugin
	for _, c := range kit.Shadowing(k, installed) {
		if kit.Owned(c, ledger) {
			buds = append(buds, c)
		} else {
			yours = append(yours, c)
		}
	}
	if len(yours)+len(buds) == 0 {
		r.Detail = "the plugins claude.ai syncs are the ones that load"
		return r, true
	}
	var details, fixes []string
	if len(yours) > 0 {
		details = append(details, kit.Labels(yours)+" loads instead of the copy claude.ai syncs, so its updates never reach you")
		for _, c := range yours {
			fixes = append(fixes, kit.UninstallHint(c))
		}
	}
	if len(buds) > 0 {
		details = append(details, "bud's own "+kit.Labels(buds)+" is still installed and loads instead of the copy claude.ai syncs")
		fixes = append(fixes, "bud kit sync")
	}
	r.Status = Warn
	r.Detail = strings.Join(details, "; ")
	r.Fix = strings.Join(fixes, "; ") + ", then restart Claude Code"
	return r, true
}

// ledgerDir is the Claude Code config dir a ledger is for: "" is from before
// bud kept its own (JGA-135), which means ~/.claude
func ledgerDir(l *kit.Ledger) string {
	if l.Dir != "" {
		return l.Dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// parseMCPList reads claude mcp list's output: each server's statuses by
// mcpKey ("name: target - ✔ Connected", a claude.ai connector named
// "claude.ai Name"), and the servers its diagnostics say are defined in more
// than one scope. The diagnostics come after the list and aren't servers
func parseMCPList(out string) (servers map[string][]string, multi map[string]bool) {
	servers, multi = map[string][]string{}, map[string]bool{}
	diagnostics := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MCP config diagnostics") {
			diagnostics = true
		}
		if diagnostics {
			if _, rest, ok := strings.Cut(line, `Server "`); ok {
				if name, tail, ok := strings.Cut(rest, `"`); ok && strings.Contains(tail, "multiple scopes") {
					multi[mcpKey(strings.TrimPrefix(name, "claude.ai "))] = true
				}
			}
			continue
		}
		name, rest, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		status := ""
		if i := strings.LastIndex(rest, " - "); i >= 0 {
			status = strings.TrimLeft(rest[i+3:], "✔✓✘✗!⚠ ")
		}
		key := mcpKey(strings.TrimPrefix(name, "claude.ai "))
		servers[key] = append(servers[key], status)
	}

	return servers, multi
}

// mcpConnected says whether a claude mcp list status is a working server. An
// older list printed only the mark, which counts
func mcpConnected(status string) bool {
	return status == "" || strings.HasPrefix(status, "Connected")
}

// mcpKey compares server names the way a person would: "Basic Memory",
// "basic-memory" and "basic_memory" are one server
func mcpKey(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return -1
	}, name)
}

// hasCoreRule says whether Claude Code's config loads core.md's text: a rule
// under rules/, or its CLAUDE.md, starting with the same first line
func hasCoreRule(dir, core string) bool {
	first := "You are Bud."
	if c, err := kit.ReadCore(core, dir, ""); err == nil && c.Path != "" {
		first = strings.TrimSpace(firstLine(c.Text))
	}
	found := false
	check := func(p string) {
		if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), first) {
			found = true
		}
	}
	check(filepath.Join(dir, "CLAUDE.md"))
	_ = filepath.WalkDir(filepath.Join(dir, "rules"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			check(p)
		}
		return nil
	})

	return found
}

// checkClaudeCommand looks for the wrapper ai.claude.command runs Claude Code
// through (D60, Headroom's): without it every session fails to start. Only
// when one is set; it only warns, like the Claude Code check
func checkClaudeCommand(cfg config.Settings, s claude.Status) (Result, bool) {
	cmd := cfg.AI.Claude.Command
	if len(cmd) == 0 {
		return Result{}, false
	}
	r := Result{Name: "claude command"}
	if _, err := lookPath(cmd[0]); err != nil {
		r.Status = Warn
		r.Detail = "ai.claude.command runs " + cmd[0] + ", which isn't on PATH, so Claude Code sessions can't start"
		r.Fix = "bud config unset ai.claude.command"
		return r, true
	}
	// Bedrock and the rest go to their cloud directly, past Headroom's proxy
	// (JGA-149): it starts, but compresses nothing
	if filepath.Base(cmd[0]) == "headroom" && s.Cloud() {
		r.Status = Warn
		r.Detail = "Claude Code on " + s.Method + " talks to it directly, past Headroom's proxy, so Headroom compresses nothing"
		r.Fix = "bud config unset ai.claude.command"
		return r, true
	}
	r.Detail = "sessions run through " + strings.Join(cmd, " ")

	return r, true
}

// ClaudeCheck names the Claude Code check, which the dashboard gives its own banner
const ClaudeCheck = "claude code"

// checkClaude reports Claude Code. It only ever warns: bud works without it,
// there's just no AI (BASE.md 4.2)
func checkClaude(cfg config.Settings) (Result, claude.Status) {
	r := Result{Name: ClaudeCheck}
	s, err := claudeCheck()
	switch {
	case err != nil:
		r.Status, r.Detail = Warn, err.Error()
	case s.Bin == "" && cfg.AI.Backend != "claude":
		// another backend runs the editor's AI (D131)
		r.Detail = "not installed (ai.backend is " + cfg.AI.Backend + ")"
	case s.Bin == "" && !cfg.AI.Claude.Autostart:
		r.Detail = "not installed (ai.claude.autostart is off)"
	case s.Bin == "":
		r.Status, r.Detail, r.Fix = Warn, "not installed, so there's no AI in the editor", "bud claude setup"
	case !s.LoggedIn && s.Elsewhere != "":
		r.Status, r.Detail, r.Fix = Warn, s.Version+", not logged in here; ~/.claude runs it on "+s.Elsewhere+", which bud's own config dir doesn't see", "bud claude setup"
	case !s.LoggedIn:
		r.Status, r.Detail, r.Fix = Warn, s.Version+", not logged in", "bud claude setup"
	default:
		r.Detail = s.Version + ", logged in with " + s.Method
	}

	return r, s
}

// agentCheck is swappable in tests
var agentCheck = agent.Check

// AgentCheck is the AI backend check's name
const AgentCheck = "ai backend"

// checkAgent checks the backend ai.backend names (D131): its command, and
// for OpenClaw the Gateway (not with --quick, which skips the network). It
// only warns: bud works without AI
func checkAgent(cfg config.Settings, quick bool) Result {
	r := Result{Name: AgentCheck}
	st := agentCheck(cfg, quick)
	var detail []string
	if st.Version != "" {
		detail = append(detail, st.Version)
	}
	if st.Reachable {
		detail = append(detail, "Gateway at "+st.Gateway+" answering")
	}
	if len(st.Problems) == 0 {
		r.Detail = st.Backend
		if len(detail) > 0 {
			r.Detail += ": " + strings.Join(detail, ", ")
		}
		return r
	}
	var whats, fixes []string
	for _, p := range st.Problems {
		whats = append(whats, p.What)
		fixes = append(fixes, p.Fix)
	}
	r.Status = Warn
	r.Detail = st.Backend + ": " + strings.Join(whats, "; ")
	r.Fix = strings.Join(fixes, "; ")
	return r
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
	median := func(filetype string) (time.Duration, error) {
		var runs []time.Duration
		for range 3 {
			d, err := startupTime(filetype)
			if err != nil {
				return 0, err
			}
			runs = append(runs, d)
		}
		slices.Sort(runs)
		return runs[1], nil
	}

	boot, err := median("")
	if err != nil {
		r.Status = Warn
		r.Detail = err.Error()
		return r, true
	}

	// one run per language is enough unless it's over budget
	slowName, slow := "", time.Duration(0)
	for _, spec := range enabled {
		ft := spec.Filetypes[0]
		d, err := startupTime(ft)
		if err == nil && d > StartupBudget {
			d, err = median(ft)
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

	dir := p.LanguageSpecs()
	userDir := p.UserLanguageSpecs()
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
	return checkDepList(deps, Warn, ", a language needs it")
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
	return checkDepList(installer.Needed(profile), Fail, "")
}

// checkDepList reports each dep, marking a missing one with status and the
// package manager command that installs it. why is added to the detail
func checkDepList(deps []installer.Dep, status Status, why string) []Result {
	m, found := installer.DetectManager()

	var results []Result
	for _, dep := range deps {
		r := Result{Name: dep.Name}
		if !dep.Installed() {
			r.Status = status
			r.Detail = "not found on PATH (" + strings.Join(dep.Bins, " or ") + ")" + why
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
		r.Fix = upgradeFix("nvim", "upgrade Neovim with your package manager")
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
		r.Fix = upgradeFix("tree-sitter", "upgrade tree-sitter-cli with your package manager, or install a newer release from GitHub")
	}

	return r, true
}

// upgradeFix says how to get a newer bin: on Linux 'bud install' downloads one
// when the one on PATH is too old (D107); elsewhere it's the package manager's
func upgradeFix(bin, other string) string {
	for _, dep := range installer.Deps {
		if dep.Managed() && dep.Tool.Bin == bin {
			return "run 'bud install': it downloads a current " + dep.Name
		}
	}

	return other
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// checkConfigSync compares ~/.config/bud with the config embedded in this binary
func checkConfigSync(p *paths.Paths) Result {
	r := Result{Name: "nvim config"}
	if checkout, ok := nvim.DevLink(p.Config); ok {
		r.Detail = "linked to " + checkout + " (bud dev unlink)"
		return r
	}
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
