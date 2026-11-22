package cli

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	models "jgarner.dev/bud/internal/registry"
)

// the command needs its five tokens from op run, a loopback address and a
// local embedding model, since devices reach it only through the gateway
func TestMemoryMCPChecksItsSetup(t *testing.T) {
	var served string
	orig := serveMemory
	serveMemory = func(_ context.Context, addr string, _ http.Handler) error { served = addr; return nil }
	t.Cleanup(func() { serveMemory = orig })
	// the public registry, whose embedding model is local
	reg := filepath.Join("..", "..", "content", "env", "registry.jsonc")

	for _, name := range memoryTokens {
		t.Setenv(name, "")
	}
	t.Setenv("BUD_MEMORY_TOKEN_P0", "t0")
	err := runMemoryMCP(&Globals{}, nil, "127.0.0.1:8798", "/run/m.sock", reg, true)
	if err == nil || !strings.Contains(err.Error(), "BUD_MEMORY_TOKEN_P1, BUD_MEMORY_TOKEN_P2, BUD_MEMORY_TOKEN_STAGE, BUD_MEMORY_TOKEN_EMBED are empty") {
		t.Fatalf("missing tokens: err = %v", err)
	}
	for _, name := range memoryTokens {
		t.Setenv(name, "tok-"+name)
	}
	if err := runMemoryMCP(&Globals{}, nil, "100.64.0.1:8798", "/run/m.sock", reg, true); err == nil || !strings.Contains(err.Error(), "memory through the MCP gateway") {
		t.Errorf("a tailnet listen address: %v", err)
	}
	if err := runMemoryMCP(&Globals{}, []string{"x"}, "127.0.0.1:8798", "/run/m.sock", reg, true); err == nil {
		t.Error("an argument was accepted")
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if err := runMemoryMCP(&Globals{}, nil, "127.0.0.1:8798", "", reg, true); err == nil || !strings.Contains(err.Error(), "pass --socket") {
		t.Errorf("no runtime dir and no --socket: %v", err)
	}

	if err := runMemoryMCP(&Globals{}, nil, "127.0.0.1:8798", "/run/m.sock", reg, true); err != nil {
		t.Fatal(err)
	}
	if served != "127.0.0.1:8798" {
		t.Errorf("served on %q", served)
	}
}

func TestEmbeddingModelMustBeLocal(t *testing.T) {
	r := &models.Registry{
		Providers: map[string]models.Provider{"cloud": {Endpoint: "https://x.example/v1"}, "home": {Endpoint: "http://127.0.0.1:8082/v1", Local: true}},
		Models:    map[string]models.Model{"c": {Provider: "cloud", Name: "c", Dims: 8}, "h": {Provider: "home", Name: "h", Dims: 1024}, "nodims": {Provider: "home", Name: "n"}},
		Roles:     map[string]models.Role{"embedding": {Pool: []string{"c"}}},
	}
	if _, _, err := embeddingModel(r); err == nil || !strings.Contains(err.Error(), "local provider") {
		t.Errorf("a cloud embedding model: %v", err)
	}
	r.Roles["embedding"] = models.Role{Pool: []string{"nodims"}}
	if _, _, err := embeddingModel(r); err == nil {
		t.Error("an embedding model with no dims was accepted")
	}
	r.Roles["embedding"] = models.Role{Pool: []string{"h", "c"}}
	if m, p, err := embeddingModel(r); err != nil || m.Name != "h" || p.Endpoint != "http://127.0.0.1:8082/v1" {
		t.Errorf("embeddingModel = %v, %v, %v", m, p, err)
	}
	if _, _, err := embeddingModel(&models.Registry{}); err == nil {
		t.Error("a registry with no embedding role was accepted")
	}
}
