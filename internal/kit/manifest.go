package kit

// The kit manifest: kit/kit.json as Go types, and the checks Load makes on it.
// The format is described in docs/internals/kit-manifest.md; change that file
// and this one together. Loading only reads and checks, it never applies
// anything to a machine and never reads a rule's or script's text.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Kit is the kit as kit.json describes it
type Kit struct {
	// the plugin marketplaces sync adds before installing plugins: name ->
	// source (a git URL, GitHub owner/repo or path, as claude plugin
	// marketplace add takes it)
	Marketplaces map[string]string `json:"marketplaces,omitempty"`
	Plugins      []string          `json:"plugins,omitempty"` // Claude Code plugins, name@marketplace
	Servers      map[string]Server `json:"servers,omitempty"`
	Settings     map[string]any    `json:"settings,omitempty"`
	Rules        map[string]Rule   `json:"rules,omitempty"`
	Account      Account           `json:"account"`
	Cloud        Cloud             `json:"cloud"`
}

// Server is one MCP server, keyed by the name Claude Code knows it by
type Server struct {
	Description string         `json:"description"`
	MCP         MCP            `json:"mcp"`
	Modes       []string       `json:"modes"` // the modes it is on in by default: full, merge; none is opt-in
	Vars        map[string]Var `json:"vars,omitempty"`
	Requires    string         `json:"requires,omitempty"`  // a command that must be on PATH
	Connector   string         `json:"connector,omitempty"` // a key of Account.Connectors
	Desktop     bool           `json:"desktop,omitempty"`
	Settings    map[string]any `json:"settings,omitempty"`
}

// MCP is how to reach a server, in the format claude mcp add-json takes
type MCP struct {
	Type    string            `json:"type"` // stdio, http or sse
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Var is a value a server's mcp placeholders need
type Var struct {
	Prompt string `json:"prompt"`
	Secret bool   `json:"secret"`
}

// Rule is a rule file, keyed by the name it gets in ~/.claude/rules/bud/. Its
// text is File (a path in kit/) or Marketplace (marketplace/path)
type Rule struct {
	File        string `json:"file,omitempty"`
	Marketplace string `json:"marketplace,omitempty"`
	Server      string `json:"server,omitempty"` // installed only while this server is on
}

// Account is what the claude.ai account should hold
type Account struct {
	Marketplaces []string             `json:"marketplaces,omitempty"` // owner/repo
	Plugins      AccountPlugins       `json:"plugins"`
	Connectors   map[string]Connector `json:"connectors,omitempty"`
	Instructions string               `json:"instructions,omitempty"` // marketplace/path
}

// AccountPlugins are the plugin names the account should and shouldn't have on
type AccountPlugins struct {
	Wanted   []string `json:"wanted,omitempty"`
	Unwanted []string `json:"unwanted,omitempty"`
}

// Connector is a claude.ai connector, by the name claude.ai shows
type Connector struct {
	On bool `json:"on"`
}

// Cloud is what a cloud environment should carry
type Cloud struct {
	Script string `json:"script,omitempty"` // a path in kit/
}

var (
	identPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	rulePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.md$`)
	varPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	placeholderEnd = regexp.MustCompile(`^[A-Z][A-Z0-9_]*(:\?)?\}`)
)

// the entries as they are written, so presence can be told from a zero value
type (
	rawServer struct {
		Description string            `json:"description"`
		MCP         *MCP              `json:"mcp"`
		Modes       []string          `json:"modes"`
		Vars        map[string]rawVar `json:"vars"`
		Requires    string            `json:"requires"`
		Connector   string            `json:"connector"`
		Desktop     bool              `json:"desktop"`
		Settings    json.RawMessage   `json:"settings"`
	}
	rawVar struct {
		Prompt string `json:"prompt"`
		Secret *bool  `json:"secret"`
	}
	rawConnector struct {
		On *bool `json:"on"`
	}
	rawAccount struct {
		Marketplaces []string `json:"marketplaces"`
		Plugins      struct {
			Wanted   []string `json:"wanted"`
			Unwanted []string `json:"unwanted"`
		} `json:"plugins"`
		Connectors   map[string]json.RawMessage `json:"connectors"`
		Instructions string                     `json:"instructions"`
	}
)

// Load reads kit.json from fsys, laid out like the repo's kit/, and returns the
// kit or the first problem with it
func Load(fsys fs.FS) (*Kit, error) {
	data, err := fs.ReadFile(fsys, "kit.json")
	if err != nil {
		return nil, err
	}
	var top struct {
		Marketplaces map[string]string          `json:"marketplaces"`
		Plugins      []string                   `json:"plugins"`
		Servers      map[string]json.RawMessage `json:"servers"`
		Settings     json.RawMessage            `json:"settings"`
		Rules        map[string]json.RawMessage `json:"rules"`
		Account      json.RawMessage            `json:"account"`
		Cloud        json.RawMessage            `json:"cloud"`
	}
	if err := decodeStrict(data, &top); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}
	k := &Kit{Marketplaces: top.Marketplaces, Plugins: top.Plugins}

	for _, name := range sortedNames(top.Servers) {
		s, err := loadServer(name, top.Servers[name])
		if err != nil {
			return nil, fmt.Errorf("kit.json: server %q: %w", name, err)
		}
		if k.Servers == nil {
			k.Servers = map[string]Server{}
		}
		k.Servers[name] = s
	}

	if len(top.Settings) > 0 {
		if k.Settings, err = objectOf(top.Settings); err != nil {
			return nil, fmt.Errorf("kit.json: settings: %w", err)
		}
	}

	if err := loadAccount(k, top.Account); err != nil {
		return nil, err
	}

	for _, name := range sortedNames(top.Rules) {
		r, err := loadRule(k, fsys, name, top.Rules[name])
		if err != nil {
			return nil, fmt.Errorf("kit.json: rule %q: %w", name, err)
		}
		if k.Rules == nil {
			k.Rules = map[string]Rule{}
		}
		k.Rules[name] = r
	}

	if len(top.Cloud) > 0 {
		if err := decodeStrict(top.Cloud, &k.Cloud); err != nil {
			return nil, fmt.Errorf("kit.json: cloud: %w", err)
		}
		if k.Cloud.Script != "" {
			if err := checkFile(fsys, k.Cloud.Script); err != nil {
				return nil, fmt.Errorf("kit.json: cloud: script: %w", err)
			}
		}
	}

	return k, nil
}

// ExpectedServers is the sorted names of the servers that are on by default in
// full mode and need no command on PATH, which doctor's claude setup check
// looks for
func (k *Kit) ExpectedServers() []string {
	var names []string
	for name, s := range k.Servers {
		if slices.Contains(s.Modes, "full") && s.Requires == "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	return names
}

// loadServer decodes and checks one server (steps 2.1 to 2.10)
func loadServer(name string, data json.RawMessage) (Server, error) {
	if !identPattern.MatchString(name) {
		return Server{}, fmt.Errorf("the name must match %s", identPattern)
	}
	var raw rawServer
	if err := decodeStrict(data, &raw); err != nil {
		return Server{}, err
	}
	switch {
	case raw.Description == "":
		return Server{}, fmt.Errorf("description is empty")
	case raw.MCP == nil:
		return Server{}, fmt.Errorf("mcp is missing")
	case raw.Modes == nil:
		return Server{}, fmt.Errorf("modes is missing (use [] for opt-in)")
	}
	m := *raw.MCP
	switch m.Type {
	case "stdio":
		if m.Command == "" {
			return Server{}, fmt.Errorf("mcp: a stdio server needs a command")
		}
		if m.URL != "" || len(m.Headers) > 0 {
			return Server{}, fmt.Errorf("mcp: a stdio server has no url or headers")
		}
	case "http", "sse":
		if m.URL == "" {
			return Server{}, fmt.Errorf("mcp: an %s server needs a url", m.Type)
		}
		if m.Command != "" || len(m.Args) > 0 || len(m.Env) > 0 {
			return Server{}, fmt.Errorf("mcp: an %s server has no command, args or env", m.Type)
		}
	default:
		return Server{}, fmt.Errorf("mcp: type %q is not stdio, http or sse", m.Type)
	}
	for i, mode := range raw.Modes {
		if mode != "full" && mode != "merge" {
			return Server{}, fmt.Errorf("mode %q is not full or merge", mode)
		}
		if slices.Contains(raw.Modes[:i], mode) {
			return Server{}, fmt.Errorf("mode %q is listed twice", mode)
		}
	}
	if strings.ContainsFunc(raw.Requires, func(r rune) bool { return r == '/' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		return Server{}, fmt.Errorf("requires %q must be a command name: no whitespace and no /", raw.Requires)
	}
	if m.Type == "stdio" && (raw.Connector != "" || raw.Desktop) {
		return Server{}, fmt.Errorf("connector and desktop are for http and sse servers, not stdio")
	}
	vars := map[string]Var{}
	for _, v := range sortedNames(raw.Vars) {
		rv := raw.Vars[v]
		switch {
		case !varPattern.MatchString(v):
			return Server{}, fmt.Errorf("var %q: the name must match %s", v, varPattern)
		case rv.Prompt == "":
			return Server{}, fmt.Errorf("var %q: prompt is empty", v)
		case rv.Secret == nil:
			return Server{}, fmt.Errorf("var %q: secret is missing", v)
		}
		vars[v] = Var{Prompt: rv.Prompt, Secret: *rv.Secret}
	}
	if err := checkPlaceholders(m, vars); err != nil {
		return Server{}, err
	}
	s := Server{Description: raw.Description, MCP: m, Modes: raw.Modes, Requires: raw.Requires, Connector: raw.Connector, Desktop: raw.Desktop}
	if len(vars) > 0 {
		s.Vars = vars
	}
	if len(raw.Settings) > 0 {
		var err error
		if s.Settings, err = objectOf(raw.Settings); err != nil {
			return Server{}, fmt.Errorf("settings: %w", err)
		}
	}

	return s, nil
}

// checkPlaceholders checks that every ${ in m is a well-formed placeholder
// naming a var, that every var is used, and that env and headers names have none
func checkPlaceholders(m MCP, vars map[string]Var) error {
	used := map[string]bool{}
	scan := func(where, s string) error {
		for rest := s; ; {
			_, after, found := strings.Cut(rest, "${")
			if !found {
				return nil
			}
			end := placeholderEnd.FindString(after)
			if end == "" {
				return fmt.Errorf("%s: %q has a ${ that isn't ${NAME} or ${NAME:?}", where, s)
			}
			name := strings.TrimSuffix(strings.TrimSuffix(end, "}"), ":?")
			if _, ok := vars[name]; !ok {
				return fmt.Errorf("%s: placeholder %s is not in vars", where, name)
			}
			used[name] = true
			rest = after[len(end):]
		}
	}
	type field struct{ where, s string }
	fields := []field{{"mcp command", m.Command}}
	for _, a := range m.Args {
		fields = append(fields, field{"mcp args", a})
	}
	for _, maps := range []struct {
		where string
		m     map[string]string
	}{{"mcp env", m.Env}, {"mcp headers", m.Headers}} {
		for _, n := range sortedNames(maps.m) {
			if strings.Contains(n, "${") {
				return fmt.Errorf("%s: the name %q holds a placeholder", maps.where, n)
			}
			fields = append(fields, field{maps.where, maps.m[n]})
		}
	}
	fields = append(fields, field{"mcp url", m.URL})
	for _, f := range fields {
		if err := scan(f.where, f.s); err != nil {
			return err
		}
	}
	for _, v := range sortedNames(vars) {
		if !used[v] {
			return fmt.Errorf("var %q is not used by any placeholder", v)
		}
	}

	return nil
}

// loadAccount fills k.Account and checks it and the connectors (steps 4 to 6)
func loadAccount(k *Kit, data json.RawMessage) error {
	var raw rawAccount
	if len(data) > 0 {
		if err := decodeStrict(data, &raw); err != nil {
			return fmt.Errorf("kit.json: account: %w", err)
		}
	}

	for _, name := range sortedNames(raw.Connectors) {
		if name == "" || strings.TrimSpace(name) != name {
			return fmt.Errorf("kit.json: connector %q: the name is empty or has a leading or trailing space", name)
		}
		var c rawConnector
		if err := decodeStrict(raw.Connectors[name], &c); err != nil {
			return fmt.Errorf("kit.json: connector %q: %w", name, err)
		}
		if c.On == nil {
			return fmt.Errorf("kit.json: connector %q: on is missing", name)
		}
		if k.Account.Connectors == nil {
			k.Account.Connectors = map[string]Connector{}
		}
		k.Account.Connectors[name] = Connector{On: *c.On}
	}

	claimed := map[string]string{}
	for _, name := range sortedNames(k.Servers) {
		c := k.Servers[name].Connector
		if c == "" {
			continue
		}
		if _, ok := k.Account.Connectors[c]; !ok {
			return fmt.Errorf("kit.json: server %q: connector %q is not in account.connectors", name, c)
		}
		if other, dup := claimed[c]; dup {
			return fmt.Errorf("kit.json: server %q: connector %q is already the connector of server %q", name, c, other)
		}
		claimed[c] = name
	}

	a := &k.Account
	a.Marketplaces = raw.Marketplaces
	a.Plugins = AccountPlugins{Wanted: raw.Plugins.Wanted, Unwanted: raw.Plugins.Unwanted}
	a.Instructions = raw.Instructions
	for _, n := range a.Plugins.Wanted {
		if !identPattern.MatchString(n) {
			return fmt.Errorf("kit.json: account: wanted plugin %q must be a plugin name (%s), without @", n, identPattern)
		}
	}
	for _, n := range a.Plugins.Unwanted {
		if !identPattern.MatchString(n) {
			return fmt.Errorf("kit.json: account: unwanted plugin %q must be a plugin name (%s), without @", n, identPattern)
		}
		if slices.Contains(a.Plugins.Wanted, n) {
			return fmt.Errorf("kit.json: account: plugin %q is both wanted and unwanted", n)
		}
	}
	for _, n := range a.Plugins.Wanted {
		if !slices.ContainsFunc(k.Plugins, func(id string) bool { return pluginName(id) == n }) {
			return fmt.Errorf("kit.json: account: wanted plugin %q is not in plugins", n)
		}
	}
	for i, m := range a.Marketplaces {
		if m == "" {
			return fmt.Errorf("kit.json: account: marketplaces has an empty entry")
		}
		if slices.Contains(a.Marketplaces[:i], m) {
			return fmt.Errorf("kit.json: account: marketplace %q is listed twice", m)
		}
	}
	if a.Instructions != "" {
		if err := checkMarketplacePath(k, a.Instructions); err != nil {
			return fmt.Errorf("kit.json: account: instructions: %w", err)
		}
	}

	return nil
}

// loadRule decodes and checks one rule (step 7)
func loadRule(k *Kit, fsys fs.FS, name string, data json.RawMessage) (Rule, error) {
	if !rulePattern.MatchString(name) {
		return Rule{}, fmt.Errorf("the name must match %s", rulePattern)
	}
	var r Rule
	if err := decodeStrict(data, &r); err != nil {
		return Rule{}, err
	}
	switch {
	case (r.File == "") == (r.Marketplace == ""):
		return Rule{}, fmt.Errorf("exactly one of file and marketplace is needed")
	case r.File != "":
		if err := checkFile(fsys, r.File); err != nil {
			return Rule{}, fmt.Errorf("file: %w", err)
		}
	default:
		if err := checkMarketplacePath(k, r.Marketplace); err != nil {
			return Rule{}, fmt.Errorf("marketplace: %w", err)
		}
	}
	if _, ok := k.Servers[r.Server]; r.Server != "" && !ok {
		return Rule{}, fmt.Errorf("server %q is not in servers", r.Server)
	}

	return r, nil
}

// checkMarketplacePath checks s is marketplace/path, the marketplace one of k's
// and the path valid
func checkMarketplacePath(k *Kit, s string) error {
	mp, path, ok := strings.Cut(s, "/")
	if !ok || path == "" {
		return fmt.Errorf("%q must be marketplace/path", s)
	}
	if _, ok := k.Marketplaces[mp]; !ok {
		return fmt.Errorf("%q: %q is not in marketplaces", s, mp)
	}
	if !fs.ValidPath(path) {
		return fmt.Errorf("%q: %q is not a valid path", s, path)
	}

	return nil
}

// checkFile checks path is a valid path to a file in fsys that isn't empty
func checkFile(fsys fs.FS, path string) error {
	if !fs.ValidPath(path) {
		return fmt.Errorf("%q is not a valid path", path)
	}
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("%s is empty", path)
	}

	return nil
}

// objectOf decodes data, which must be a JSON object
func objectOf(data json.RawMessage) (map[string]any, error) {
	var obj map[string]any
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return nil, fmt.Errorf("must be an object")
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}

	return obj, nil
}

// decodeStrict is json.Unmarshal that refuses a key v has no field for
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	return dec.Decode(v)
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)

	return names
}
