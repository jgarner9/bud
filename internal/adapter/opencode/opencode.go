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
// itself ({env:VAR}) or by op run from an op:// reference (Suite D7)
package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

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

type Fragment struct {
	Name string // for errors, such as public/base or overlay/laptop
	Src  []byte
}

// Output is the rendered config and the secrets op run must fill in
type Output struct {
	Config []byte
	// Secrets maps an environment variable to the op:// reference it is read
	// from; bud writes these to an env file for op run --env-file
	Secrets map[string]string
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
			if m.Context > 0 {
				entry["limit"] = map[string]any{"context": m.Context}
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
		m, ok := r.Models[ro.Model]
		if !ok {
			return "", fmt.Errorf("render opencode.jsonc: the %s role's model %q isn't in the registry", role, ro.Model)
		}
		return m.Provider + "/" + ro.Model, nil
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

	for _, f := range in.Env {
		var frag map[string]any
		b, err := registry.StripJSONC(f.Src)
		if err == nil {
			err = json.Unmarshal(b, &frag)
		}
		if err != nil {
			return nil, fmt.Errorf("env fragment %s: %w", f.Name, err)
		}
		if _, ok := frag["update"]; ok {
			return nil, fmt.Errorf("env fragment %s sets update, which bud owns so only bud update moves OpenCode's pin; drop it", f.Name)
		}
		merge(cfg, frag)
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

// merge lays src over dst: objects merge key by key, arrays append (so an
// overlay's permission rules come after the public ones, and OpenCode's
// last-match-wins makes them count), and anything else replaces
func merge(dst, src map[string]any) {
	for k, v := range src {
		switch sv := v.(type) {
		case map[string]any:
			if dv, ok := dst[k].(map[string]any); ok {
				merge(dv, sv)
				continue
			}
		case []any:
			if dv, ok := dst[k].([]any); ok {
				dst[k] = append(slices.Clone(dv), sv...)
				continue
			}
		}
		dst[k] = v
	}
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
