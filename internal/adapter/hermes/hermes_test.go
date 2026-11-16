package hermes

import (
	"strings"
	"testing"

	"jgarner.dev/bud/internal/adapter/env"
)

func render(t *testing.T, srcs ...string) string {
	t.Helper()
	var frags []env.Fragment
	for _, s := range srcs {
		frags = append(frags, env.Fragment{Name: "test", Src: []byte(s)})
	}
	out, err := Render(frags)
	if err != nil {
		t.Fatal(err)
	}

	return string(out.Config)
}

func TestServersAndSettingsRenderAsSortedYAML(t *testing.T) {
	got := render(t, `{"bud": {
		"privacy": "P1",
		"gateway": "http://127.0.0.1:8790",
		"mcp": {
			"linear": {"gateway": true, "privacy": ["P1"]},
			"github": {"command": ["github-mcp-server", "stdio"], "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "{env:GH_PAT}"}, "privacy": ["P1"]}
		},
		"hermes": {
			"web": {"keyless_fallback": false, "backend": "exa"},
			"platform_toolsets": {"cli": ["web", "file"]},
			"mcp_servers": {"linear": {"trust": "untrusted"}, "gone": {"trust": "untrusted"}}
		}
	}}`)
	want := Header + `mcp_servers:
  github:
    args:
      - "stdio"
    command: "github-mcp-server"
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GH_PAT}"
  linear:
    headers:
      X-Bud-Privacy: "P1"
    trust: "untrusted"
    url: "http://127.0.0.1:8790/linear/mcp"
platform_toolsets:
  cli:
    - "web"
    - "file"
web:
  backend: "exa"
  keyless_fallback: false
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAWordYAMLWouldReadAsABoolIsQuoted(t *testing.T) {
	got := render(t, `{"bud": {"hermes": {"x": {"on": 1, "no": "yes", "a b": [], "e": {}}}}}`)
	for _, want := range []string{`  "on": 1`, `  "no": "yes"`, `  "a b": []`, `  e: {}`} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestAListOfMapsIsRefused(t *testing.T) {
	_, err := Render([]env.Fragment{{Name: "x", Src: []byte(`{"bud": {"hermes": {"a": [{"b": 1}]}}}`)}})
	if err == nil || !strings.Contains(err.Error(), "a:") {
		t.Fatalf("got %v", err)
	}
}

func TestRenderingIsDeterministic(t *testing.T) {
	src := `{"bud": {"hermes": {"z": 1, "a": {"y": true, "b": "c"}}}}`
	if render(t, src) != render(t, src) {
		t.Fatal("two renders differ")
	}
}
