package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/mcpgateway"
)

// the command reads the config from the state dir, opens the audit log
// there, and refuses to start when a key op run should have set is missing
func TestMCPGatewayReadsItsConfigAndNeedsItsKeys(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", state)
	cfg := `{"listen": "127.0.0.1:8790", "servers": {"exa": {"url": "https://mcp.exa.ai/mcp", "privacy": ["P2"], "read_only": true,
		"key": {"header": "x-api-key", "env": "BUD_TEST_EXA_KEY"}}}}`
	if err := os.MkdirAll(filepath.Join(state, "bud"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "bud", "mcp-gateway.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var served *mcpgateway.Config
	orig := serveGateway
	serveGateway = func(_ context.Context, c *mcpgateway.Config, _ http.Handler) error { served = c; return nil }
	t.Cleanup(func() { serveGateway = orig })

	reg := filepath.Join("..", "..", "content", "env", "registry.jsonc")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("BUD_TEST_EXA_KEY", "k")
	// no --registry and nothing synced: the privacy rules can't be read, so
	// the gateway doesn't start
	if err := runMCPGateway(&Globals{}, nil, "", ""); err == nil || !strings.Contains(err.Error(), "bud sync") {
		t.Fatalf("no registry: err = %v", err)
	}
	t.Setenv("BUD_TEST_EXA_KEY", "")
	if err := runMCPGateway(&Globals{}, nil, "", reg); err == nil || !strings.Contains(err.Error(), "BUD_TEST_EXA_KEY") {
		t.Fatalf("missing key: err = %v", err)
	}
	t.Setenv("BUD_TEST_EXA_KEY", "k")
	if err := runMCPGateway(&Globals{}, nil, "", reg); err != nil {
		t.Fatal(err)
	}
	if served == nil || served.Listen != "127.0.0.1:8790" {
		t.Errorf("served %+v", served)
	}
	if fi, err := os.Stat(filepath.Join(state, "bud", "mcp-audit.jsonl")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("audit log: %v %v", fi, err)
	}
	if err := runMCPGateway(&Globals{}, []string{"x"}, "", reg); err == nil {
		t.Error("an argument was accepted")
	}
}
