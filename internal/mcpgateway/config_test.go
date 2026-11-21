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
	for _, name := range []string{"linear", "exa", "ops", "calendar"} {
		if _, ok := c.Servers[name]; !ok {
			t.Errorf("no %s in servers: %v", name, c.Servers)
		}
	}
	// the calendar stays with Qwen (Suite D82)
	if p := c.Servers["calendar"].Privacy; len(p) != 1 || p[0] != "P0" {
		t.Errorf("calendar privacy %v", p)
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
		{"privacy header", "127.0.0.1:1", Server{URL: "https://mcp.linear.app/mcp", Privacy: []string{"P1"}, Key: &Key{Header: "x-bud-privacy", Env: "K"}}, "linear", "gateway's own"},
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
		"mem": {URL: "http://[fd7a:115c:a1e0::2]:8792/mcp", Privacy: []string{"P0"}, ReadOnly: true},
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

func TestApprovalServerURLWithAQueryIsRefused(t *testing.T) {
	c := &Config{Listen: "127.0.0.1:0", Servers: map[string]Server{
		"linear": {URL: "http://127.0.0.1:1/mcp?key=abc", Privacy: []string{"P1"}, Approval: &Approval{}},
	}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "key header") {
		t.Errorf("got %v, want a query-string error naming the key header", err)
	}
	s := c.Servers["linear"]
	s.Approval, s.ReadOnly = nil, true
	c.Servers["linear"] = s
	if err := c.Validate(); err != nil {
		t.Errorf("without approval: %v", err)
	}
}

func TestApprovalOnTheOpsServerIsRefused(t *testing.T) {
	// ops reviews its own approval-tier tools, so a gateway review would ask twice
	c := &Config{Listen: "127.0.0.1:1", Servers: map[string]Server{
		"ops": {URL: "http://127.0.0.1:8792/mcp", Privacy: []string{"P1"}, Approval: &Approval{Tools: map[string]string{"*": "ask"}}},
	}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "reviewed twice") {
		t.Errorf("err = %v, want it to say ops enforces approvals itself", err)
	}
}

func TestServerWithoutApprovalOrReadOnlyIsRefusedButOpsIsNot(t *testing.T) {
	c := &Config{Listen: "127.0.0.1:1", Servers: map[string]Server{
		"linear": {URL: "https://mcp.linear.app/mcp", Privacy: []string{"P1"}},
		"ops":    {URL: "http://127.0.0.1:8792/mcp", Privacy: []string{"P0"}},
	}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "servers.linear.approval") || !strings.Contains(err.Error(), "bud sync") {
		t.Fatalf("err = %v, want it to name linear and say to run bud sync", err)
	}
	if strings.Contains(err.Error(), "servers.ops") {
		t.Errorf("ops was refused: %v", err)
	}
}

func TestRenderedApprovalsWithReadOnlyAndToolsAreRefused(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp-gateway.json")
	cfg := `{"listen":"127.0.0.1:1","servers":{"linear":{"url":"https://mcp.linear.app/mcp","privacy":["P1"]}}}`
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	rendered := `{"servers":{"linear":{"read_only":true,"tools":{}}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp-gateway-approvals.json"), []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("Read = %v, want read_only and tools refused", err)
	}
}

func TestAnEmptyRenderedToolsBlockAsksEverything(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp-gateway.json")
	cfg := `{"listen":"127.0.0.1:1","servers":{"linear":{"url":"https://mcp.linear.app/mcp","privacy":["P1"]}}}`
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp-gateway-approvals.json"), []byte(`{"servers":{"linear":{"tools":{}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Servers["linear"].Approval.Asks("get_issue") {
		t.Error("an empty tools block let a call through without asking")
	}
}

func TestReadLaysTheRenderedApprovalsOverTheServers(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"listen":"127.0.0.1:1","servers":{"linear":{"url":"https://mcp.linear.app/mcp","privacy":["P1"]},
		"exa":{"url":"https://mcp.exa.ai/mcp","privacy":["P1"]}}}`
	p := filepath.Join(dir, "mcp-gateway.json")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil || !strings.Contains(err.Error(), "servers.linear.approval") {
		t.Fatalf("before sync: %v", err)
	}
	rendered := `{"servers":{"linear":{"tools":{"get_*":"allow"}},"exa":{"read_only":true},"gone":{"read_only":true}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp-gateway-approvals.json"), []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Servers["linear"].Approval.Asks("get_issue") || !c.Servers["linear"].Approval.Asks("save_issue") || !c.Servers["exa"].ReadOnly {
		t.Errorf("servers %+v", c.Servers)
	}
}
