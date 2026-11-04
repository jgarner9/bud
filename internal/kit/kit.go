// Package kit syncs bud's Claude Code setup (BASE.md 4.1, D77) from the
// kit.json embedded in the binary into Claude Code's user config: the plugin
// marketplaces bud's plugins come from, and the plugins. What Claude knows
// and how it works lives in those plugins (jgarner9/bud-plugins), not in bud.
//
// bud only touches what it added: a ledger records each item, so the next
// sync refreshes what's there, removes what the kit dropped, and leaves
// anything you set up yourself alone. Everything goes through `claude
// plugin`, never by editing its files. An older bud also synced MCP servers,
// skills, agents, commands, output styles and a CLAUDE.md block; sync takes
// out the ones still in the ledger, and nothing of yours.
package kit

import (
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

// Kit is the kit as kit.json describes it
type Kit struct {
	// the plugin marketplaces sync adds before installing plugins: name ->
	// source (a git URL, GitHub owner/repo or path, as claude plugin
	// marketplace add takes it)
	Marketplaces map[string]string
	Plugins      []string // Claude Code plugins, name@marketplace
	// the always-on instructions you paste into claude.ai, as
	// "marketplace/path" in that marketplace (JGA-179, ReadCore)
	Core string
	// MCP servers bud's sessions rely on that Claude Code's own config
	// provides (clod's), which doctor checks for (JGA-175)
	ExpectedMCP []string
}

// Load reads kit.json from fsys, laid out like the repo's kit/
func Load(fsys fs.FS) (*Kit, error) {
	data, err := fs.ReadFile(fsys, "kit.json")
	if err != nil {
		return nil, err
	}
	var index struct {
		Marketplaces map[string]string `json:"marketplaces"`
		Plugins      []string          `json:"plugins"`
		Core         string            `json:"core"`
		ExpectedMCP  []string          `json:"expected_mcp"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}

	return &Kit{Marketplaces: index.Marketplaces, Plugins: index.Plugins, Core: index.Core, ExpectedMCP: index.ExpectedMCP}, nil
}

// Ledger is what bud put into Claude Code's config
type Ledger struct {
	Plugins      map[string]string `json:"plugins,omitempty"`
	Marketplaces map[string]string `json:"marketplaces,omitempty"` // name -> the source bud added it from
	// the Claude Code config dir all this is in; "" from before bud kept its
	// own (JGA-135) means ~/.claude
	Dir string `json:"dir,omitempty"`

	// what an older bud synced before D77 and D78, kept so sync can take it
	// out: MCP servers, files under the Claude dir (path -> hash of what bud
	// wrote), the hash of its CLAUDE.md block, and the outputStyle it set in
	// settings.json
	MCP          map[string]string `json:"mcp,omitempty"`
	Files        map[string]string `json:"files,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	OutputStyle  string            `json:"output_style,omitempty"`
}

// ReadLedger reads path; a missing ledger is an empty one, and false
func ReadLedger(path string) (*Ledger, bool, error) {
	l := &Ledger{}
	ok := true
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		ok = false
	case err != nil:
		return nil, false, err
	default:
		if err := json.Unmarshal(data, l); err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
	}
	for _, m := range []*map[string]string{&l.Plugins, &l.Marketplaces, &l.MCP, &l.Files} {
		if *m == nil {
			*m = map[string]string{}
		}
	}

	return l, ok, nil
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
	out := *k
	out.Plugins = slices.DeleteFunc(slices.Clone(k.Plugins), func(n string) bool { return slices.Contains(names, n) })
	if len(k.Marketplaces) > 0 {
		out.Marketplaces = map[string]string{}
		for name, src := range k.Marketplaces {
			if !slices.Contains(names, name) {
				out.Marketplaces[name] = src
			}
		}
	}

	return &out
}

// Action is one thing sync does, or would do with --dry-run
type Action struct {
	Kind string // "marketplace", "plugin"; "mcp", "file", "instructions" and "output-style" for what an older bud left
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
	registered, err := syncMarketplaces(k, env, ledger, dryRun, &actions)
	if err != nil {
		errs = append(errs, err)
	}
	err = syncPlugins(k, env, ledger, dryRun, &actions)
	if err != nil {
		errs = append(errs, err)
	}
	removeMarketplaces(k, env, ledger, registered, dryRun, &actions, &errs)

	// what an older bud synced that the plugins and Claude Code's own config
	// have now (D77)
	removeLegacy(env, ledger, dryRun, &actions, &errs)

	if a, err := removeOutputStyle(env, ledger, dryRun); err != nil {
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

// syncPlugins installs the kit's plugins (user scope) that aren't installed
// from any source, updates bud's own at every sync (JGA-174: a marketplace you
// own, like clod's local checkout, isn't refreshed by bud, but the copy
// Claude Code installed from it still goes stale), and uninstalls bud's that
// the kit dropped. A plugin you installed yourself stays yours, and so does
// one claude.ai syncs (it loads as name@synced, JGA-177): then bud takes its
// own copy out, so the synced one loads and updates with claude.ai
func syncPlugins(k *Kit, env Env, ledger *Ledger, dryRun bool, actions *[]Action) error {
	if len(k.Plugins) == 0 && len(ledger.Plugins) == 0 {
		return nil
	}
	var installed []string
	var err error
	var errs []error
	if env.InstalledPlugins != nil {
		if installed, err = env.InstalledPlugins(); err != nil {
			errs = append(errs, fmt.Errorf("listing plugins: %w", err))
		}
	}
	for _, id := range k.Plugins {
		_, owned := ledger.Plugins[id]
		synced := pluginName(id) + "@synced"
		switch {
		case slices.Contains(installed, synced) && owned:
			*actions = append(*actions, Action{"plugin", id, "remove", "claude.ai syncs it"})
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
		case slices.Contains(installed, synced):
			*actions = append(*actions, Action{"plugin", id, "skip", "claude.ai syncs it"})
		case slices.Contains(installed, id) && owned:
			*actions = append(*actions, Action{"plugin", id, "ok", ""})
			if dryRun {
				continue
			}
			// a failure (offline, say) only means it stays as it was until
			// next time
			_ = env.Claude("plugin", "update", "--scope", "user", id)
		case slices.Contains(installed, id):
			*actions = append(*actions, Action{"plugin", id, "skip", "you installed it yourself"})
		default:
			*actions = append(*actions, Action{"plugin", id, "add", ""})
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
		*actions = append(*actions, Action{"plugin", id, "remove", "no longer in the kit"})
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

	return errors.Join(errs...)
}

// syncMarketplaces adds the kit's marketplaces Claude Code doesn't have yet
// and refreshes the ones bud added, so the plugins from them can follow. One
// you added yourself, from whatever source, is left alone (D77). It returns
// the marketplaces Claude Code had
func syncMarketplaces(k *Kit, env Env, ledger *Ledger, dryRun bool, actions *[]Action) (registered []string, err error) {
	if len(k.Marketplaces) == 0 && len(ledger.Marketplaces) == 0 {
		return nil, nil
	}
	if env.Marketplaces != nil {
		if registered, err = env.Marketplaces(); err != nil {
			return nil, fmt.Errorf("listing plugin marketplaces: %w", err)
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

	return registered, errors.Join(errs...)
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

// removeLegacy takes out what an older bud synced that the plugins and
// Claude Code's own config carry now (D77): MCP servers, while they're still
// there; skill, agent, command and output style files, only while they're
// what bud wrote; and bud's fenced CLAUDE.md block. Anything of yours stays
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

	for _, p := range sortedKeys(ledger.Files) {
		*actions = append(*actions, Action{"file", p, "remove", "bud's plugins have it now"})
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

	if ledger.Instructions == "" {
		return
	}
	*actions = append(*actions, Action{"instructions", "CLAUDE.md", "remove", "the plugins carry the instructions now"})
	if dryRun {
		return
	}
	p := filepath.Join(env.ClaudeDir, "CLAUDE.md")
	if cur, err := os.ReadFile(p); err == nil {
		if err := atomicfile.WriteFile(p, []byte(withoutBlock(string(cur))), 0o644); err != nil {
			*errs = append(*errs, err)
			return
		}
	}
	ledger.Instructions = ""
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

// the fenced block an older bud kept in CLAUDE.md
const (
	blockStart = "<!-- bud:begin (written by bud kit sync; edits in here are replaced) -->"
	blockEnd   = "<!-- bud:end -->"
)

// withoutBlock returns doc with bud's block taken out, and what's around it
// kept
func withoutBlock(doc string) string {
	before, rest, found := strings.Cut(doc, blockStart)
	if !found {
		return doc
	}
	after := ""
	if _, a, ok := strings.Cut(rest, blockEnd); ok {
		after = strings.TrimLeft(a, "\n")
	}
	out := strings.TrimRight(before, "\n")
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

// pluginName is the name in a plugin id, name@marketplace
func pluginName(id string) string {
	name, _, _ := strings.Cut(id, "@")
	return name
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
	for _, name := range sortedKeys(k.Marketplaces) {
		out = append(out, [3]string{"marketplace", name, k.Marketplaces[name]})
	}
	for _, p := range k.Plugins {
		out = append(out, [3]string{"plugin", p, ""})
	}

	return out
}
