package mcpgateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheExampleConfigIsValid(t *testing.T) {
	c, err := Read(filepath.Join("..", "..", "server", "mcp-gateway", "mcp-gateway.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Servers) != 3 {
		t.Errorf("servers: %v", c.Servers)
	}
}

func TestValidateNamesEachProblem(t *testing.T) {
	good := func() Server { return Server{URL: "https://mcp.linear.app/mcp", Privacy: []string{"P1"}} }
	tests := []struct {
		name   string
		listen string
		server Server
		key    string
		want   string
	}{
		{"wildcard listen", ":8790", good(), "linear", "loopback or tailnet"},
		{"all-interfaces listen", "0.0.0.0:8790", good(), "linear", "loopback or tailnet"},
		{"LAN listen", "192.168.1.5:8790", good(), "linear", "loopback or tailnet"},
		{"localhost listen", "localhost:8790", good(), "linear", "loopback or tailnet"},
		{"no port", "127.0.0.1", good(), "linear", "host:port"},
		{"bad name", "127.0.0.1:1", good(), "Linear", "lowercase"},
		{"plain http to the cloud", "127.0.0.1:1", Server{URL: "http://mcp.linear.app/mcp", Privacy: []string{"P1"}}, "linear", "use https"},
		{"P0 to the cloud", "127.0.0.1:1", Server{URL: "https://mcp.linear.app/mcp", Privacy: []string{"P0", "P1"}}, "linear", "D81"},
		{"unknown tier", "127.0.0.1:1", Server{URL: "https://mcp.linear.app/mcp", Privacy: []string{"P4"}}, "linear", `"P4" isn't a tier`},
		{"no tiers", "127.0.0.1:1", Server{URL: "https://mcp.linear.app/mcp"}, "linear", "list the session tiers"},
		{"bad env", "127.0.0.1:1", Server{URL: "https://mcp.linear.app/mcp", Privacy: []string{"P1"}, Key: &Key{Header: "Authorization", Env: "op://vault/x/y"}}, "linear", "environment variable"},
		{"no header", "127.0.0.1:1", Server{URL: "https://mcp.linear.app/mcp", Privacy: []string{"P1"}, Key: &Key{Env: "K"}}, "linear", "key.header"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Listen: tt.listen, Servers: map[string]Server{tt.key: tt.server}}
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestInsideServersMayTakeP0(t *testing.T) {
	c := &Config{Listen: "100.64.0.1:8790", Servers: map[string]Server{
		"ops": {URL: "http://127.0.0.1:8791/mcp", Privacy: []string{"P0", "P1"}},
		"mem": {URL: "http://[fd7a:115c:a1e0::2]:8792/mcp", Privacy: []string{"P0"}},
	}}
	if err := c.Validate(); err != nil {
		t.Error(err)
	}
}

func TestReadRefusesUnknownKeysAndSaysWhereTheFileGoes(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(filepath.Join(dir, "none.json")); err == nil || !strings.Contains(err.Error(), "mcp-gateway.example.json") {
		t.Errorf("missing file: %v", err)
	}
	p := filepath.Join(dir, "c.json")
	if err := os.WriteFile(p, []byte(`{"listen":"127.0.0.1:1","servers":{},"extra":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Errorf("unknown key: %v", err)
	}
}
