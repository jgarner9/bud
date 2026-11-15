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
