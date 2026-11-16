// Package opencode is the OpenCode adapter (Suite D12, D15, D17, D46): it
// renders the one opencode.jsonc bud sync writes, from the model registry,
// the content a device takes and its env fragments. Runtimes sit behind a
// thin adapter like this one, so swapping a runtime means a new adapter, not
// new clients or content.
//
// What it writes follows OpenCode's V2 config: providers as OpenAI-compatible
// packages with their base URL, the coding role as the default model and the
// planning role on the plan agent, skills, instructions and plugins pointed
// at synced content, and update checks off, so bud update is the only thing
// that moves OpenCode's pinned version. Env fragments are merged on top, in
// order, so the overlay's apply last. Keys never appear in the file: each
// provider names an environment variable, filled at launch by OpenCode
// itself ({env:VAR}) or by op run from an op:// reference (Suite D7).
//
// The fragments' bud block (internal/adapter/env) becomes OpenCode's MCP
// servers, and the P0 tool rule (Suite D81) becomes a private agent: a
// primary agent on a model allowed P0 whose permission rules deny every tool
// that leaves the house
package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/adapter/env"
	"jgarner.dev/bud/internal/registry"
)

// Schema is OpenCode's config schema, for editors that validate the file
const Schema = "https://opencode.ai/config.json"

// compatiblePackage is OpenCode's runtime for OpenAI-compatible APIs
const compatiblePackage = "@opencode/ai/providers/openai-compatible"

// Header opens the rendered file, so a hand edit is never a surprise loss
const Header = "// Written by bud sync from the model registry and env fragments. Edits here are\n// replaced on the next sync: change content/env or your overlay's env instead.\n"

// Input is what a device's config is rendered from. Paths are absolute
type Input struct {
	Registry     *registry.Registry
	Skills       []string // dirs OpenCode searches for skills
	Instructions []string // instruction files, public AGENTS.md before the overlay's
	Plugins      []string // plugin dirs
	// Reachable are dirs outside any project the agent may always reach,
	// where the synced content lives: rules allowing them come after every
	// fragment's, so a fragment that asks before leaving the project still
	// lets skills and instructions load. Canonical paths, since OpenCode
	// matches external_directory against those
	Reachable []string
	// Env holds the env fragments' JSONC, in the order they apply
	Env []Fragment
}

// Fragment is one env fragment
type Fragment = env.Fragment

// PrivateAgent is the agent P0 sessions run as (Suite D81)
const PrivateAgent = "private"

// cloudTools are the built-in tools that send text out of the house, which
// the private agent is denied along with every MCP server not allowed P0.
// Subagents are denied too, since a subagent runs under its own rules, not
// its parent's
var cloudTools = []string{"webfetch", "websearch", "experimental_image_generate", "subagent"}

// Output is the rendered config and the secrets op run must fill in
type Output struct {
	Config []byte
	// Secrets maps an environment variable to the op:// reference it is read
	// from; bud writes these to an env file for op run --env-file
	Secrets map[string]string
	// Notes say what was left out of the config and why, such as an MCP
	// server whose URL only the overlay sets
	Notes []string
}

// Render builds opencode.jsonc. The same input always gives the same bytes
func Render(in Input) (*Output, error) {
	r := in.Registry
	if r == nil {
		return nil, fmt.Errorf("render opencode.jsonc: no registry")
	}
	out := &Output{Secrets: map[string]string{}}

	providers := map[string]any{}
	for _, pid := range sortedKeys(r.Providers) {
		p := r.Providers[pid]
		models := map[string]any{}
		for _, mid := range sortedKeys(r.Models) {
			m := r.Models[mid]
			if m.Provider != pid || m.Dims > 0 {
				// embedding models are for memory, not chat
				continue
			}
			entry := map[string]any{"modelID": m.Name}
			limit := map[string]any{}
			if m.Context > 0 {
				limit["context"] = m.Context
			}
			if m.Output > 0 {
				limit["output"] = m.Output
			}
			if len(limit) > 0 {
				entry["limit"] = limit
			}
			models[mid] = entry
		}
		if len(models) == 0 {
			continue
		}
		entry := map[string]any{
			"name":     pid,
			"package":  compatiblePackage,
			"settings": map[string]any{"baseURL": strings.TrimSuffix(p.Endpoint, "/")},
			"models":   models,
		}
		if v := keyVar(pid, p.Key); v != "" {
			entry["env"] = []any{v}
			if strings.HasPrefix(p.Key, "op://") {
				out.Secrets[v] = p.Key
			}
		}
		providers[pid] = entry
	}

	ref := func(role string) (string, error) {
		ro, ok := r.Roles[role]
		if !ok {
			return "", fmt.Errorf("render opencode.jsonc: the registry has no %s role", role)
		}
		id := ro.Default()
		m, ok := r.Models[id]
		if !ok {
			return "", fmt.Errorf("render opencode.jsonc: the %s role's model %q isn't in the registry", role, id)
		}
		return m.Provider + "/" + id, nil
	}
	coding, err := ref("coding")
	if err != nil {
		return nil, err
	}
	planning, err := ref("planning")
	if err != nil {
		return nil, err
	}

	cfg := map[string]any{
		"$schema":   Schema,
		"update":    "disable",
		"providers": providers,
		"model":     coding,
		"agents":    map[string]any{"plan": map[string]any{"model": planning}},
	}
	if len(in.Skills) > 0 {
		cfg["skills"] = strs(in.Skills)
	}
	if len(in.Instructions) > 0 {
		cfg["instructions"] = strs(in.Instructions)
	}
	if len(in.Plugins) > 0 {
		var ps []any
		for _, p := range in.Plugins {
			ps = append(ps, map[string]any{"package": p})
		}
		cfg["plugins"] = ps
	}

	frags := map[string]any{}
	for _, f := range in.Env {
		frag, err := env.Merge([]Fragment{f})
		if err != nil {
			return nil, err
		}
		if _, ok := frag["update"]; ok {
			return nil, fmt.Errorf("env fragment %s sets update, which bud owns so only bud update moves OpenCode's pin; drop it", f.Name)
		}
		env.MergeInto(frags, frag)
	}
	tools, err := env.Take(frags)
	if err != nil {
		return nil, err
	}
	out.Notes = tools.Notes()
	if servers := mcpServers(tools); len(servers) > 0 {
		cfg["mcp"] = map[string]any{"servers": servers}
	}
	private, note := privateAgent(r, tools)
	if private != nil {
		cfg["agents"].(map[string]any)[PrivateAgent] = private
	} else {
		out.Notes = append(out.Notes, note)
	}
	env.MergeInto(cfg, frags)
	// a fragment's extras for a server that was left out would be an
	// entry without a type, which OpenCode refuses
	if m, ok := cfg["mcp"].(map[string]any); ok {
		if servers, ok := m["servers"].(map[string]any); ok {
			for name := range tools.Left {
				delete(servers, name)
			}
		}
	}
	if len(in.Reachable) > 0 {
		rules, _ := cfg["permissions"].([]any)
		for _, dir := range in.Reachable {
			rules = append(rules, map[string]any{"action": "external_directory", "resource": filepath.Join(dir, "*"), "effect": "allow"})
		}
		cfg["permissions"] = rules
	}

	var buf bytes.Buffer
	buf.WriteString(Header)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	out.Config = buf.Bytes()

	return out, nil
}

// keyVar is the environment variable a provider's key comes from: the one
// an {env:VAR} names, or BUD_<PROVIDER>_API_KEY for an op:// reference
func keyVar(provider, key string) string {
	switch {
	case key == "":
		return ""
	case strings.HasPrefix(key, "{env:"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "{env:"), "}")
	default:
		up := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' {
				return r - 'a' + 'A'
			}
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '_'
		}, provider)
		return "BUD_" + up + "_API_KEY"
	}
}

// mcpServers is OpenCode's mcp.servers for the resolved servers: remote
// ones with OAuth off, since every key bud uses is a header or the
// gateway's, and local ones as a command
func mcpServers(tools *env.Resolved) map[string]any {
	servers := map[string]any{}
	for _, name := range tools.Names() {
		s := tools.Servers[name]
		var entry map[string]any
		if s.Remote() {
			entry = map[string]any{"type": "remote", "url": s.URL, "oauth": false}
			if len(s.Headers) > 0 {
				entry["headers"] = strMap(s.Headers)
			}
		} else {
			entry = map[string]any{"type": "local", "command": strs(s.Command)}
			if len(s.Env) > 0 {
				entry["environment"] = strMap(s.Env)
			}
		}
		servers[name] = entry
	}

	return servers
}

// privateAgent is the agent P0 sessions use (Suite D81): the first model of
// the coding role's pool that may see P0, then the fallback role's, with
// rules that deny every MCP server not allowed P0 and the built-in tools
// that leave the house. Agent rules apply after every global rule, so no
// fragment's allow outranks them. With no such model there's nowhere for a
// P0 session to run, and note says so
func privateAgent(r *registry.Registry, tools *env.Resolved) (agent map[string]any, note string) {
	var model string
	for _, role := range []string{"coding", "fallback"} {
		for _, id := range r.Roles[role].Members() {
			if m, ok := r.Models[id]; ok && slices.Contains(m.Privacy, "P0") {
				model = m.Provider + "/" + id
				break
			}
		}
		if model != "" {
			break
		}
	}
	if model == "" {
		return nil, "no private agent: neither the coding nor the fallback role has a model allowed P0"
	}
	var rules []any
	deny := func(action string) {
		rules = append(rules, map[string]any{"action": action, "resource": "*", "effect": "deny"})
	}
	for _, t := range cloudTools {
		deny(t)
	}
	for _, name := range tools.Names() {
		if !tools.Servers[name].P0() {
			deny(name + "_*")
		}
	}

	return map[string]any{
		"description": "Private (P0) work: a local model, and only tools that never leave the house",
		"mode":        "primary",
		"model":       model,
		"permissions": rules,
	}, ""
}

func strMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}

	return out
}

func strs(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}

	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
