// Package kit syncs bud's Claude Code setup (BASE.md 4.1): plugin
// marketplaces, plugins, skills, subagents, slash commands and output styles,
// from the kit/ embedded in the binary into Claude Code's user config.
//
// bud only touches what it wrote: a ledger records each item with a hash of
// what was written, so the next sync updates what changed, removes what the
// kit dropped, and leaves anything you set up by hand alone. MCP servers and
// the CLAUDE.md block are no longer in the kit (D77): Claude Code's own
// config has them, and sync only takes out the ones an older bud put there.
package kit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Kit is the kit as the repo describes it
type Kit struct {
	Skills  []string
	Plugins []string // Claude Code plugins, name@marketplace
	// the plugin marketplaces sync adds before installing plugins: name ->
	// source (a git URL, GitHub owner/repo or path, as claude plugin
	// marketplace add takes it)
	Marketplaces map[string]string
	Agents       []string
	Commands     []string
	OutputStyles []string
	OutputStyle  string // the one sync turns on in settings.json

	fsys fs.FS
}

// Load reads the kit from fsys, laid out like the repo's kit/
func Load(fsys fs.FS) (*Kit, error) {
	data, err := fs.ReadFile(fsys, "kit.json")
	if err != nil {
		return nil, err
	}
	var index struct {
		Skills       []string          `json:"skills"`
		Plugins      []string          `json:"plugins"`
		Marketplaces map[string]string `json:"marketplaces"`
		Agents       []string          `json:"agents"`
		Commands     []string          `json:"commands"`
		OutputStyles []string          `json:"output-styles"`
		OutputStyle  string            `json:"output_style"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}
	k := &Kit{Skills: index.Skills, Plugins: index.Plugins, Marketplaces: index.Marketplaces, Agents: index.Agents, Commands: index.Commands, OutputStyles: index.OutputStyles, OutputStyle: index.OutputStyle, fsys: fsys}
	return k, nil
}

// Ledger is what bud wrote into Claude Code's config, with a hash of each
type Ledger struct {
	// MCP servers and the CLAUDE.md block an older bud synced (before D77),
	// kept so sync can take them out
	MCP          map[string]string `json:"mcp,omitempty"`
	Plugins      map[string]string `json:"plugins,omitempty"`
	Marketplaces map[string]string `json:"marketplaces,omitempty"` // name -> the source bud added it from
	Files        map[string]string `json:"files"`                  // path under the Claude dir -> hash
	Instructions string            `json:"instructions,omitempty"`
	// the outputStyle bud set in settings.json; "" when it didn't set one
	OutputStyle string `json:"output_style,omitempty"`
	// the Claude Code config dir all this is in; "" from before bud kept its
	// own (JGA-135) means ~/.claude
	Dir string `json:"dir,omitempty"`
}

// ReadLedger reads path; a missing ledger is an empty one, and false
func ReadLedger(path string) (*Ledger, bool, error) {
	l := &Ledger{MCP: map[string]string{}, Plugins: map[string]string{}, Marketplaces: map[string]string{}, Files: map[string]string{}}
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
	if l.Marketplaces == nil {
		l.Marketplaces = map[string]string{}
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

// Own records that bud manages something you adopted into the kit: the files
// of a skill, agent or command as they are now
func Own(ledgerPath, claudeDir, kind, name string) error {
	l, _, err := ReadLedger(ledgerPath)
	if err != nil {
		return err
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
	Claude     func(args ...string) error // runs claude
	// lists the installed plugins (claude plugin list --json); nil means none
	InstalledPlugins func() ([]string, error)
	// lists the plugin marketplaces Claude Code has, by name (claude plugin
	// marketplace list --json); nil means none
	Marketplaces func() ([]string, error)
	Disabled     []string // entries turned off on this machine (bud kit disable)
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
	out.Skills, out.Agents, out.Commands, out.Plugins = keep(k.Skills), keep(k.Agents), keep(k.Commands), keep(k.Plugins)
	out.OutputStyles = keep(k.OutputStyles)
	if len(k.Marketplaces) > 0 {
		out.Marketplaces = map[string]string{}
		for name, src := range k.Marketplaces {
			if !slices.Contains(names, name) {
				out.Marketplaces[name] = src
			}
		}
	}
	if slices.Contains(names, k.OutputStyle) {
		out.OutputStyle = ""
	}

	return &out
}

// Action is one thing sync does, or would do with --dry-run
type Action struct {
	Kind string // "marketplace", "plugin", "skill", "agent", "command", "output-style"; "mcp" and "instructions" for what an older bud left
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
	}{{"agent", "agents", k.Agents}, {"command", "commands", k.Commands}, {"output-style", "output-styles", k.OutputStyles}} {
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
	// marketplaces, then the plugins from them, then the marketplaces the kit
	// dropped, once nothing of bud's is installed from them
	registered, refreshed, err := syncMarketplaces(k, env, ledger, dryRun, &actions)
	if err != nil {
		errs = append(errs, err)
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
				if dryRun || !refreshed[marketplaceOf(id)] {
					continue
				}
				// its marketplace was just refreshed: bring it up to date too
				// (a failure only means it stays as it was until next time)
				_ = env.Claude("plugin", "update", "--scope", "user", id)
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
	removeMarketplaces(k, env, ledger, registered, dryRun, &actions, &errs)

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

	// what an older bud synced that Claude Code's own config has now (D77)
	removeLegacy(env, ledger, dryRun, &actions, &errs)

	// outputStyle in settings.json, after the style's file is in place
	styleFile := "output-styles/" + k.OutputStyle + ".md"
	haveStyle := ledger.Files[styleFile] != ""
	if dryRun {
		haveStyle = slices.ContainsFunc(actions, func(a Action) bool {
			return a.Name == styleFile && (a.Do == "add" || a.Do == "update" || a.Do == "ok")
		})
	}
	if a, err := syncOutputStyle(k.OutputStyle, haveStyle, env, ledger, dryRun); err != nil {
		errs = append(errs, err)
	} else if a != nil {
		actions = append(actions, *a)
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
	return Sync(&Kit{}, env, dryRun)
}

// removeLegacy takes out the MCP servers and the CLAUDE.md block an older bud
// synced (D77 moved both to Claude Code's own config): a server only while
// it's still there, the block only bud's fenced part, and anything of yours
// stays
func removeLegacy(env Env, ledger *Ledger, dryRun bool, actions *[]Action, errs *[]error) {
	have := userServers(env.ClaudeJSON)
	for _, name := range sortedKeys(ledger.MCP) {
		*actions = append(*actions, Action{"mcp", name, "remove", "Claude Code's own config has the MCP servers now"})
		if dryRun {
			continue
		}
		if _, present := have[name]; present {
			if err := env.Claude("mcp", "remove", "--scope", "user", name); err != nil {
				*errs = append(*errs, fmt.Errorf("removing %s: %w", name, err))
				continue
			}
		}
		delete(ledger.MCP, name)
	}
	if ledger.Instructions == "" {
		return
	}
	*actions = append(*actions, Action{"instructions", "CLAUDE.md", "remove", "the plugins carry the instructions now"})
	if dryRun {
		return
	}
	p := filepath.Join(env.ClaudeDir, "CLAUDE.md")
	if cur, err := os.ReadFile(p); err == nil {
		if err := atomicfile.WriteFile(p, []byte(withBlock(string(cur), "")), 0o644); err != nil {
			*errs = append(*errs, err)
			return
		}
	}
	ledger.Instructions = ""
}

// syncMarketplaces adds the kit's marketplaces Claude Code doesn't have yet
// and refreshes the ones bud added, so the plugins from them can follow. One
// you added yourself, from whatever source, is left alone (D77). It returns
// the marketplaces Claude Code had and the ones it refreshed
func syncMarketplaces(k *Kit, env Env, ledger *Ledger, dryRun bool, actions *[]Action) (registered []string, refreshed map[string]bool, err error) {
	refreshed = map[string]bool{}
	if len(k.Marketplaces) == 0 && len(ledger.Marketplaces) == 0 {
		return nil, refreshed, nil
	}
	if env.Marketplaces != nil {
		if registered, err = env.Marketplaces(); err != nil {
			return nil, refreshed, fmt.Errorf("listing plugin marketplaces: %w", err)
		}
	}
	var errs []error
	for _, name := range sortedKeys(k.Marketplaces) {
		src := k.Marketplaces[name]
		_, owned := ledger.Marketplaces[name]
		switch {
		case slices.Contains(registered, name) && !owned:
			*actions = append(*actions, Action{"marketplace", name, "skip", "you added it yourself"})
		case slices.Contains(registered, name):
			if dryRun {
				*actions = append(*actions, Action{"marketplace", name, "ok", ""})
				continue
			}
			// refreshed every sync: a git marketplace is a cached copy that
			// Claude Code doesn't update by itself. Offline, it stays as it was
			if err := env.Claude("plugin", "marketplace", "update", name); err != nil {
				*actions = append(*actions, Action{"marketplace", name, "skip", "couldn't refresh it: " + err.Error()})
				continue
			}
			*actions = append(*actions, Action{"marketplace", name, "ok", ""})
			refreshed[name] = true
		default:
			*actions = append(*actions, Action{"marketplace", name, "add", ""})
			if dryRun {
				continue
			}
			if err := env.Claude("plugin", "marketplace", "add", src); err != nil {
				errs = append(errs, fmt.Errorf("adding marketplace %s: %w", name, err))
				continue
			}
			ledger.Marketplaces[name] = src
			registered = append(registered, name)
		}
	}

	return registered, refreshed, errors.Join(errs...)
}

// removeMarketplaces takes out the marketplaces bud added that the kit no
// longer has. It runs after the plugins, so bud's plugins from one are
// uninstalled first
func removeMarketplaces(k *Kit, env Env, ledger *Ledger, registered []string, dryRun bool, actions *[]Action, errs *[]error) {
	for _, name := range sortedKeys(ledger.Marketplaces) {
		if _, keep := k.Marketplaces[name]; keep {
			continue
		}
		*actions = append(*actions, Action{"marketplace", name, "remove", "no longer in the kit"})
		if dryRun {
			continue
		}
		if slices.Contains(registered, name) {
			if err := env.Claude("plugin", "marketplace", "remove", name); err != nil {
				*errs = append(*errs, fmt.Errorf("removing marketplace %s: %w", name, err))
				continue
			}
		}
		delete(ledger.Marketplaces, name)
	}
}

// ParseMarketplaces reads the names out of claude plugin marketplace list
// --json
func ParseMarketplaces(out []byte) ([]string, error) {
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("claude plugin marketplace list: %w", err)
	}
	names := make([]string, len(list))
	for i, m := range list {
		names[i] = m.Name
	}

	return names, nil
}

// marketplaceOf is the marketplace in a plugin id, name@marketplace
func marketplaceOf(id string) string {
	_, m, _ := strings.Cut(id, "@")
	return m
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
	for _, s := range k.Skills {
		out = append(out, [3]string{"skill", s, ""})
	}
	for _, name := range sortedKeys(k.Marketplaces) {
		out = append(out, [3]string{"marketplace", name, k.Marketplaces[name]})
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
	for _, s := range k.OutputStyles {
		out = append(out, [3]string{"output-style", s, ""})
	}

	return out
}
