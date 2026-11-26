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
// Hermes runs as two profiles (Suite D23, D95). The default profile is
// private. With a router (the home server), it lists one provider, the
// router, and its chats start at P1 (Suite D117, superseding D95's P0-only
// models): the router raises a chat to P0 and keeps it local when a rule
// matches or a calendar or mail call is in it (D118, D119), which is what
// lets the profile read P0 data such as the calendar. Without a router,
// its config lists only the providers and models allowed P0, so neither
// its own fallbacks nor a model Jason picks with /model can send a chat to
// the cloud. It owns Signal and the reminders. The cloud profile
// (~/.hermes/profiles/cloud, `hermes -p cloud`) lists every chat model and
// none of the servers only P0 may use, keeps its own memory and sessions,
// and stays out of the private profile's gateway process.
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
	"maps"
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
	"gateway.standalone",
	"plugins", "platforms.signal",
	// where a delegated subagent's requests go (Suite D135): the router's
	// always-P0 endpoint, which an override could point around
	"delegation",
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
	// Cloud renders the cloud profile instead of the private one
	Cloud bool
}

// Output is the rendered config and the gate's tiers file
type Output struct {
	Config []byte
	Tiers  []byte
	// GatewayApprovals is the MCP gateway's approvals file (env.Resolved.GatewayApprovals), nil when no gateway server is declared
	GatewayApprovals []byte
	// RouterP0Servers is the router's list of the servers only P0 sessions
	// may use (env.Resolved.RouterP0Servers, Suite D119)
	RouterP0Servers []byte
	// Notes say what was left out and why
	Notes []string
	// CloudKeys are the variables that hold the keys of the providers the
	// private config leaves out: every provider when it goes through the
	// router, else those with no model allowed P0. Hermes's own built-in
	// providers read some of them (OPENCODE_GO_API_KEY among them), so one
	// set in Hermes's environment would let /model reach a cloud model
	// around the router's privacy rules; bud sync refuses a ~/.hermes/.env
	// that sets one (Suite D95, still needed under D117)
	CloudKeys []string
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

	cfg, notes, err := models(in.Registry, in.Cloud)
	if err != nil {
		return nil, err
	}
	if tools.Router != "" && !in.Cloud {
		// the router serves a P0 request only from a local model, and moves
		// it to a P1 one only once Jason approves (Suite D104), so the
		// private profile keeps its boundary while gaining the fallback
		tier := ChatTier(tools.Privacy)
		cfg = routed(in.Registry, tools.Router, tier)
		notes = []string{fmt.Sprintf("Hermes's private profile asks the router at %s for %s at %s; the router keeps a chat local when its rules or a calendar or mail call say so (Suite D117)", tools.Router, RouterModel, tier)}
	}
	out.Notes = append(out.Notes, notes...)

	// the gate tiers the sandboxed terminal by bud.sandbox_shell when a
	// fragment sets it (Suite D114), else by the permission shell rules.
	// Hermes itself refuses every shell deny in either set
	var shell []Rule
	denied := []any{}
	for _, r := range rules {
		if r.Action != "shell" {
			continue
		}
		if len(tools.SandboxShell) == 0 {
			shell = append(shell, r)
		}
		if r.Effect == Deny {
			denied = append(denied, r.Resource)
		}
	}
	for _, r := range tools.SandboxShell {
		shell = append(shell, Rule{Action: "shell", Resource: r.Resource, Effect: r.Effect})
		if r.Effect == Deny && !slices.Contains(denied, any(r.Resource)) {
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
	if in.Cloud {
		// never served by the private profile's gateway process, which
		// would otherwise take in every profile (Hermes multiplexes them)
		cfg["gateway"] = map[string]any{"standalone": true}
	}
	plugins := []any{}
	for _, n := range in.Plugins {
		plugins = append(plugins, n)
	}
	cfg["plugins"] = map[string]any{"enabled": plugins}
	if !in.Cloud {
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
	}

	if len(tools.Servers) > 0 {
		servers, left := mcpServers(tools, in.Cloud)
		cfg["mcp_servers"] = servers
		for _, name := range left {
			out.Notes = append(out.Notes, fmt.Sprintf("Hermes's cloud profile leaves out %s, which only private sessions may use (Suite D95)", name))
		}
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
			if in.Cloud {
				// a server the cloud profile left out keeps none of its extras
				for _, name := range tools.Names() {
					if privateOnly(tools.Servers[name]) {
						delete(m, name)
					}
				}
			}
		}
		env.MergeInto(cfg, extra)
	}
	if err := providerSecrets(in.Registry, cfg); err != nil {
		return nil, err
	}
	if !in.Cloud {
		out.CloudKeys = cloudKeys(in.Registry, cfg)
		if err := noCloudSecrets(cfg, out.CloudKeys); err != nil {
			return nil, err
		}
	}

	var b strings.Builder
	b.WriteString(Header)
	if err := writeYAML(&b, cfg, 0); err != nil {
		return nil, err
	}
	out.Config = []byte(b.String())

	t := Tiers{Shell: shell, Tools: tools.Tiers, When: tools.TiersWhen, Private: privateTools(tools), Trusted: trustedGateway(tools, cfg)}
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
	if out.GatewayApprovals, err = tools.GatewayApprovals(); err != nil {
		return nil, err
	}
	if out.RouterP0Servers, err = tools.RouterP0Servers(); err != nil {
		return nil, err
	}

	return out, nil
}

// models renders the providers and models, the manager's model and its
// fallbacks. The private profile gets only the chat models allowed P0, and
// a provider with none is left out whole, so its key isn't loaded either;
// the cloud profile gets every chat model
func models(r *registry.Registry, all bool) (map[string]any, []string, error) {
	providers := map[string]any{}
	var cloud []string
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
			if !all && !slices.Contains(m.Privacy, "P0") {
				// a cloud model listed here could be picked with /model
				// in a chat that holds P0 data (Suite D95)
				cloud = append(cloud, mid)
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

	var chain, left []string
	var err error
	if all {
		chain, err = anyChain(r)
	} else {
		chain, left, err = p0Chain(r)
	}
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
	// the manager's members not allowed P0 are among the cloud models
	// named below; only the rest (a missing model, say) get a note here
	left = slices.DeleteFunc(left, func(id string) bool { return slices.Contains(cloud, id) })
	if len(left) > 0 {
		notes = append(notes, fmt.Sprintf("Hermes is private, so these members of the manager role aren't its model or fallbacks: %s", strings.Join(left, ", ")))
	}
	if len(cloud) > 0 {
		notes = append(notes, fmt.Sprintf("Hermes is private, so its config lists no model not allowed P0, and /model can't reach one: %s", strings.Join(cloud, ", ")))
	}

	return cfg, notes, nil
}

// RouterProvider and RouterModel are what the private profile names when it
// goes through the router: one provider, the router, and the manager role
const (
	RouterProvider = "bud"
	RouterModel    = "bud/manager"
)

// ChatTier is the tier the private profile's chats start at (Suite D117,
// superseding D95's P0-only models): P1, so a plain chat gets a fast cloud
// model, and the router raises it to P0 on evidence (the #private marker,
// the secret and PII rules, a P0-only tool in the conversation). A device
// whose fragments set P0 keeps every chat P0
func ChatTier(device string) string {
	if device == "P0" || device == "" {
		return "P0"
	}
	return "P1"
}

// routed renders the private profile's model settings for the router at
// base: the manager role as its one model, with no fallbacks of Hermes's
// own, since the router does the falling back. A P1 profile sends
// X-Bud-Privacy: P1 on every request to the router, auxiliary calls such as
// compression and titles included; a P0 one sends no header, which the
// router takes as P0. Hermes sends its conversation id as
// x-opencode-session, which the router passes on to OpenCode Go and uses to
// keep a raised conversation P0. The context is the smallest of the manager
// members the router may pick for it (P0 or P1), so Hermes never packs more
// than the local model can take once a chat is raised
func routed(r *registry.Registry, base, tier string) map[string]any {
	entry := map[string]any{}
	if c := managerContext(r); c > 0 {
		entry["context_length"] = c
	}
	provider := map[string]any{
		"api":       base + "/v1",
		"transport": "chat_completions",
		"models":    map[string]any{RouterModel: entry},
		// Hermes sends its conversation id under this name, which the
		// router passes on: OpenCode Go refuses a request without one
		"session_affinity_header": "x-opencode-session",
	}
	if tier != "P0" {
		provider["extra_headers"] = map[string]any{env.PrivacyHeader: tier}
	}
	return map[string]any{
		"model":              map[string]any{"default": RouterModel, "provider": "custom:" + RouterProvider},
		"providers":          map[string]any{RouterProvider: provider},
		"fallback_providers": []any{},
		// a delegated subagent carries its own conversation id and no
		// parent's, so the router can't hold it at the parent's tier. It
		// goes to the router's always-P0 endpoint instead (Suite D135).
		// The router asks for no key; Hermes wants one for a direct endpoint
		"delegation": map[string]any{
			"base_url": base + "/p0/v1",
			"api_key":  "bud-router",
			"api_mode": "chat_completions",
			"model":    RouterModel,
		},
	}
}

// managerContext is the smallest context of the manager role's chat models
// allowed P0 or P1, 0 when none states one
func managerContext(r *registry.Registry) int {
	role := r.Roles["manager"]
	ids := role.Members()
	if role.Pin != "" {
		ids = append([]string{role.Pin}, ids...)
	}
	least := 0
	for _, id := range ids {
		m, ok := r.Models[id]
		if !ok || m.Dims > 0 || m.Has("fim") || m.Context == 0 || !(m.Allows("P0") || m.Allows("P1")) {
			continue
		}
		if least == 0 || m.Context < least {
			least = m.Context
		}
	}
	return least
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

// anyChain is the manager role's chat models, then the fallback role's, in
// order, whatever their tier: the cloud profile's model and fallbacks
func anyChain(r *registry.Registry) ([]string, error) {
	role, ok := r.Roles["manager"]
	if !ok {
		return nil, fmt.Errorf("render Hermes's config: the registry has no manager role")
	}
	ids := role.Members()
	if role.Pin != "" {
		ids = append([]string{role.Pin}, ids...)
	}
	if fb, ok := r.Roles["fallback"]; ok {
		ids = append(ids, fb.Members()...)
	}
	var chain []string
	for _, id := range ids {
		if m, ok := r.Models[id]; ok && m.Dims == 0 && !m.Has("fim") && !slices.Contains(chain, id) {
			chain = append(chain, id)
		}
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("render Hermes's config: neither the manager nor the fallback role has a chat model")
	}

	return chain, nil
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

// providerSecrets adds the op:// key of each provider the config lists to
// Hermes's 1Password source, which reads it with op read at start; a
// fragment's own entries stay. A provider left out (one with no model
// allowed P0) gets no key, so Hermes holds no cloud model's key at all
func providerSecrets(r *registry.Registry, cfg map[string]any) error {
	listed, _ := cfg["providers"].(map[string]any)
	refs := map[string]any{}
	for _, pid := range sortedKeys(r.Providers) {
		p := r.Providers[pid]
		if _, ok := listed[pid]; ok && strings.HasPrefix(p.Key, "op://") {
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

// BuiltinKeys are the variables Hermes v0.23.2's built-in model providers
// read their credentials from (its plugins/model-providers and
// hermes_cli/auth.py). Any of them in the private profile's environment
// would let /model reach that provider, whatever the config lists, so they
// are cloud keys unless a provider the private config lists uses one.
// Check them again with the pin
var BuiltinKeys = []string{
	"ACTUAL_API_KEY", "AI_GATEWAY_API_KEY", "ALIBABA_CODING_PLAN_API_KEY", "ALIBABA_CODING_PLAN_CN_API_KEY",
	"ALIBABA_TOKEN_PLAN_API_KEY", "ALIBABA_TOKEN_PLAN_CN_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_TOKEN",
	"ARCEEAI_API_KEY", "AWS_ACCESS_KEY_ID", "AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN", "AZURE_FOUNDRY_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "COMMANDCODE_API_KEY",
	"DASHSCOPE_API_KEY", "DEEPINFRA_API_KEY", "DEEPSEEK_API_KEY", "FIREWORKS_API_KEY", "GEMINI_API_KEY",
	"GLM_API_KEY", "GMI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS", "HF_TOKEN",
	"KILOCODE_API_KEY", "KIMI_API_KEY", "KIMI_CN_API_KEY", "KIMI_CODING_API_KEY", "LM_API_KEY", "META_API_KEY",
	"META_MODEL_API_KEY", "MINIMAX_API_KEY", "MINIMAX_CN_API_KEY", "MODEL_API_KEY", "NEBIUS_API_KEY",
	"NEBIUS_TOKEN_FACTORY_API_KEY", "NOUS_API_KEY", "NOVITA_API_KEY", "NVIDIA_API_KEY", "OLLAMA_API_KEY",
	"OPENAI_API_KEY", "OPENCODE_GO_API_KEY", "OPENCODE_ZEN_API_KEY", "OPENROUTER_API_KEY", "QWEN_API_KEY",
	"RAMP_ROUTER_API_KEY", "ROUTER_API_KEY", "STEPFUN_API_KEY", "TOKENHUB_API_KEY", "TOKENPLAN_API_KEY",
	"UPSTAGE_API_KEY", "XAI_API_KEY", "XIAOMI_API_KEY", "ZAI_API_KEY", "Z_AI_API_KEY",
}

// cloudKeys are the variables that must not reach the private profile, sorted:
// the key variables of the registry's providers the config leaves out, and
// Hermes's built-in provider keys, less those of the providers it lists
func cloudKeys(r *registry.Registry, cfg map[string]any) []string {
	listed, _ := cfg["providers"].(map[string]any)
	var keep []string
	keys := slices.Clone(BuiltinKeys)
	for _, pid := range sortedKeys(r.Providers) {
		v := keyVar(pid, r.Providers[pid].Key)
		if v == "" {
			continue
		}
		if _, ok := listed[pid]; ok {
			keep = append(keep, v)
		} else if !slices.Contains(keys, v) {
			keys = append(keys, v)
		}
	}
	keys = slices.DeleteFunc(keys, func(k string) bool { return slices.Contains(keep, k) })
	slices.Sort(keys)

	return keys
}

// noCloudSecrets refuses a fragment that loads a left-out provider's key
// into Hermes through its 1Password source
func noCloudSecrets(cfg map[string]any, keys []string) error {
	secrets, _ := cfg["secrets"].(map[string]any)
	op, _ := secrets["onepassword"].(map[string]any)
	envs, _ := op["env"].(map[string]any)
	for _, k := range keys {
		if _, ok := envs[k]; ok {
			return fmt.Errorf("bud.hermes.secrets.onepassword.env.%s loads the key of a provider with no model allowed P0, which /model could then reach; Hermes is private (Suite D95), so drop the entry", k)
		}
	}

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

// privateOnly says whether a server is one only private sessions may use:
// one P0 may use and no cloud tier may, such as the calendar (Suite D82,
// D92). It goes by the server's tiers alone, so a device whose own tier is
// P0 (or unset) doesn't hand the server to the cloud profile
func privateOnly(s env.Server) bool {
	return s.P0() && !slices.Contains(s.Privacy, "P1") && !slices.Contains(s.Privacy, "P2")
}

// sendsP0 says whether the private profile sends P0 instead of the
// device's tier to a server: a gateway server the device's tier may not use
// but P0 may, or one that marks the P0 records it returns (JGA-389), such
// as Bud memory. Memory reached at the device's tier would save a record
// from a chat that read the calendar at P1, where a later P1 chat could
// recall it on a cloud model; at P0 it saves at P0 by default, and a P0
// record it returns makes the router keep that chat local. The cloud
// profile reaches a marking server at the device's tier, since its chats
// are never meant to hold P0 data
func sendsP0(s env.Server, cloud bool) bool {
	tier := s.Headers[env.PrivacyHeader]
	if !s.Gateway || tier == "" || tier == "P0" || !s.P0() {
		return false
	}
	return !slices.Contains(s.Privacy, tier) || (s.MarksP0 && !cloud)
}

// cloudTier says whether the cloud profile reaches a server at a tier
// other than P0: the device's tier is P1 or P2 and the server allows it. A
// server that marks P0 records is left out of the cloud profile otherwise,
// since with no header, or with P0 sent for want of a tier it allows, the
// gateway would hand it P0 records
func cloudTier(s env.Server) bool {
	tier := s.Headers[env.PrivacyHeader]
	return (tier == "P1" || tier == "P2") && slices.Contains(s.Privacy, tier)
}

// TrustFull is Hermes's trust for a server it calls without asking first;
// "untrusted" asks before any tool not marked read-only, and an unset trust
// is full
const TrustFull = "full"

// trustedGateway are the gateway servers in the rendered config that Hermes
// trusts, sorted: those whose trust, after the fragments' extras, isn't
// "untrusted"
func trustedGateway(tools *env.Resolved, cfg map[string]any) []string {
	servers, _ := cfg["mcp_servers"].(map[string]any)
	out := []string{} // always written, so bud doctor can tell a file from before D115
	for _, name := range tools.Names() {
		entry, ok := servers[name].(map[string]any)
		if !ok || !tools.Servers[name].Gateway {
			continue
		}
		if trust, _ := entry["trust"].(string); trust == "" || trust == TrustFull {
			out = append(out, name)
		}
	}

	return out
}

// privateTools are the tool patterns of the servers only private sessions
// may use, which the gate refuses outside the private profile
func privateTools(tools *env.Resolved) []string {
	out := []string{}
	for _, name := range tools.Names() {
		if privateOnly(tools.Servers[name]) {
			out = append(out, "mcp__"+name+"__*")
		}
	}

	return out
}

// mcpServers is Hermes's mcp_servers for the resolved servers, and the
// names of those it left out. In the private profile a server only private
// sessions may use gets P0 in X-Bud-Privacy instead of the device's tier
// (Suite D95): what it reads there stays local, since the router raises any
// chat holding a call to it to P0 (Suite D119), or, without a router, the
// profile's config lists only models allowed P0 (see models). The cloud profile leaves
// such a server out. Every other server keeps the device's tier
func mcpServers(tools *env.Resolved, cloud bool) (map[string]any, []string) {
	servers := map[string]any{}
	var left []string
	for _, name := range tools.Names() {
		s := tools.Servers[name]
		if cloud && (privateOnly(s) || (s.MarksP0 && !cloudTier(s))) {
			left = append(left, name)
			continue
		}
		var entry map[string]any
		if s.Remote() {
			entry = map[string]any{"url": hermesRefs(s.URL)}
			if s.Gateway && s.Approval != nil {
				// the gateway asks for this server's writes, bound to the
				// exact arguments (D103), so Hermes's own untrusted-server
				// prompt would only ask a second time (Suite D115)
				entry["trust"] = TrustFull
			}
			headers := s.Headers
			if sendsP0(s, cloud) {
				headers = maps.Clone(headers)
				headers[env.PrivacyHeader] = "P0"
			}
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

	return servers, left
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
