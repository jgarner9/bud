package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTrustFiles(t *testing.T, tiers, gateway string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	tp, gp := filepath.Join(dir, "hermes-tiers.json"), filepath.Join(dir, "mcp-gateway.json")
	if tiers != "" {
		if err := os.WriteFile(tp, []byte(tiers), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if gateway != "" {
		if err := os.WriteFile(gp, []byte(gateway), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return tp, gp
}

// Suite D115: a server Hermes trusts to the gateway must be reviewed there
func TestTheGatewayReviewsEveryServerHermesTrustsToIt(t *testing.T) {
	gateway := `{"listen": "127.0.0.1:8790", "servers": {
		"linear": {"url": "https://mcp.linear.app/mcp", "privacy": ["P1"], "approval": {"tools": {"get_*": "allow"}}},
		"exa": {"url": "https://mcp.exa.ai/mcp", "privacy": ["P1"], "read_only": true}
	}}`
	for _, c := range []struct {
		name, tiers, gateway string
		want                 Status
		says                 string
	}{
		{"reviewed", `{"trusted": ["linear", "exa"]}`, gateway, OK, "linear, exa"},
		{"none trusted", `{"shell": [], "trusted": []}`, "", OK, "trusts no gateway server"},
		{"from before D115", `{"shell": []}`, gateway, Warn, "from a bud before D115"},
		{"not on the gateway", `{"trusted": ["linear", "mail"]}`, gateway, Fail, "writes to mail, because the gateway should"},
		{"no gateway config", `{"trusted": ["linear"]}`, "", Fail, "can't be read"},
		{"no tiers", "", gateway, Warn, "no Hermes tiers file"},
		{"damaged tiers", `{`, gateway, Fail, "can't read"},
	} {
		tp, gp := writeTrustFiles(t, c.tiers, c.gateway)
		r := checkGatewayTrust(tp, gp)
		if r.Status != c.want || !strings.Contains(r.Detail, c.says) {
			t.Errorf("%s: got %s %q, want %s naming %q", c.name, r.Status, r.Detail, c.want, c.says)
		}
		if c.want != OK && r.Fix == "" {
			t.Errorf("%s: no fix", c.name)
		}
	}
}
