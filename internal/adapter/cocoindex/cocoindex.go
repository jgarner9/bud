// Package cocoindex renders cocoindex-code's global settings, the file its
// MCP server needs before it will start (JGA-424). It makes embeddings with
// the model the registry's embedding role names, on the provider that role's
// model lives on, so a device indexes code on bud's own embedding server and
// nothing leaves the house (Suite D52). The endpoint comes from the merged
// registry, so its host stays in the private overlay and never in this repo
package cocoindex

import (
	"encoding/json"
	"fmt"
	"net/url"

	"jgarner.dev/bud/internal/registry"
)

// Role is the registry role whose first usable model embeds the code
const Role = "embedding"

// Header opens the rendered file and says what a hand edit does: sync leaves
// the file as it is and names it (planFiles), so an edit is never lost
const Header = "# Written by bud sync from the registry's embedding role. If you edit this file,\n# bud sync leaves it alone and says so; move it aside to let sync write it again.\n"

// Render builds global_settings.yml. note is empty when it rendered, and
// otherwise says why it didn't, which is never an error: a registry with no
// embedding model on an OpenAI-compatible endpoint leaves cocoindex to its
// own setup
func Render(r *registry.Registry) (data []byte, note string, err error) {
	if r == nil {
		return nil, "", fmt.Errorf("render cocoindex settings: no registry")
	}
	role, ok := r.Roles[Role]
	if !ok {
		return nil, "cocoindex-code's settings weren't written: the registry has no " + Role + " role", nil
	}
	for _, id := range role.Members() {
		m, ok := r.Models[id]
		if !ok {
			continue
		}
		p, ok := r.Providers[m.Provider]
		if !ok || p.API != "openai" {
			continue
		}
		if p.Key != "" {
			// the file is plain text and LiteLLM would read a reference as the key itself
			return nil, "cocoindex-code's settings weren't written: " + id + "'s provider needs an API key, and this file holds none", nil
		}
		u, err := url.Parse(p.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, "", fmt.Errorf("render cocoindex settings: %s's provider %q has no http or https endpoint", id, m.Provider)
		}

		return []byte(Header + "embedding:\n  provider: litellm\n  model: " + quote("openai/"+m.Name) + "\nenvs:\n  OPENAI_API_BASE: " + quote(p.Endpoint) + "\n  OPENAI_API_KEY: not-needed\n"), "", nil
	}

	return nil, "cocoindex-code's settings weren't written: no model in the " + Role + " role is on an OpenAI-compatible provider", nil
}

// quote writes s as a YAML double-quoted string; a JSON string is one
func quote(s string) string {
	b, _ := json.Marshal(s)

	return string(b)
}
