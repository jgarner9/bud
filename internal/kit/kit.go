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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Kit is the kit as the repo describes it
type Kit struct {
	MCP          []MCP
	Skills       []string
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
	Profiles        []string          `json:"profiles,omitempty"` // empty means every profile
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
		Agents   []string `json:"agents"`
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}
	k := &Kit{Skills: index.Skills, Agents: index.Agents, Commands: index.Commands, fsys: fsys}
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
			name, ok := strings.CutPrefix(v, secretPrefix)
			if !ok {
				out[k] = v
				continue
			}
			if !set(name) {
				if !m.OptionalSecrets {
					missing = append(missing, name)
				}
				continue
			}
			out[k] = "${" + name + "}"
		}
		return out
	}
	s := map[string]any{"type": m.Transport}
	if m.URL != "" {
		s["url"] = m.URL
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
	Files        map[string]string `json:"files"` // path under the Claude dir -> hash
	Instructions string            `json:"instructions,omitempty"`
}

// ReadLedger reads path; a missing ledger is an empty one, and false
func ReadLedger(path string) (*Ledger, bool, error) {
	l := &Ledger{MCP: map[string]string{}, Files: map[string]string{}}
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

// Env is where sync writes and how it runs claude
type Env struct {
	ClaudeDir  string // ~/.claude
	ClaudeJSON string // ~/.claude.json, where user-scope MCP servers live
	Ledger     string
	Profile    string
	Claude     func(args ...string) error // runs claude
	SecretSet  func(name string) bool
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
	ledger, _, err := ReadLedger(env.Ledger)
	if err != nil {
		return nil, err
	}
	var actions []Action
	var errs []error
	have := userServers(env.ClaudeJSON)

	// MCP servers
	wanted := map[string]bool{}
	for _, m := range k.MCP {
		if !m.For(env.Profile) {
			continue
		}
		wanted[m.Name] = true
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
		actions = append(actions, Action{"mcp", name, "remove", "no longer in the kit"})
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
			if h == "" {
				do = "remove"
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
		if err := ledger.save(env.Ledger); err != nil {
			errs = append(errs, err)
		}
	}

	return actions, errors.Join(errs...)
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
