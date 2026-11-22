package env

import (
	"strings"
	"testing"
)

func resolve(t *testing.T, srcs ...string) (*Resolved, error) {
	t.Helper()
	var frags []Fragment
	for i, s := range srcs {
		frags = append(frags, Fragment{Name: "f" + string(rune('0'+i)), Src: []byte(s)})
	}
	merged, err := Merge(frags)
	if err != nil {
		t.Fatal(err)
	}

	return Take(merged)
}

func TestAGatewayServerGetsItsPathAndTheDevicesTier(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"privacy": "P1", "mcp": {"linear": {"gateway": true, "privacy": ["P1", "P2"]}}}}`,
		`{"bud": {"gateway": "http://100.64.0.1:8790/"}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Servers["linear"]
	if s.URL != "http://100.64.0.1:8790/linear/mcp" || s.Headers[PrivacyHeader] != "P1" {
		t.Fatalf("got %+v", s)
	}
}

func TestWithoutATierTheHeaderIsLeftOffSoTheGatewayTakesP0(t *testing.T) {
	r, err := resolve(t, `{"bud": {"gateway": "http://127.0.0.1:8790", "mcp": {"exa": {"gateway": true, "privacy": ["P1"]}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Servers["exa"].Headers[PrivacyHeader]; ok {
		t.Fatalf("got %+v", r.Servers["exa"])
	}
}

func TestServersMissingTheirAddressAreLeftOutAndNamed(t *testing.T) {
	r, err := resolve(t, `{"bud": {"mcp": {
		"linear": {"gateway": true, "privacy": ["P1"]},
		"memory": {"privacy": ["P0"]},
		"off": {"url": "https://example.com/mcp", "privacy": ["P2"], "disabled": true}
	}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Servers) != 0 {
		t.Fatalf("servers = %v", r.Servers)
	}
	notes := strings.Join(r.Notes(), "\n")
	for _, want := range []string{"linear is left out: it goes through the MCP gateway", "memory is left out: no fragment sets its url", "off is left out: disabled"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
}

func TestALaterFragmentFillsInAServer(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"mcp": {"memory": {"privacy": ["P0", "P1"]}}}}`,
		`{"bud": {"mcp": {"memory": {"url": "http://100.64.0.1:8798/mcp"}}}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Servers["memory"]; s.URL != "http://100.64.0.1:8798/mcp" || !s.P0() {
		t.Fatalf("got %+v", s)
	}
}

func TestTheGatewayMustBeALoopbackOrTailnetIP(t *testing.T) {
	for raw, want := range map[string]string{
		"http://gateway.example.ts.net:8790": "use the tailnet IP",
		"http://192.168.1.2:8790":            "isn't a loopback or tailnet address",
		"http://100.64.0.1":                  "give the gateway's port",
		"http://100.64.0.1:8790/mcp":         "bud adds /<server>/mcp",
		"ftp://100.64.0.1:8790":              "isn't an http(s) URL",
	} {
		_, err := resolve(t, `{"bud": {"gateway": "`+raw+`"}}`)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", raw, err, want)
		}
	}
	for _, ok := range []string{"http://127.0.0.1:8790", "http://[fd7a:115c:a1e0::1]:8790", "https://100.100.1.1:443"} {
		if _, err := resolve(t, `{"bud": {"gateway": "`+ok+`"}}`); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestTheRouterMustBeALoopbackOrTailnetIPWithNoPath(t *testing.T) {
	for raw, want := range map[string]string{
		"http://archie.example.ts.net:8795": "the router answers only its IP address",
		"http://192.168.1.2:8795":           "the router listens on the tailnet only",
		"http://100.64.0.1":                 "give the router's port, such as :8795",
		"http://100.64.0.1:8795/v1":         "bud adds /v1",
		"http://100.64.0.1:8795#x":          "bud adds /v1",
	} {
		_, err := resolve(t, `{"bud": {"router": "`+raw+`"}}`)
		if err == nil || !strings.Contains(err.Error(), "bud.router") || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", raw, err, want)
		}
	}
	r, err := resolve(t, `{"bud": {"router": "http://100.64.0.1:8795/"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Router != "http://100.64.0.1:8795" {
		t.Errorf("Router = %q, want it without the trailing slash", r.Router)
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	_, err := resolve(t, `{"bud": {"privacy": "P3", "mcp": {
		"Bad": {"privacy": ["P1"]},
		"two": {"url": "https://example.com/mcp", "command": ["x"], "privacy": ["P1"]},
		"none": {"command": ["x"]},
		"keyed": {"gateway": true, "headers": {"Authorization": "x"}, "privacy": ["P1"]}
	}}}`)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"bud.privacy", "bud.mcp.Bad", "bud.mcp.two: set one of", "bud.mcp.none.privacy", "bud.mcp.keyed: a gateway server takes no headers"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestAnUnknownKeyInTheBudBlockIsAnError(t *testing.T) {
	if _, err := resolve(t, `{"bud": {"gatway": "http://127.0.0.1:8790"}}`); err == nil || !strings.Contains(err.Error(), "gatway") {
		t.Fatalf("got %v", err)
	}
}

func TestTakeRemovesTheBudBlock(t *testing.T) {
	merged, err := Merge([]Fragment{{Name: "a", Src: []byte(`{"formatter": true, "bud": {}}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Take(merged); err != nil {
		t.Fatal(err)
	}
	if _, ok := merged["bud"]; ok || merged["formatter"] != true {
		t.Fatalf("merged = %v", merged)
	}
}

// An overlay narrows a server's tiers or swaps its command: those lists are
// replaced, not appended to, so P0 can be taken away
func TestALaterFragmentReplacesAServersTiersAndCommand(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"mcp": {"memory": {"command": ["old", "--a"], "privacy": ["P0", "P1", "P2"]}}}}`,
		`{"bud": {"mcp": {"memory": {"command": ["new"], "privacy": ["P1", "P2"]}}}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Servers["memory"]
	if s.P0() || len(s.Privacy) != 2 || strings.Join(s.Command, " ") != "new" {
		t.Fatalf("got %+v", s)
	}
}

func TestAURLMayHoldEnvReferences(t *testing.T) {
	for _, u := range []string{"{env:MCP_URL}", "https://{env:MCP_HOST}/mcp", "https://example.com/{env:PATH_PART}"} {
		if _, err := resolve(t, `{"bud": {"mcp": {"x": {"url": "`+u+`", "privacy": ["P1"]}}}}`); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"ftp://{env:H}/mcp", "{env:A}{env:B}", "not a url"} {
		if _, err := resolve(t, `{"bud": {"mcp": {"x": {"url": "`+u+`", "privacy": ["P1"]}}}}`); err == nil {
			t.Errorf("%s: want an error", u)
		}
	}
}

func TestTiersTakeKnownEffectsAndToolNamesOrPatterns(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"tiers": {"*": "ask", "read_file": "allow"}}}`,
		`{"bud": {"tiers": {"read_file": "deny", "mcp__calendar__*": "ask"}}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tiers["read_file"] != "deny" || r.Tiers["*"] != "ask" || r.Tiers["mcp__calendar__*"] != "ask" {
		t.Errorf("a later fragment sets a tool's tier over an earlier one's: %v", r.Tiers)
	}
	_, err = resolve(t, `{"bud": {"tiers": {"Read File": "allow", "x": "sometimes"}}}`)
	if err == nil || !strings.Contains(err.Error(), "bud.tiers.Read File") || !strings.Contains(err.Error(), `"sometimes" isn't allow, ask or deny`) {
		t.Fatalf("got %v", err)
	}
}

func TestHermesOnlyServersRequirePrivateLoopback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		server Server
		valid  bool
	}{
		{"literal loopback", Server{HermesOnly: true, URL: "http://127.0.0.1:8794/mcp", Privacy: []string{"P0"}}, true},
		{"not yet configured", Server{HermesOnly: true, Privacy: []string{"P0"}}, true},
		{"gateway", Server{HermesOnly: true, Gateway: true, Privacy: []string{"P0"}}, false},
		{"external URL", Server{HermesOnly: true, URL: "https://example.invalid/mcp", Privacy: []string{"P0"}}, false},
		{"substituted host", Server{HermesOnly: true, URL: "http://{env:HOST}:8794/mcp", Privacy: []string{"P0"}}, false},
		{"cloud tier", Server{HermesOnly: true, URL: "http://127.0.0.1:8794/mcp", Privacy: []string{"P0", "P1"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Bud{MCP: map[string]Server{"contacts": tc.server}}
			if _, err := b.Resolve(); (err == nil) != tc.valid {
				t.Errorf("Resolve = %v, want validity %t", err, tc.valid)
			}
		})
	}
}

func TestGatewayApprovalsStripThePrefixAndRenderEveryGatewayServer(t *testing.T) {
	r, err := resolve(t, `{"bud": {"mcp": {
		"linear": {"gateway": true, "privacy": ["P1"], "approval": {"tools": {"linear_get_*": "allow", "linear_save_issue": "ask"}}},
		"exa": {"gateway": true, "privacy": ["P1"], "read_only": true},
		"off": {"gateway": true, "privacy": ["P1"], "disabled": true},
		"context7": {"url": "https://example.com/mcp", "privacy": ["P1"]}
	}}}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.GatewayApprovals()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "servers": {
    "exa": {
      "read_only": true
    },
    "linear": {
      "tools": {
        "get_*": "allow",
        "save_issue": "ask"
      }
    }
  }
}
`
	if string(got) != want {
		t.Fatalf("got\n%s", got)
	}
	none, _ := resolve(t, `{"bud": {"mcp": {}}}`)
	if b, _ := none.GatewayApprovals(); b != nil {
		t.Errorf("no gateway servers rendered %s", b)
	}
}

func TestBadApprovalTiersAreReported(t *testing.T) {
	for name, src := range map[string]string{
		"no prefix":      `{"linear": {"gateway": true, "privacy": ["P1"], "approval": {"tools": {"get_*": "allow"}}}}`,
		"deny":           `{"linear": {"gateway": true, "privacy": ["P1"], "approval": {"tools": {"linear_x": "deny"}}}}`,
		"not on gateway": `{"linear": {"url": "https://example.com/mcp", "privacy": ["P1"], "approval": {"tools": {"linear_x": "ask"}}}}`,
		"both":           `{"linear": {"gateway": true, "privacy": ["P1"], "read_only": true, "approval": {"tools": {}}}}`,
	} {
		if _, err := resolve(t, `{"bud": {"mcp": `+src+`}}`); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestALaterFragmentReplacesAServersApprovalTiers(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"mcp": {"linear": {"gateway": true, "privacy": ["P1"], "approval": {"tools": {"linear_get_*": "allow"}}}}}}`,
		`{"bud": {"mcp": {"linear": {"approval": {"tools": {"linear_list_*": "allow"}}}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if tools := r.Gateway["linear"].Tools; tools == nil || len(*tools) != 1 || (*tools)["list_*"] != "allow" {
		t.Errorf("got %v", tools)
	}
}

func TestAnEmptyApprovalBlockRendersAsAskEverything(t *testing.T) {
	r, err := resolve(t, `{"bud": {"mcp": {"linear": {"gateway": true, "privacy": ["P1"], "approval": {"tools": {}}}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.GatewayApprovals()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"tools": {}`) {
		t.Errorf("rendered %s, want linear's empty tools kept", b)
	}
}

func TestOpenCodeSecretsTakeVariableNamesAndOpReferences(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"opencode_secrets": {"EXA_API_KEY": "op://vault/exa/old", "GITHUB_PERSONAL_ACCESS_TOKEN": "op://vault/github/pat"}}}`,
		`{"bud": {"opencode_secrets": {"EXA_API_KEY": "op://vault/exa/key"}}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if r.OpenCodeSecrets["EXA_API_KEY"] != "op://vault/exa/key" || r.OpenCodeSecrets["GITHUB_PERSONAL_ACCESS_TOKEN"] != "op://vault/github/pat" {
		t.Errorf("a later fragment sets a variable over an earlier one's: %v", r.OpenCodeSecrets)
	}
	_, err = resolve(t, `{"bud": {"opencode_secrets": {"exa key": "op://vault/exa/key", "EXA_API_KEY": "sk-plain", "GH_PAT": "op://vault/item"}}}`)
	if err == nil || !strings.Contains(err.Error(), "bud.opencode_secrets.exa key") || !strings.Contains(err.Error(), "bud.opencode_secrets.EXA_API_KEY: the value must be an op://vault/item/field reference") || !strings.Contains(err.Error(), "bud.opencode_secrets.GH_PAT") {
		t.Fatalf("got %v", err)
	}
}

// Suite D116: when rules render for the gateway with the prefix stripped,
// and an overlay that sets only when rules adds to the earlier ones and
// keeps the tools
func TestWhenRulesStripThePrefixAndAnOverlayAddsToThem(t *testing.T) {
	r, err := resolve(t,
		`{"bud": {"mcp": {"mail": {"gateway": true, "privacy": ["P0"], "approval": {
			"tools": {"mail_read_*": "allow", "mail_send_*": "ask"},
			"when": [{"tool": "mail_send_*", "effect": "ask", "args": {"bcc": {"absent": false}}}]
		}}}}}`,
		`{"bud": {"mcp": {"mail": {"approval": {
			"when": [{"tool": "mail_send_message", "effect": "allow", "args": {"to": {"equals": ["me@example.com"]}, "cc": {"empty": true}}}]
		}}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.GatewayApprovals()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"read_*": "allow"`, `"send_*": "ask"`, `"tool": "send_*"`, `"tool": "send_message"`, `"me@example.com"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in\n%s", want, b)
		}
	}
	if w := r.Gateway["mail"].When; len(w) != 2 || w[0].Effect != "ask" || w[1].Effect != "allow" {
		t.Errorf("when = %+v, want the base rule then the overlay's", w)
	}
}

func TestBadWhenRulesAreReported(t *testing.T) {
	for name, c := range map[string]struct{ src, want string }{
		"no prefix":      {`{"mcp": {"mail": {"gateway": true, "privacy": ["P0"], "approval": {"tools": {}, "when": [{"tool": "send_*", "effect": "allow"}]}}}}`, `bud.mcp.mail.approval.when[0].tool: "send_*" must start with mail_`},
		"deny":           {`{"mcp": {"mail": {"gateway": true, "privacy": ["P0"], "approval": {"tools": {}, "when": [{"tool": "mail_x", "effect": "deny"}]}}}}`, `when[0].effect: "deny" isn't allow or ask`},
		"two tests":      {`{"mcp": {"mail": {"gateway": true, "privacy": ["P0"], "approval": {"tools": {}, "when": [{"tool": "mail_x", "effect": "allow", "args": {"to": {"absent": true, "empty": true}}}]}}}}`, `set exactly one`},
		"tiers terminal": {`{"tiers_when": [{"tool": "terminal", "effect": "allow"}]}`, `bud.tiers_when[0].tool: the terminal is tiered`},
		"tiers effect":   {`{"tiers_when": [{"tool": "cronjob_manage", "effect": "maybe"}]}`, `bud.tiers_when[0].effect: "maybe" isn't allow or ask or deny`},
	} {
		_, err := resolve(t, `{"bud": `+c.src+`}`)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

// JGA-389, Copilot on #116: only the gateway checks a marked server's tier
func TestMarksP0NeedsP0AndTheGateway(t *testing.T) {
	all := []string{"P0", "P1", "P2"}
	for _, tc := range []struct {
		name   string
		server Server
		valid  bool
	}{
		{"gateway", Server{MarksP0: true, Gateway: true, Privacy: all}, true},
		{"not yet configured", Server{MarksP0: true, Privacy: all}, true},
		{"no P0", Server{MarksP0: true, Gateway: true, Privacy: []string{"P1"}}, false},
		{"direct URL", Server{MarksP0: true, URL: "http://127.0.0.1:8798/mcp", Privacy: all}, false},
		{"stdio", Server{MarksP0: true, Command: []string{"bud", "memory-mcp"}, Privacy: all}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Bud{Gateway: "http://127.0.0.1:8790", MCP: map[string]Server{"memory": tc.server}}
			if _, err := b.Resolve(); (err == nil) != tc.valid {
				t.Errorf("Resolve = %v, want validity %t", err, tc.valid)
			}
		})
	}
}
