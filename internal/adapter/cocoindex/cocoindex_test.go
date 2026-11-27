package cocoindex

import (
	"strings"
	"testing"

	"jgarner.dev/bud/internal/registry"
)

func reg(t *testing.T, src string) *registry.Registry {
	t.Helper()
	r, err := registry.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	return r
}

const embedModel = `"m": {"provider": "embed", "name": "qwen3-embedding-0.6b", "privacy": ["P0"], "cost": "C0", "inputs": ["text"], "caps": ["embed"], "dims": 1024}`

func TestRenderNamesTheEmbeddingModelAndItsEndpoint(t *testing.T) {
	r := reg(t, `{"version": 1, "providers": {"embed": {"api": "openai", "endpoint": "http://host.example:8082/v1", "local": true}},
		"models": {`+embedModel+`}, "roles": {"embedding": {"needs": ["embed"], "pool": ["m"]}}}`)
	data, note, err := Render(r)
	if err != nil || note != "" {
		t.Fatalf("got note %q, err %v", note, err)
	}
	want := Header + "embedding:\n  provider: litellm\n  model: \"openai/qwen3-embedding-0.6b\"\nenvs:\n  OPENAI_API_BASE: \"http://host.example:8082/v1\"\n  OPENAI_API_KEY: not-needed\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	again, _, _ := Render(r)
	if string(again) != string(data) {
		t.Fatal("two renders differ")
	}
}

func TestRenderSaysWhyItWroteNothing(t *testing.T) {
	cases := map[string]struct{ src, note string }{
		"no embedding role": {
			`{"version": 1, "providers": {}, "models": {}, "roles": {}}`,
			"no embedding role",
		},
		"a provider that needs a key": {
			`{"version": 1, "providers": {"embed": {"api": "openai", "endpoint": "https://e.example/v1", "key": "op://vault/item/field"}},
			  "models": {` + embedModel + `}, "roles": {"embedding": {"needs": ["embed"], "pool": ["m"]}}}`,
			"needs an API key",
		},
		"a provider that isn't OpenAI-compatible": {
			`{"version": 1, "providers": {"embed": {"api": "anthropic", "endpoint": "https://e.example/v1"}},
			  "models": {` + embedModel + `}, "roles": {"embedding": {"needs": ["embed"], "pool": ["m"]}}}`,
			"OpenAI-compatible",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := registry.Parse([]byte(c.src))
			if err != nil {
				t.Skipf("the registry doesn't validate here: %v", err)
			}
			data, note, err := Render(r)
			if err != nil || data != nil || !strings.Contains(note, c.note) {
				t.Fatalf("got data %q, note %q, err %v; want a note with %q", data, note, err, c.note)
			}
			if strings.Contains(note, "op://") {
				t.Fatalf("the note quotes a reference: %q", note)
			}
		})
	}
}

func TestRenderRefusesAnEndpointThatIsNotHTTP(t *testing.T) {
	r := reg(t, `{"version": 1, "providers": {"embed": {"api": "openai", "endpoint": "file:///x", "local": true}},
		"models": {`+embedModel+`}, "roles": {"embedding": {"needs": ["embed"], "pool": ["m"]}}}`)
	if _, _, err := Render(r); err == nil {
		t.Fatal("a file URL rendered")
	}
}
