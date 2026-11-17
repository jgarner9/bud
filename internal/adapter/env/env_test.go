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
		`{"bud": {"mcp": {"memory": {"url": "http://100.64.0.1:8791/mcp"}}}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Servers["memory"]; s.URL != "http://100.64.0.1:8791/mcp" || !s.P0() {
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
