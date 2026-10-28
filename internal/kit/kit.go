// Package kit syncs bud's Claude Code setup (BASE.md 4.1): MCP servers,
// skills, subagents, slash commands and global instructions, from the kit/
// embedded in the binary into Claude Code's user config.
//
// bud only touches what it wrote: a ledger records each item with a hash of
// what was written, so the next sync updates what changed, removes what the
// kit dropped, and leaves anything you set up by hand alone. MCP servers go
// through `claude mcp`, never by editing its files. An entry never holds a
// secret: `$secret:NAME` becomes Claude Code's `${NAME}`, expanded when it
// runs, and a secret that isn't set skips that server, or for an entry with
// optional_secrets, just that header or variable.
package kit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Kit is the kit as the repo describes it
type Kit struct {
	MCP          []MCP
	Skills       []string
	Plugins      []string // Claude Code plugins, name@marketplace
	Agents       []string
	Commands     []string
	Instructions string

	fsys fs.FS
}

// MCP is one MCP server entry, kit/mcp/<name>.json
type MCP struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Transport       string            `json:"transport"` // "http", "sse" or "stdio"
	URL             string            `json:"url,omitempty"`
	Command         string            `json:"command,omitempty"`
	Args            []string          `json:"args,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	OptionalSecrets bool              `json:"optional_secrets,omitempty"`
	// a binary the server needs on PATH, and how to install it; without it
	// sync skips the server and says how
	Requires *Requires `json:"requires,omitempty"`
	// only for big projects: then it isn't synced for every session, and bud
	// adds it to the sessions it starts in a project that's big enough (JGA-117)
	When     *When    `json:"when,omitempty"`
	Profiles []string `json:"profiles,omitempty"` // empty means every profile
	// the claude.ai connector that provides the same service ("Context7"):
	// where Claude Code has it, sync leaves the kit's entry out (JGA-128).
	// Empty matches a connector by the entry's name or its URL's host
	Connector string `json:"connector,omitempty"`
}

// When is which projects an entry loads in, instead of every session. Large:
// big enough, by either threshold (zero means config.json's kit.large_files,
// kit.large_loc). Files and Packages: the project uses it, when one of Files is
// at its root or its package.json depends on one of Packages. Both kinds of
// condition, when set, have to hold
type When struct {
	Large    bool     `json:"large"`
	MinFiles int      `json:"min_files,omitempty"`
	MinLOC   int64    `json:"min_loc,omitempty"`
	Files    []string `json:"files,omitempty"`
	Packages []string `json:"packages,omitempty"`
}

// Project is what When looks at: the root, and its size (projects.Measure)
type Project struct {
	Root  string
	Files int
	LOC   int64
}

// Fits says whether p is a project for w, with defFiles and defLOC standing
// in for the size thresholds w leaves out
func (w When) Fits(p Project, defFiles int, defLOC int64) bool {
	if w.Large && !w.Big(p.Files, p.LOC, defFiles, defLOC) {
		return false
	}
	if len(w.Files) == 0 && len(w.Packages) == 0 {
		return true
	}
	hasFile := slices.ContainsFunc(w.Files, func(name string) bool {
		_, err := os.Stat(filepath.Join(p.Root, name))
		return err == nil
	})

	return hasFile || slices.ContainsFunc(w.Packages, packageDeps(p.Root))
}

// packageDeps reads root's package.json and returns whether a package is one
// of its dependencies (dev ones too)
func packageDeps(root string) func(string) bool {
	var pkg struct {
		Dependencies    map[string]any `json:"dependencies"`
		DevDependencies map[string]any `json:"devDependencies"`
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		_ = json.Unmarshal(data, &pkg)
	}

	return func(name string) bool {
		_, dep := pkg.Dependencies[name]
		_, dev := pkg.DevDependencies[name]
		return dep || dev
	}
}

// Describe says which projects w is for, for sync's skip message
func (w When) Describe() string {
	switch {
	case w.Large:
		return "large projects"
	case len(w.Packages) > 0 || len(w.Files) > 0:
		names := append(slices.Clone(w.Files), w.Packages...)
		if len(names) > 3 {
			names = append(names[:3], "...")
		}
		return "projects that use it (" + strings.Join(names, ", ") + ")"
	}

	return "some projects"
}

// Big says whether a project of files and loc is big enough for w, with the
// defaults standing in for thresholds it leaves out
func (w When) Big(files int, loc int64, defFiles int, defLOC int64) bool {
	minFiles, minLOC := w.MinFiles, w.MinLOC
	if minFiles == 0 {
		minFiles = defFiles
	}
	if minLOC == 0 {
		minLOC = defLOC
	}

	return (minFiles > 0 && files >= minFiles) || (minLOC > 0 && loc >= minLOC)
}

// SessionServers is the --mcp-config bud adds to a session in project p: the
// conditional entries it fits (When), that are on, have their binary and
// secrets. nil when there are none
func (k *Kit) SessionServers(profile string, p Project, defFiles int, defLOC int64, disabled []string, set func(string) bool, hasBinary func(string) bool) ([]byte, error) {
	servers := map[string]json.RawMessage{}
	for _, m := range k.MCP {
		if m.When == nil || !m.For(profile) || slices.Contains(disabled, m.Name) || !m.When.Fits(p, defFiles, defLOC) {
			continue
		}
		if m.Requires != nil && m.Requires.Bin != "" && !hasBinary(m.Requires.Bin) {
			continue
		}
		server, missing, err := m.Server(set)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			servers[m.Name] = server
		}
	}
	if len(servers) == 0 {
		return nil, nil
	}

	return json.Marshal(map[string]any{"mcpServers": servers})
}

type Requires struct {
	Bin     string `json:"bin"`
	Install string `json:"install"`
}

// Load reads the kit from fsys, laid out like the repo's kit/
func Load(fsys fs.FS) (*Kit, error) {
	data, err := fs.ReadFile(fsys, "kit.json")
	if err != nil {
		return nil, err
	}
	var index struct {
		MCP      []string `json:"mcp"`
		Skills   []string `json:"skills"`
		Plugins  []string `json:"plugins"`
		Agents   []string `json:"agents"`
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}
	k := &Kit{Skills: index.Skills, Plugins: index.Plugins, Agents: index.Agents, Commands: index.Commands, fsys: fsys}
	for _, name := range index.MCP {
		data, err := fs.ReadFile(fsys, "mcp/"+name+".json")
		if err != nil {
			return nil, err
		}
		var m MCP
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("mcp/%s.json: %w", name, err)
		}
		if m.Name == "" {
			m.Name = name
		}
		k.MCP = append(k.MCP, m)
	}
	if data, err := fs.ReadFile(fsys, "CLAUDE.md"); err == nil {
		k.Instructions = strings.TrimSpace(string(data))
	}

	return k, nil
}

// For reports whether the entry is on for profile
func (m MCP) For(profile string) bool {
	return len(m.Profiles) == 0 || slices.Contains(m.Profiles, profile)
}

const secretPrefix = "$secret:"

// secretRef finds $secret:NAME anywhere in a value ("Bearer $secret:GITHUB_PAT")
var secretRef = regexp.MustCompile(`\$secret:([A-Za-z_][A-Za-z0-9_]*)`)

// SecretRefs lists the secret names a value refers to
func SecretRefs(v string) []string {
	var out []string
	for _, m := range secretRef.FindAllStringSubmatch(v, -1) {
		out = append(out, m[1])
	}

	return out
}

// Server is the JSON `claude mcp add-json` takes, with each $secret:NAME
// turned into ${NAME}. set says whether a secret is available; missing lists
// the required ones that aren't, and then there's no server to add
func (m MCP) Server(set func(name string) bool) (server []byte, missing []string, err error) {
	resolve := func(in map[string]string) map[string]string {
		if len(in) == 0 {
			return nil
		}
		out := map[string]string{}
		for k, v := range in {
			names := SecretRefs(v)
			var absent []string
			for _, name := range names {
				if !set(name) {
					absent = append(absent, name)
				}
			}
			if len(absent) > 0 {
				if !m.OptionalSecrets {
					missing = append(missing, absent...)
				}
				continue
			}
			out[k] = secretRef.ReplaceAllString(v, "$${$1}")
		}
		return out
	}
	s := map[string]any{"type": m.Transport}
	if m.URL != "" {
		// a URL that differs per machine (a self-hosted server) is a secret
		// too; without it there's nothing to connect to, optional or not
		for _, name := range SecretRefs(m.URL) {
			if !set(name) {
				missing = append(missing, name)
			}
		}
		s["url"] = secretRef.ReplaceAllString(m.URL, "$${$1}")
	}
	if m.Command != "" {
		s["command"] = m.Command
		s["args"] = m.Args
	}
	if h := resolve(m.Headers); len(h) > 0 {
		s["headers"] = h
	}
	if e := resolve(m.Env); len(e) > 0 {
		s["env"] = e
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, missing, nil
	}
	server, err = json.Marshal(s)

	return server, nil, err
}

// Ledger is what bud wrote into Claude Code's config, with a hash of each
type Ledger struct {
	MCP          map[string]string `json:"mcp"`
	Plugins      map[string]string `json:"plugins,omitempty"`
	Files        map[string]string `json:"files"` // path under the Claude dir -> hash
	Instructions string            `json:"instructions,omitempty"`
	// the Claude Code config dir all this is in; "" from before bud kept its
	// own (JGA-135) means ~/.claude
	Dir string `json:"dir,omitempty"`
}

// ReadLedger reads path; a missing ledger is an empty one, and false
func ReadLedger(path string) (*Ledger, bool, error) {
	l := &Ledger{MCP: map[string]string{}, Plugins: map[string]string{}, Files: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(data, l); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	if l.Plugins == nil {
		l.Plugins = map[string]string{}
	}
	if l.MCP == nil {
		l.MCP = map[string]string{}
	}
	if l.Files == nil {
		l.Files = map[string]string{}
	}

	return l, true, nil
}

func (l *Ledger) save(path string) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return atomicfile.WriteFile(path, append(data, '\n'), 0o644)
}

// Own records that bud manages something you adopted into the kit: an MCP
// server (hash "", so the next sync rewrites it with the kit's version) or
// the files of a skill, agent or command as they are now
func Own(ledgerPath, claudeDir, kind, name string) error {
	l, _, err := ReadLedger(ledgerPath)
	if err != nil {
		return err
	}
	if kind == "mcp" {
		l.MCP[name] = ""
		return l.save(ledgerPath)
	}
	root := filepath.Join(claudeDir, filepath.FromSlash(entryPath(kind, name)))
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(claudeDir, p)
		l.Files[filepath.ToSlash(rel)] = hash(data)
		return nil
	})
	if err != nil {
		return err
	}

	return l.save(ledgerPath)
}

// Env is where sync writes and how it runs claude
type Env struct {
	ClaudeDir  string // ~/.claude
	ClaudeJSON string // ~/.claude.json, where user-scope MCP servers live
	Ledger     string
	Profile    string
	Claude     func(args ...string) error // runs claude
	// lists the installed plugins (claude plugin list --json); nil means none
	InstalledPlugins func() ([]string, error)
	SecretSet        func(name string) bool
	HasBin           func(name string) bool // nil means look on PATH
	Disabled         []string               // entries turned off on this machine (bud kit disable)
	// the claude.ai connectors Claude Code has (claude mcp list); nil means none
	Connectors func() ([]Connector, error)
}

// Connector is one claude.ai connector, as claude mcp list shows it
type Connector struct {
	Name string // "Context7", without the "claude.ai " prefix
	URL  string
}

// ProvidedBy returns the connector that already gives Claude Code what m
// does: one named like m.Connector (or m's own name), or at m's URL's host
func (m MCP) ProvidedBy(connectors []Connector) (Connector, bool) {
	name := m.Connector
	if name == "" {
		name = m.Name
	}
	for _, c := range connectors {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
		if h := host(m.URL); h != "" && h == host(c.URL) {
			return c, true
		}
	}

	return Connector{}, false
}

func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return strings.ToLower(u.Hostname())
}

// ParseConnectors reads the claude.ai connectors out of claude mcp list's
// output ("claude.ai Context7: https://mcp.context7.com/mcp - ✔ Connected").
// Other lines (local servers, a wrapper's banner) are skipped
func ParseConnectors(out string) []Connector {
	var list []Connector
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "claude.ai ")
		if !ok {
			continue
		}
		name, after, ok := strings.Cut(rest, ": ")
		if !ok {
			continue
		}
		u, _, _ := strings.Cut(after, " - ")
		list = append(list, Connector{Name: strings.TrimSpace(name), URL: strings.TrimSpace(u)})
	}

	return list
}

// without is k minus the named entries
func (k *Kit) without(names []string) *Kit {
	if len(names) == 0 {
		return k
	}
	keep := func(list []string) []string {
		return slices.DeleteFunc(slices.Clone(list), func(n string) bool { return slices.Contains(names, n) })
	}
	out := *k
	out.MCP = slices.DeleteFunc(slices.Clone(k.MCP), func(m MCP) bool { return slices.Contains(names, m.Name) })
	out.Skills, out.Agents, out.Commands, out.Plugins = keep(k.Skills), keep(k.Agents), keep(k.Commands), keep(k.Plugins)

	return &out
}

// Action is one thing sync does, or would do with --dry-run
type Action struct {
	Kind string // "mcp", "skill", "agent", "command", "instructions"
	Name string
	Do   string // "add", "update", "remove", "skip", "ok"
	Why  string
}

func (a Action) String() string {
	s := fmt.Sprintf("%-6s %-12s %s", a.Do, a.Kind, a.Name)
	if a.Why != "" {
		s += "  (" + a.Why + ")"
	}

	return strings.TrimRight(s, " ")
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// userServers reads the user-scope MCP servers Claude Code has
func userServers(path string) map[string]json.RawMessage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}

	return cfg.MCPServers
}

// file is one kit file and where it goes under the Claude dir
type file struct {
	kind, name, src, dest string
}

func (k *Kit) files() ([]file, error) {
	var out []file
	for _, name := range k.Skills {
		err := fs.WalkDir(k.fsys, "skills/"+name, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			out = append(out, file{"skill", name, p, p})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, g := range []struct {
		kind, dir string
		names     []string
	}{{"agent", "agents", k.Agents}, {"command", "commands", k.Commands}} {
		for _, name := range g.names {
			p := g.dir + "/" + name + ".md"
			out = append(out, file{g.kind, name, p, p})
		}
	}

	return out, nil
}

// the fenced block bud owns in ~/.claude/CLAUDE.md
const (
	blockStart = "<!-- bud:begin (written by bud kit sync; edits in here are replaced) -->"
	blockEnd   = "<!-- bud:end -->"
)

// withBlock returns doc with bud's block set to body, or taken out for ""
func withBlock(doc, body string) string {
	before, rest, found := strings.Cut(doc, blockStart)
	after := ""
	if found {
		if _, a, ok := strings.Cut(rest, blockEnd); ok {
			after = strings.TrimPrefix(a, "\n")
		}
	} else {
		before = doc
	}
	before = strings.TrimRight(before, "\n")
	if body == "" {
		out := before
		if after != "" {
			if out != "" {
				out += "\n\n"
			}
			out += after
		}
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out
	}
	block := blockStart + "\n" + body + "\n" + blockEnd + "\n"
	if before != "" {
		block = before + "\n\n" + block
	}
	if after != "" {
		block += "\n" + after
	}

	return block
}

// Sync brings Claude Code's config in line with the kit. With dryRun it only
// says what it would do. It keeps going past one item failing, and returns
// every failure at the end
func Sync(k *Kit, env Env, dryRun bool) ([]Action, error) {
	k = k.without(env.Disabled)
	ledger, _, err := ReadLedger(env.Ledger)
	if err != nil {
		return nil, err
	}
	var actions []Action
	var errs []error
	have := userServers(env.ClaudeJSON)
	var connectors []Connector
	if env.Connectors != nil {
		// without the list, sync goes on as if there were none
		connectors, _ = env.Connectors()
	}
	provided := map[string]string{}

	// MCP servers
	wanted := map[string]bool{}
	for _, m := range k.MCP {
		if !m.For(env.Profile) {
			continue
		}
		if m.When != nil {
			// not for every session: bud adds it per project (SessionServers)
			actions = append(actions, Action{"mcp", m.Name, "skip", "only for " + m.When.Describe() + "; bud adds it to their sessions"})
			continue
		}
		if c, ok := m.ProvidedBy(connectors); ok {
			provided[m.Name] = c.Name
			if _, inLedger := ledger.MCP[m.Name]; !inLedger {
				actions = append(actions, Action{"mcp", m.Name, "skip", "provided by the claude.ai " + c.Name + " connector"})
			}
			continue
		}
		wanted[m.Name] = true
		if r := m.Requires; r != nil && r.Bin != "" && !hasBin(env, r.Bin) {
			actions = append(actions, Action{"mcp", m.Name, "skip", "needs " + r.Bin + ": " + r.Install})
			continue
		}
		server, missing, err := m.Server(env.SecretSet)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(missing) > 0 {
			actions = append(actions, Action{"mcp", m.Name, "skip", "set " + strings.Join(missing, ", ")})
			continue
		}
		h := hash(server)
		_, present := have[m.Name]
		owned, inLedger := ledger.MCP[m.Name]
		switch {
		case present && !inLedger:
			actions = append(actions, Action{"mcp", m.Name, "skip", "you have your own " + m.Name})
			continue
		case present && owned == h:
			actions = append(actions, Action{"mcp", m.Name, "ok", ""})
			continue
		}
		do := "add"
		if present {
			do = "update"
		}
		actions = append(actions, Action{"mcp", m.Name, do, ""})
		if dryRun {
			continue
		}
		if present {
			_ = env.Claude("mcp", "remove", "--scope", "user", m.Name)
		}
		if err := env.Claude("mcp", "add-json", "--scope", "user", m.Name, string(server)); err != nil {
			errs = append(errs, fmt.Errorf("adding %s: %w", m.Name, err))
			continue
		}
		ledger.MCP[m.Name] = h
	}
	for _, name := range sortedKeys(ledger.MCP) {
		if wanted[name] {
			continue
		}
		why := "no longer in the kit"
		if c, ok := provided[name]; ok {
			why = "provided by the claude.ai " + c + " connector"
		}
		actions = append(actions, Action{"mcp", name, "remove", why})
		if dryRun {
			continue
		}
		if _, present := have[name]; present {
			if err := env.Claude("mcp", "remove", "--scope", "user", name); err != nil {
				errs = append(errs, fmt.Errorf("removing %s: %w", name, err))
				continue
			}
		}
		delete(ledger.MCP, name)
	}

	// plugins: through claude plugin, user scope
	if len(k.Plugins) > 0 || len(ledger.Plugins) > 0 {
		var installed []string
		if env.InstalledPlugins != nil {
			if installed, err = env.InstalledPlugins(); err != nil {
				errs = append(errs, fmt.Errorf("listing plugins: %w", err))
			}
		}
		for _, id := range k.Plugins {
			_, owned := ledger.Plugins[id]
			switch {
			case slices.Contains(installed, id) && owned:
				actions = append(actions, Action{"plugin", id, "ok", ""})
			case slices.Contains(installed, id):
				actions = append(actions, Action{"plugin", id, "skip", "you installed it yourself"})
			default:
				actions = append(actions, Action{"plugin", id, "add", ""})
				if dryRun {
					continue
				}
				if err := env.Claude("plugin", "install", "--scope", "user", id); err != nil {
					errs = append(errs, fmt.Errorf("installing %s: %w", id, err))
					continue
				}
				ledger.Plugins[id] = "installed"
			}
		}
		for _, id := range sortedKeys(ledger.Plugins) {
			if slices.Contains(k.Plugins, id) {
				continue
			}
			actions = append(actions, Action{"plugin", id, "remove", "no longer in the kit"})
			if dryRun {
				continue
			}
			if slices.Contains(installed, id) {
				if err := env.Claude("plugin", "uninstall", "--scope", "user", id); err != nil {
					errs = append(errs, fmt.Errorf("uninstalling %s: %w", id, err))
					continue
				}
			}
			delete(ledger.Plugins, id)
		}
	}

	// skills, agents and commands: files under the Claude dir
	files, err := k.files()
	if err != nil {
		return nil, err
	}
	wantFile := map[string]bool{}
	for _, f := range files {
		wantFile[f.dest] = true
		data, err := fs.ReadFile(k.fsys, f.src)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		h := hash(data)
		dest := filepath.Join(env.ClaudeDir, filepath.FromSlash(f.dest))
		cur, err := os.ReadFile(dest)
		owned, inLedger := ledger.Files[f.dest]
		switch {
		case err == nil && !inLedger:
			actions = append(actions, Action{f.kind, f.dest, "skip", "you have your own"})
			continue
		case err == nil && bytes.Equal(cur, data):
			ledger.Files[f.dest] = h
			actions = append(actions, Action{f.kind, f.dest, "ok", ""})
			continue
		case err == nil && hash(cur) != owned:
			actions = append(actions, Action{f.kind, f.dest, "skip", "you edited it; bud kit adopt keeps your version"})
			continue
		}
		do := "add"
		if err == nil {
			do = "update"
		}
		actions = append(actions, Action{f.kind, f.dest, do, ""})
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := atomicfile.WriteFile(dest, data, 0o644); err != nil {
			errs = append(errs, err)
			continue
		}
		ledger.Files[f.dest] = h
	}
	for _, p := range sortedKeys(ledger.Files) {
		if wantFile[p] {
			continue
		}
		actions = append(actions, Action{"file", p, "remove", "no longer in the kit"})
		if dryRun {
			continue
		}
		dest := filepath.Join(env.ClaudeDir, filepath.FromSlash(p))
		// only when it's still what bud wrote
		if cur, err := os.ReadFile(dest); err == nil && hash(cur) == ledger.Files[p] {
			os.Remove(dest)
			removeEmptyParents(filepath.Dir(dest), env.ClaudeDir)
		}
		delete(ledger.Files, p)
	}

	// instructions: bud's block in ~/.claude/CLAUDE.md
	if k.Instructions != "" || ledger.Instructions != "" {
		h := ""
		if k.Instructions != "" {
			h = hash([]byte(k.Instructions))
		}
		switch {
		case h == ledger.Instructions:
			actions = append(actions, Action{"instructions", "CLAUDE.md", "ok", ""})
		default:
			do := "update"
			switch {
			case h == "":
				do = "remove"
			case ledger.Instructions == "":
				do = "add"
			}
			actions = append(actions, Action{"instructions", "CLAUDE.md", do, ""})
			if !dryRun {
				p := filepath.Join(env.ClaudeDir, "CLAUDE.md")
				cur, _ := os.ReadFile(p)
				if err := os.MkdirAll(env.ClaudeDir, 0o755); err != nil {
					errs = append(errs, err)
				} else if err := atomicfile.WriteFile(p, []byte(withBlock(string(cur), k.Instructions)), 0o644); err != nil {
					errs = append(errs, err)
				} else {
					ledger.Instructions = h
				}
			}
		}
	}

	if !dryRun {
		ledger.Dir = env.ClaudeDir
		if err := ledger.save(env.Ledger); err != nil {
			errs = append(errs, err)
		}
	}

	return actions, errors.Join(errs...)
}

// Unsync takes out of env's Claude Code config everything the ledger says
// bud put there, leaving an empty ledger: a sync with nothing in the kit
func Unsync(env Env, dryRun bool) ([]Action, error) {
	env.Connectors = nil
	return Sync(&Kit{}, env, dryRun)
}

func hasBin(env Env, name string) bool {
	if env.HasBin != nil {
		return env.HasBin(name)
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// removeEmptyParents removes dir and its parents up to (not including) stop
// while they're empty, so a removed skill doesn't leave its folder behind
func removeEmptyParents(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// Entries lists the kit for bud kit ls: kind, name, description
func (k *Kit) Entries() [][3]string {
	var out [][3]string
	for _, m := range k.MCP {
		out = append(out, [3]string{"mcp", m.Name, m.Description})
	}
	for _, s := range k.Skills {
		out = append(out, [3]string{"skill", s, ""})
	}
	for _, p := range k.Plugins {
		out = append(out, [3]string{"plugin", p, ""})
	}
	for _, a := range k.Agents {
		out = append(out, [3]string{"agent", a, ""})
	}
	for _, c := range k.Commands {
		out = append(out, [3]string{"command", c, ""})
	}
	if k.Instructions != "" {
		out = append(out, [3]string{"instructions", "CLAUDE.md", ""})
	}

	return out
}
