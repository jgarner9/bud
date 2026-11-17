// Package hermes is the Hermes adapter (Suite D12, D46, D48): it renders the
// config.yaml bud sync writes for Hermes, the home server's manager, and the
// tiers file its tool gate reads.
//
// The config holds the model registry's providers and the manager role's
// model (Suite D46), the D48 changes to Hermes's defaults (manual approvals
// wired to the action tiers through a pre_tool_call hook, every skill and
// memory write staged for review, synced skills read-only, the terminal in
// Docker, update checks off), the MCP servers from the env fragments' bud
// block (Suite D72-D78) and the fragments' bud.hermes settings laid over the
// rest (the built-in toolsets per platform and the web backends, D77, D85).
//
// Hermes takes only models allowed P0 (Suite D23): until the router (phase
// 3) can tell a session's tier, any session may hold P0 data, such as the
// calendar, so a cloud model is only ever picked by hand with /model.
//
// Values a fragment reads from the environment, {env:VAR}, become Hermes's
// own ${VAR}; Hermes refuses to connect a remote server whose URL or headers
// name an unset one, so a missing variable fails closed. Provider keys that
// are op:// references are resolved by Hermes's 1Password source at start
// (Suite D7), so no key is ever in the file
package hermes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/adapter/env"
	"jgarner.dev/bud/internal/registry"
)

// Header opens the rendered file, so a hand edit is never a surprise loss
const Header = "# Written by bud sync from the model registry and env fragments. Edits here are\n# replaced on the next sync: change content/env or your overlay's env instead.\n"

// transportKeys are the fields of a Hermes server entry that come from the
// bud block, which bud.hermes.mcp_servers may not set
var transportKeys = []string{"url", "headers", "command", "args", "env", "transport"}

// owned are the settings bud renders for the model registry and the D48
// changes, which no fragment may set, so nothing turns a safety setting
// back off or routes Hermes around the registry
var owned = []string{
	"_config_version", "model", "providers", "fallback_providers", "fallback_model", "custom_providers",
	"approvals", "command_allowlist", "hooks", "hooks_auto_accept", "updates",
	"skills.write_approval", "skills.external_dirs", "memory.write_approval",
	"auxiliary.background_review", "curator.consolidate",
	"terminal.backend", "terminal.docker_mount_cwd_to_workspace",
	"plugins", "platforms.signal",
}

// ConfigVersion is the config schema version of the Hermes release bud
// pins (v0.23.2's _config_version). Without it Hermes reads the file as v0
// and asks to migrate it; raise it with the pin, after checking what the
// new version's migrations change
const ConfigVersion = 53

// Sandbox is the terminal backend, so the agent's commands can't read
// Hermes's own .env, the 1Password token or anything else on the host
const Sandbox = "docker"

// ApprovalTimeout is how long an approval waits before it is denied (Suite
// D42)
const ApprovalTimeout = 3600

// GateTimeout is how many seconds the tier gate may take before Hermes
// blocks the call
const GateTimeout = 10

// SignalURL is where signal-cli's daemon serves JSON-RPC and events for
// Hermes's Signal adapter (Suite D30): loopback only, on the home server's
// 879x ports (server/signal-cli/)
const SignalURL = "http://127.0.0.1:8793"

// Input is what the config is rendered from. Paths are absolute
type Input struct {
	Registry *registry.Registry
	// Env holds the env fragments' JSONC, in the order they apply
	Env []env.Fragment
	// Gate is the tier gate's command line, the program first: Hermes runs
	// it before every tool call
	Gate []string
	// Plugins are the Hermes plugins bud installed, which the config enables
	// (bud-signal, D30); no other plugin's code runs
	Plugins []string
}

// Output is the rendered config and the gate's tiers file
type Output struct {
	Config []byte
	Tiers  []byte
	// Notes say what was left out and why
	Notes []string
}

// Render builds config.yaml and the tiers file. The same input always gives
// the same bytes
func Render(in Input) (*Output, error) {
	if in.Registry == nil {
		return nil, fmt.Errorf("render Hermes's config: no registry")
	}
	if len(in.Gate) == 0 {
		return nil, fmt.Errorf("render Hermes's config: no tier gate command")
	}
	for _, f := range in.Env {
		frag, err := env.Merge([]env.Fragment{f})
		if err != nil {
			return nil, err
		}
		if h, ok := frag["bud"].(map[string]any); ok {
			if extra, ok := h["hermes"].(map[string]any); ok {
				for _, path := range owned {
					if touches(extra, path) {
						return nil, fmt.Errorf("env fragment %s sets bud.hermes.%s, which bud renders from the registry and the D48 safety settings; drop it", f.Name, path)
					}
				}
			}
		}
	}
	merged, err := env.Merge(in.Env)
	if err != nil {
		return nil, err
	}
	rules, err := permissionRules(merged)
	if err != nil {
		return nil, err
	}
	tools, err := env.Take(merged)
	if err != nil {
		return nil, err
	}
	out := &Output{Notes: tools.Notes()}

	cfg, notes, err := models(in.Registry)
	if err != nil {
		return nil, err
	}
	out.Notes = append(out.Notes, notes...)

	var shell []Rule
	denied := []any{}
	for _, r := range rules {
		if r.Action != "shell" {
			continue
		}
		shell = append(shell, r)
		if r.Effect == Deny {
			denied = append(denied, r.Resource)
		}
	}
	cfg["_config_version"] = ConfigVersion
	cfg["approvals"] = map[string]any{
		// nobody but Jason answers: no model approves on his behalf, and a
		// session with nobody to ask is refused
		"mode":              "manual",
		"timeout":           ApprovalTimeout,
		"cron_mode":         "deny",
		"single_query_mode": "deny",
		"unattended_mode":   "deny",
		"approvers":         []any{},
		// the never tier, refused by Hermes itself too, even under --yolo
		"deny": denied,
	}
	cfg["command_allowlist"] = []any{}
	cfg["hooks"] = map[string]any{
		"pre_tool_call": []any{map[string]any{
			"command":     shellJoin(in.Gate),
			"timeout":     GateTimeout,
			"fail_closed": true,
		}},
	}
	// the gateway can't answer a consent prompt, and bud owns the hooks key,
	// so the only hook to accept is the gate
	cfg["hooks_auto_accept"] = true
	// synced skills are copied into Hermes's own dir and locked there by bud
	// sync, so no external dir is set: a skill there couldn't be locked
	cfg["skills"] = map[string]any{"write_approval": true, "external_dirs": []any{}}
	cfg["memory"] = map[string]any{"write_approval": true}
	cfg["auxiliary"] = map[string]any{"background_review": map[string]any{"enabled": false}}
	cfg["curator"] = map[string]any{"consolidate": false}
	cfg["terminal"] = map[string]any{"backend": Sandbox, "docker_mount_cwd_to_workspace": false}
	cfg["updates"] = map[string]any{"check": false}
	plugins := []any{}
	for _, n := range in.Plugins {
		plugins = append(plugins, n)
	}
	cfg["plugins"] = map[string]any{"enabled": plugins}
	// Signal (D30): the bot number, Jason's number as the only allowed user
	// and the home channel are personal, so they come from Hermes's .env or
	// the overlay (SIGNAL_ACCOUNT, SIGNAL_ALLOWED_USERS, SIGNAL_HOME_CHANNEL).
	// Anyone else is ignored rather than offered pairing, and groups stay off
	// while SIGNAL_GROUP_ALLOWED_USERS is unset
	cfg["platforms"] = map[string]any{"signal": map[string]any{
		"enabled":                  true,
		"unauthorized_dm_behavior": "ignore",
		"extra":                    map[string]any{"http_url": SignalURL},
	}}

	if len(tools.Servers) > 0 {
		cfg["mcp_servers"] = mcpServers(tools)
	}
	// the fragments' own Hermes settings, which may add to a server's entry
	// (say, trust: untrusted) but not its transport, and not bring back one
	// that was left out
	if tools.Hermes != nil {
		extra, err := clone(tools.Hermes)
		if err != nil {
			return nil, err
		}
		if m, ok := extra["mcp_servers"].(map[string]any); ok {
			if err := tools.CheckExtras("bud.hermes.mcp_servers", m, transportKeys); err != nil {
				return nil, err
			}
		}
		env.MergeInto(cfg, extra)
	}
	if err := providerSecrets(in.Registry, cfg); err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(Header)
	if err := writeYAML(&b, cfg, 0); err != nil {
		return nil, err
	}
	out.Config = []byte(b.String())

	t := Tiers{Shell: shell, Tools: tools.Tiers}
	if t.Shell == nil {
		t.Shell = []Rule{}
	}
	if t.Tools == nil {
		t.Tools = map[string]string{}
	}
	tb, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}
	out.Tiers = append(tb, '\n')

	return out, nil
}

// models renders the providers, the manager's model and its fallbacks
func models(r *registry.Registry) (map[string]any, []string, error) {
	providers := map[string]any{}
	for _, pid := range sortedKeys(r.Providers) {
		p := r.Providers[pid]
		if p.API != "openai" {
			return nil, nil, fmt.Errorf("render Hermes's config: provider %s speaks %q, which the Hermes adapter can't render yet", pid, p.API)
		}
		ms := map[string]any{}
		for _, mid := range sortedKeys(r.Models) {
			m := r.Models[mid]
			if m.Provider != pid || m.Dims > 0 || m.Has("fim") {
				// embedding models are for memory and FIM models for
				// autocomplete, not chat
				continue
			}
			entry := map[string]any{}
			if m.Context > 0 {
				entry["context_length"] = m.Context
			}
			ms[m.Name] = entry
		}
		if len(ms) == 0 {
			continue
		}
		entry := map[string]any{
			"api":       strings.TrimSuffix(p.Endpoint, "/"),
			"transport": "chat_completions",
			"models":    ms,
		}
		if v := keyVar(pid, p.Key); v != "" {
			entry["key_env"] = v
		}
		providers[pid] = entry
	}

	chain, left, err := p0Chain(r)
	if err != nil {
		return nil, nil, err
	}
	ref := func(id string) map[string]any {
		m := r.Models[id]
		return map[string]any{"provider": "custom:" + m.Provider, "model": m.Name}
	}
	first := ref(chain[0])
	fallbacks := []any{}
	for _, id := range chain[1:] {
		fallbacks = append(fallbacks, ref(id))
	}
	cfg := map[string]any{
		"model":              map[string]any{"default": first["model"], "provider": first["provider"]},
		"providers":          providers,
		"fallback_providers": fallbacks,
	}
	var notes []string
	if len(left) > 0 {
		notes = append(notes, fmt.Sprintf("Hermes takes only the manager role's P0 models until the router knows each session's tier; left out: %s", strings.Join(left, ", ")))
	}

	return cfg, notes, nil
}

// p0Chain is the manager role's models allowed P0, then the fallback role's,
// in order, and the manager's members it left out
func p0Chain(r *registry.Registry) (chain, left []string, err error) {
	role, ok := r.Roles["manager"]
	if !ok {
		return nil, nil, fmt.Errorf("render Hermes's config: the registry has no manager role")
	}
	members := role.Members()
	if role.Pin != "" {
		members = append([]string{role.Pin}, members...)
	}
	add := func(id string, note bool) {
		if slices.Contains(chain, id) || slices.Contains(left, id) {
			return
		}
		m, ok := r.Models[id]
		if ok && m.Dims == 0 && slices.Contains(m.Privacy, "P0") {
			chain = append(chain, id)
		} else if note {
			left = append(left, id)
		}
	}
	for _, id := range members {
		add(id, true)
	}
	if fb, ok := r.Roles["fallback"]; ok {
		for _, id := range fb.Members() {
			add(id, false)
		}
	}
	if len(chain) == 0 {
		return nil, nil, fmt.Errorf("render Hermes's config: neither the manager nor the fallback role has a model allowed P0; add a local model to the manager's pool (Suite D23)")
	}

	return chain, left, nil
}

// keyVar is the environment variable a provider's key comes from: the one
// an {env:VAR} names, or BUD_<PROVIDER>_API_KEY for an op:// reference, as
// the OpenCode adapter names it
func keyVar(provider, key string) string {
	switch {
	case key == "":
		return ""
	case strings.HasPrefix(key, "{env:"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "{env:"), "}")
	}
	up := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return r
		}
		return '_'
	}, provider)

	return "BUD_" + up + "_API_KEY"
}

// providerSecrets adds each op:// provider key to Hermes's 1Password source,
// which reads it with op read at start; a fragment's own entries stay
func providerSecrets(r *registry.Registry, cfg map[string]any) error {
	refs := map[string]any{}
	for _, pid := range sortedKeys(r.Providers) {
		p := r.Providers[pid]
		if strings.HasPrefix(p.Key, "op://") {
			refs[keyVar(pid, p.Key)] = p.Key
		}
	}
	if len(refs) == 0 {
		return nil
	}
	secrets, _ := cfg["secrets"].(map[string]any)
	if secrets == nil {
		secrets = map[string]any{}
		cfg["secrets"] = secrets
	}
	op, _ := secrets["onepassword"].(map[string]any)
	if op == nil {
		op = map[string]any{}
		secrets["onepassword"] = op
	}
	envs, _ := op["env"].(map[string]any)
	if envs == nil {
		envs = map[string]any{}
		op["env"] = envs
	}
	for k, v := range refs {
		if have, ok := envs[k]; ok && have != v {
			return fmt.Errorf("bud.hermes.secrets.onepassword.env.%s is %v, but the registry's provider key is %v; drop the fragment's entry", k, have, v)
		}
		envs[k] = v
	}
	op["enabled"] = true

	return nil
}

// permissionRules are the merged fragments' permission rules, checked
func permissionRules(merged map[string]any) ([]Rule, error) {
	raw, ok := merged["permissions"]
	if !ok {
		return []Rule{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var rules []Rule
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, fmt.Errorf("env fragments: permissions: %w", err)
	}
	for i, r := range rules {
		if r.Action == "" || r.Resource == "" || !slices.Contains([]string{Allow, Ask, Deny}, r.Effect) {
			return nil, fmt.Errorf("env fragments: permissions[%d]: a rule needs an action, a resource and an effect of allow, ask or deny", i)
		}
	}

	return rules, nil
}

// mcpServers is Hermes's mcp_servers for the resolved servers. Gateway
// servers keep the device's privacy header; Hermes can select a cloud model
// by hand, so a P0-only server must remain unavailable until its session tier
// can be enforced.
func mcpServers(tools *env.Resolved) map[string]any {
	servers := map[string]any{}
	for _, name := range tools.Names() {
		s := tools.Servers[name]
		var entry map[string]any
		if s.Remote() {
			entry = map[string]any{"url": hermesRefs(s.URL)}
			headers := s.Headers
			if len(headers) > 0 {
				entry["headers"] = refMap(headers)
			}
		} else {
			entry = map[string]any{"command": hermesRefs(s.Command[0])}
			var args []any
			for _, a := range s.Command[1:] {
				args = append(args, hermesRefs(a))
			}
			if len(args) > 0 {
				entry["args"] = args
			}
			if len(s.Env) > 0 {
				entry["env"] = refMap(s.Env)
			}
		}
		servers[name] = entry
	}

	return servers
}

// touches says whether m sets a dotted path, or replaces a section on the
// way to it with something that isn't a map, which would replace bud's
// section whole when merged
func touches(m map[string]any, path string) bool {
	head, rest, nested := strings.Cut(path, ".")
	v, ok := m[head]
	if !ok {
		return false
	}
	if !nested {
		return true
	}
	sub, ok := v.(map[string]any)

	return !ok || touches(sub, rest)
}

// hermesRefs turns each {env:VAR} into Hermes's ${VAR}
func hermesRefs(s string) string { return env.EnvRef.ReplaceAllString(s, "$${$1}") }

func refMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = hermesRefs(v)
	}

	return out
}

func clone(m map[string]any) (map[string]any, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(b, &out)

	return out, err
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+-]+$`)

// shellJoin quotes a command line the way Hermes splits it (shlex, no shell)
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if shellSafe.MatchString(a) {
			parts[i] = a
		} else {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'"'"'`) + "'"
		}
	}

	return strings.Join(parts, " ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
