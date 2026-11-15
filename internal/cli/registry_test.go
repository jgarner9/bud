package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryCheckPassesTheShippedRegistry(t *testing.T) {
	out := captureStdout(t, func() {
		if err := registryCheck([]string{filepath.Join("..", "..", "content", "env", "registry.jsonc")}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "all 9 roles filled") {
		t.Fatalf("got %q", out)
	}
}

func TestRegistryCheckFailsAnOverlayThatBreaksIt(t *testing.T) {
	overlay := filepath.Join(t.TempDir(), "overlay.jsonc")
	// the classifier sees private data, so a cloud model there must fail
	os.WriteFile(overlay, []byte(`{"roles": {"classifier": {"pool": ["glm-5.3"]}}}`), 0o644)
	err := registryCheck([]string{filepath.Join("..", "..", "content", "env", "registry.jsonc"), overlay})
	if err == nil || !strings.Contains(err.Error(), "classifier sees private data") || !strings.Contains(err.Error(), "over it") {
		t.Fatalf("got %v", err)
	}
}

func TestRegistryCheckNeedsAFile(t *testing.T) {
	var u *UsageError
	if err := registryCheck(nil); !errors.As(err, &u) {
		t.Fatalf("want a usage error, got %v", err)
	}
}

func TestRegistryRefreshReportsThenRewritesFacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": [
		  {"id": "glm-5.3", "modalities": {"input": ["text"]}, "capabilities": {"tools": {"supported": true}},
		   "limit": {"context": 1000000, "output": 131072}, "cost": {"text": {"input": {"price": 1.5}, "output": {"price": 4.4}}}},
		  {"id": "grok-4.7"}]}`))
	}))
	t.Cleanup(srv.Close)
	orig := refreshClient
	refreshClient = srv.Client()
	t.Cleanup(func() { refreshClient = orig })

	src := `{
  "version": 1,
  "providers": {
    "go": { "api": "openai", "endpoint": "` + srv.URL + `/v1" },
    "qwen": { "api": "openai", "endpoint": "http://qwen.invalid:8080/v1", "local": true },
  },
  "models": {
    // the coding pick
    "glm-5.3": { "provider": "go", "name": "glm-5.3", "privacy": ["P1", "P2"], "cost": "C1", "caps": ["tools"], "grade": "top" },
    "gone": { "provider": "go", "name": "kimi-k3", "privacy": ["P1", "P2"], "cost": "C1", "caps": ["tools"] },
    "qwen-35b": { "provider": "qwen", "name": "qwen-35b", "privacy": ["P0", "P1", "P2"], "cost": "C0" },
    "vec": { "provider": "qwen", "name": "vec", "privacy": ["P0"], "cost": "C0", "dims": 1024 },
  },
  "roles": {
    "manager": { "pool": ["qwen-35b", "glm-5.3"] }, "coding": { "pool": ["glm-5.3", "gone"], "needs": ["tools"] },
    "planning": { "pool": ["glm-5.3"] }, "classifier": { "pool": ["qwen-35b"] }, "fim": { "pool": ["qwen-35b"] },
    "embedding": { "pool": ["vec"] }, "fallback": { "pool": ["qwen-35b"] }, "curator": { "pool": ["qwen-35b"] },
    "vision": { "pool": ["qwen-35b"] },
  },
}`
	path := filepath.Join(t.TempDir(), "registry.jsonc")
	os.WriteFile(path, []byte(src), 0o644)

	out := captureStdout(t, func() {
		if err := registryRefresh([]string{path, "go"}, false); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"go: 2 registry models checked against 2 listed",
		`glm-5.3.price: unset -> { "in": 1.5, "out": 4.4 }`,
		"gone: gone (no longer listed",
		"not in the registry: grok-4.7",
		"run with --write",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != src {
		t.Fatal("without --write the file must not change")
	}

	captureStdout(t, func() {
		if err := registryRefresh([]string{path, "go"}, true); err != nil {
			t.Fatal(err)
		}
	})
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"grade": "top", "inputs": ["text"], "context": 1000000, "output": 131072, "price": { "in": 1.5, "out": 4.4 } },`) ||
		!strings.Contains(string(b), "// the coding pick") {
		t.Fatalf("want the facts added and the comment kept:\n%s", b)
	}
	if err := registryCheck([]string{path}); err != nil {
		t.Fatalf("the rewritten file must still pass: %v", err)
	}
}

func TestRegistryRefreshNeedsAKnownProvider(t *testing.T) {
	var u *UsageError
	if err := registryRefresh([]string{filepath.Join("..", "..", "content", "env", "registry.jsonc")}, false); !errors.As(err, &u) {
		t.Fatalf("want a usage error, got %v", err)
	}
	if err := registryRefresh([]string{filepath.Join("..", "..", "content", "env", "registry.jsonc"), "nope"}, false); !errors.As(err, &u) {
		t.Fatalf("want a usage error, got %v", err)
	}
}
