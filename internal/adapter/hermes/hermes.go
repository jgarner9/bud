// Package hermes is the Hermes adapter (Suite D12, D46, D48): it renders the
// config.yaml bud sync writes for Hermes, the home server's manager. For now
// it holds the tool settings: the fragments' bud.hermes block laid over an
// empty config (the built-in toolsets per platform and the web backends,
// Suite D77, D85) and the MCP servers from the bud block (Suite D72-D78).
// The models, providers and the D48 changes come with Hermes's install
// (JGA-290).
//
// The file is YAML, written by a small emitter here, since bud takes no
// third-party modules (D1): every string double-quoted, keys sorted, so the
// same input always gives the same bytes. Values a fragment reads from the
// environment, {env:VAR}, become Hermes's own ${VAR}; Hermes refuses to
// connect a remote server whose URL or headers name an unset one, so a
// missing variable fails closed
package hermes

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"jgarner.dev/bud/internal/adapter/env"
)

// Header opens the rendered file, so a hand edit is never a surprise loss
const Header = "# Written by bud sync from env fragments. Edits here are replaced on the next\n# sync: change content/env or your overlay's env instead.\n"

// Output is the rendered config
type Output struct {
	Config []byte
	// Notes say what was left out and why
	Notes []string
}

// Render builds config.yaml from the env fragments, in the order they apply
func Render(frags []env.Fragment) (*Output, error) {
	merged, err := env.Merge(frags)
	if err != nil {
		return nil, err
	}
	tools, err := env.Take(merged)
	if err != nil {
		return nil, err
	}

	cfg := map[string]any{}
	if len(tools.Servers) > 0 {
		servers := map[string]any{}
		for _, name := range tools.Names() {
			s := tools.Servers[name]
			var entry map[string]any
			if s.Remote() {
				entry = map[string]any{"url": hermesRefs(s.URL)}
				if len(s.Headers) > 0 {
					entry["headers"] = refMap(s.Headers)
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
		cfg["mcp_servers"] = servers
	}
	// the fragments' own Hermes settings, which may add to a server's entry
	// (say, trust: untrusted) but not bring back one that was left out
	if tools.Hermes != nil {
		extra, err := clone(tools.Hermes)
		if err != nil {
			return nil, err
		}
		if m, ok := extra["mcp_servers"].(map[string]any); ok {
			for name := range m {
				if _, ok := tools.Servers[name]; !ok {
					delete(m, name)
				}
			}
		}
		env.MergeInto(cfg, extra)
	}

	var b strings.Builder
	b.WriteString(Header)
	if err := writeYAML(&b, cfg, 0); err != nil {
		return nil, err
	}

	return &Output{Config: []byte(b.String()), Notes: tools.Notes()}, nil
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

// writeYAML writes a map as block-style YAML at the given indent
func writeYAML(b *strings.Builder, m map[string]any, indent int) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	pad := strings.Repeat("  ", indent)
	for _, k := range keys {
		b.WriteString(pad + key(k) + ":")
		if err := writeValue(b, m[k], indent); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}

	return nil
}

// writeValue writes v after a key or a list dash: a scalar or empty value on
// the same line, a map or list on the lines below
func writeValue(b *strings.Builder, v any, indent int) error {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			b.WriteString(" {}\n")
			return nil
		}
		b.WriteString("\n")
		return writeYAML(b, x, indent+1)
	case []any:
		if len(x) == 0 {
			b.WriteString(" []\n")
			return nil
		}
		b.WriteString("\n")
		pad := strings.Repeat("  ", indent+1)
		for _, item := range x {
			switch item.(type) {
			case map[string]any, []any:
				return fmt.Errorf("a list holds a map or a list, which this emitter doesn't write; use a scalar list")
			}
			b.WriteString(pad + "-")
			if err := writeValue(b, item, indent+1); err != nil {
				return err
			}
		}
		return nil
	}
	s, err := scalar(v)
	if err != nil {
		return err
	}
	b.WriteString(" " + s + "\n")

	return nil
}

func scalar(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case string:
		return quote(x), nil
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return "", fmt.Errorf("%v isn't a number YAML can hold", x)
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}

	return "", fmt.Errorf("can't write a %T", v)
}

var plainKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// key writes a map key plain when YAML can't read it as anything but that
// string, and quoted otherwise
func key(k string) string {
	switch strings.ToLower(k) {
	case "y", "n", "yes", "no", "on", "off", "true", "false", "null":
		return quote(k)
	}
	if plainKey.MatchString(k) {
		return k
	}

	return quote(k)
}

// quote is a JSON string, which is also a YAML double-quoted scalar
func quote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes

	return strings.TrimSuffix(b.String(), "\n")
}
