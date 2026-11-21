package opencode

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/registry"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func shipped(t *testing.T) Input {
	t.Helper()
	r, err := registry.Load(filepath.Join("..", "..", "..", "content", "env", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join("..", "..", "..", "content", "env", "base.jsonc"))
	if err != nil {
		t.Fatal(err)
	}

	return Input{
		Registry:     r,
		Skills:       []string{"/home/you/.local/share/bud/active/skills"},
		Instructions: []string{"/home/you/.local/share/bud/content/current/AGENTS.md"},
		Env:          []Fragment{{Name: "public/base", Src: base}},
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs; check the diff and run go test ./internal/adapter/opencode -update\ngot:\n%s", path, got)
	}
}

func TestTheShippedRegistryRendersToTheGoldenConfig(t *testing.T) {
	out, err := Render(shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "dev-machine.jsonc", out.Config)
	if len(out.Secrets) != 0 {
		t.Fatalf("the public registry uses {env:...} keys only, got secrets %v", out.Secrets)
	}
}

func TestRenderingIsDeterministic(t *testing.T) {
	a, _ := Render(shipped(t))
	b, _ := Render(shipped(t))
	if !bytes.Equal(a.Config, b.Config) {
		t.Fatal("two renders of one input differ")
	}
}

// decode reads a rendered config back, header and all
func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	clean, err := registry.StripJSONC(b)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(clean, &cfg); err != nil {
		t.Fatal(err)
	}

	return cfg
}

func TestRolesBecomeModelsAndUpdatesAreOff(t *testing.T) {
	out, err := Render(shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg := decode(t, out.Config)
	if cfg["model"] != "go/glm-5.3" || cfg["update"] != "disable" {
		t.Fatalf("model %v, update %v", cfg["model"], cfg["update"])
	}
	plan := cfg["agents"].(map[string]any)["plan"].(map[string]any)
	if plan["model"] != "go/kimi-k3.1-pro" {
		t.Fatalf("the plan agent should use the planning role, got %v", plan["model"])
	}
	providers := cfg["providers"].(map[string]any)
	if _, ok := providers["embed"]; ok {
		t.Fatal("a provider with only embedding models has nothing for OpenCode")
	}
	goProvider := providers["go"].(map[string]any)
	if env := goProvider["env"].([]any); len(env) != 1 || env[0] != "OPENCODE_GO_API_KEY" {
		t.Fatalf("go's key should come from OPENCODE_GO_API_KEY, got %v", env)
	}
	if _, ok := providers["qwen"].(map[string]any)["env"]; ok {
		t.Fatal("a provider with no key names no variable")
	}
}

func TestAnOpReferenceBecomesASecretForOpRun(t *testing.T) {
	in := shipped(t)
	p := in.Registry.Providers["go"]
	p.Key = "op://vault/opencode-go/credential"
	in.Registry.Providers["go"] = p
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Secrets["BUD_GO_API_KEY"] != "op://vault/opencode-go/credential" {
		t.Fatalf("secrets = %v", out.Secrets)
	}
	if strings.Contains(string(out.Config), "op://") {
		t.Fatal("the reference belongs in the env file, not the config")
	}
}

func TestFragmentSecretsJoinTheProviderKeysForOpRun(t *testing.T) {
	in := shipped(t)
	in.Env = append(in.Env, Fragment{Name: "overlay/personal", Src: []byte(`{"bud": {"opencode_secrets": {"EXA_API_KEY": "op://vault/exa/key"}}}`)})
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Secrets["EXA_API_KEY"] != "op://vault/exa/key" {
		t.Fatalf("secrets = %v", out.Secrets)
	}
	if strings.Contains(string(out.Config), "op://") || strings.Contains(string(out.Config), "opencode_secrets") {
		t.Fatal("the reference belongs in the env file, not the config")
	}

	// a fragment can't point a provider's key variable somewhere else
	p := in.Registry.Providers["go"]
	p.Key = "op://vault/opencode-go/credential"
	in.Registry.Providers["go"] = p
	in.Env = append(in.Env, Fragment{Name: "overlay/other", Src: []byte(`{"bud": {"opencode_secrets": {"BUD_GO_API_KEY": "op://vault/elsewhere/key"}}}`)})
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "bud.opencode_secrets.BUD_GO_API_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestFragmentsMergeInOrderAndRulesAppend(t *testing.T) {
	in := shipped(t)
	in.Env = append(in.Env, Fragment{Name: "overlay/laptop", Src: []byte(`{
		// mine
		"permissions": [{ "action": "shell", "resource": "cargo test *", "effect": "allow" }],
		"agents": { "plan": { "description": "planning" } },
		"model": "qwen/qwen-35b",
	}`)})
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	cfg := decode(t, out.Config)
	rules := cfg["permissions"].([]any)
	last := rules[len(rules)-1].(map[string]any)
	if last["resource"] != "cargo test *" || rules[0].(map[string]any)["resource"] != "*" {
		t.Fatalf("the overlay's rule should come last, after the public ones: first %v, last %v", rules[0], last)
	}
	plan := cfg["agents"].(map[string]any)["plan"].(map[string]any)
	if plan["model"] != "go/kimi-k3.1-pro" || plan["description"] != "planning" {
		t.Fatalf("objects merge key by key: %v", plan)
	}
	if cfg["model"] != "qwen/qwen-35b" {
		t.Fatal("a scalar in a later fragment replaces")
	}
}

func TestAFragmentCantTurnUpdatesBackOn(t *testing.T) {
	in := shipped(t)
	in.Env = append(in.Env, Fragment{Name: "overlay/x", Src: []byte(`{"update": "auto"}`)})
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "overlay/x sets update") {
		t.Fatalf("got %v", err)
	}
}

func TestABrokenFragmentIsNamed(t *testing.T) {
	in := shipped(t)
	in.Env = append(in.Env, Fragment{Name: "overlay/bad", Src: []byte(`{"permissions": [`)})
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "overlay/bad") {
		t.Fatalf("got %v", err)
	}
}

func TestAnOutputLimitNeedsNoContext(t *testing.T) {
	in := shipped(t)
	m := in.Registry.Models["glm-5.3"]
	m.Context = 0
	in.Registry.Models["glm-5.3"] = m
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	glm := decode(t, out.Config)["providers"].(map[string]any)["go"].(map[string]any)["models"].(map[string]any)["glm-5.3"].(map[string]any)
	limit, _ := glm["limit"].(map[string]any)
	if _, ok := limit["context"]; ok || limit["output"] != float64(131072) {
		t.Fatalf("want only the output limit, got %v", glm["limit"])
	}
}

func withCoding(t *testing.T) Input {
	t.Helper()
	in := shipped(t)
	coding, err := os.ReadFile(filepath.Join("..", "..", "..", "content", "env", "coding.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	in.Env = append(in.Env, Fragment{Name: "public/coding", Src: coding})

	return in
}

// Without an overlay there's no gateway or memory URL, so those servers are
// left out and named, and the rest still render
func TestPublicContentAloneLeavesOutTheGatewayServers(t *testing.T) {
	out, err := Render(withCoding(t))
	if err != nil {
		t.Fatal(err)
	}
	servers := decode(t, out.Config)["mcp"].(map[string]any)["servers"].(map[string]any)
	for _, gone := range []string{"linear", "exa", "memory", "ops"} {
		if _, ok := servers[gone]; ok {
			t.Errorf("%s rendered without its address", gone)
		}
	}
	for _, kept := range []string{"context7", "serena", "cocoindex", "playwright"} {
		if _, ok := servers[kept]; !ok {
			t.Errorf("%s is missing", kept)
		}
	}
	if len(out.Notes) != 4 {
		t.Fatalf("notes = %q", out.Notes)
	}
}

func TestTheLaptopsOwnWebsearchIsOff(t *testing.T) {
	out, err := Render(withCoding(t))
	if err != nil {
		t.Fatal(err)
	}
	if ws, ok := decode(t, out.Config)["websearch"]; !ok || ws != false {
		t.Fatalf("websearch = %v", ws)
	}
}

// The P0 tool rule (Suite D81): the private agent runs on a P0 model and
// denies every server that isn't allowed P0, after every global rule
func TestThePrivateAgentDeniesEveryCloudTool(t *testing.T) {
	in := withCoding(t)
	in.Env = append(in.Env, Fragment{Name: "overlay/personal", Src: []byte(`{"bud": {"gateway": "http://127.0.0.1:8790", "mcp": {"memory": {"url": "http://127.0.0.1:8791/mcp"}}}}`)})
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	agent := decode(t, out.Config)["agents"].(map[string]any)[PrivateAgent].(map[string]any)
	if agent["model"] != "qwen/qwen-35b" || agent["mode"] != "primary" {
		t.Fatalf("agent = %v", agent)
	}
	denied := map[string]bool{}
	for _, r := range agent["permissions"].([]any) {
		rule := r.(map[string]any)
		if rule["effect"] != "deny" || rule["resource"] != "*" {
			t.Fatalf("rule %v isn't a deny", rule)
		}
		denied[rule["action"].(string)] = true
	}
	for _, want := range []string{"linear_*", "exa_*", "context7_*", "playwright_*", "webfetch", "websearch", "subagent"} {
		if !denied[want] {
			t.Errorf("the private agent doesn't deny %s: %v", want, denied)
		}
	}
	for _, p0 := range []string{"memory_*", "serena_*", "cocoindex_*"} {
		if denied[p0] {
			t.Errorf("the private agent denies P0-safe %s", p0)
		}
	}
}

func TestNoP0ModelMeansNoPrivateAgentAndANote(t *testing.T) {
	in := shipped(t)
	for id, m := range in.Registry.Models {
		m.Privacy = []string{"P1", "P2"}
		in.Registry.Models[id] = m
	}
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decode(t, out.Config)["agents"].(map[string]any)[PrivateAgent]; ok {
		t.Fatal("rendered a private agent with no P0 model")
	}
	if len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "no private agent") {
		t.Fatalf("notes = %q", out.Notes)
	}
}

// A fragment can add to a server's entry, but extras for one that was left
// out don't make an entry OpenCode would refuse
func TestExtrasForALeftOutServerAreDropped(t *testing.T) {
	in := withCoding(t)
	in.Env = append(in.Env, Fragment{Name: "overlay/x", Src: []byte(`{"mcp": {"servers": {"linear": {"codemode": false}, "context7": {"codemode": false}}}}`)})
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	servers := decode(t, out.Config)["mcp"].(map[string]any)["servers"].(map[string]any)
	if _, ok := servers["linear"]; ok {
		t.Fatal("linear's extras made an entry")
	}
	if c := servers["context7"].(map[string]any); c["codemode"] != false || c["type"] != "remote" {
		t.Fatalf("context7 = %v", c)
	}
}

// Every server needs its tiers from bud.mcp, so the private agent can deny
// it, and a fragment's own entry adds fields but never the transport or the
// private agent's rules
func TestFragmentsCantGoAroundTheP0Rule(t *testing.T) {
	for src, want := range map[string]string{
		`{"mcp": {"servers": {"rogue": {"type": "remote", "url": "https://example.com/mcp"}}}}`:           "mcp.servers.rogue isn't in bud.mcp",
		`{"mcp": {"servers": {"serena": {"type": "remote", "url": "https://example.com/mcp"}}}}`:          "mcp.servers.serena.type comes from bud.mcp.serena",
		`{"agents": {"private": {"model": "go/glm-5.3"}}}`:                                                "sets agents.private",
		`{"agents": {"private": {"permissions": [{"action": "*", "resource": "*", "effect": "allow"}]}}}`: "sets agents.private",
	} {
		in := withCoding(t)
		in.Env = append(in.Env, Fragment{Name: "overlay/x", Src: []byte(src)})
		if _, err := Render(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", src, err, want)
		}
	}
}
