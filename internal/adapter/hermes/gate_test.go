package hermes

import (
	"strings"
	"testing"
)

func testTiers() *Tiers {
	return &Tiers{
		Shell: []Rule{
			{"shell", "*", Ask},
			{"shell", "go test", Allow},
			{"shell", "go test *", Allow},
			{"shell", "git status", Allow},
			{"shell", "git push *", Ask},
			{"shell", "op *", Deny},
		},
		Tools: map[string]string{
			"*":                     Ask,
			"read_file":             Allow,
			"browser_vault":         Deny,
			"mcp__*":                Allow,
			"mcp__calendar__*":      Ask,
			"mcp__calendar__list_*": Allow,
		},
	}
}

func terminal(cmd string) Payload {
	return Payload{Event: "pre_tool_call", Tool: "terminal", Input: map[string]any{"command": cmd}}
}

func TestTheGateTiersTerminalCommandsByTheShellRules(t *testing.T) {
	for cmd, want := range map[string]string{
		"go test ./...":                   Allow,
		"go test":                         Allow,
		"  go   test   ./... ":            Allow, // spaces fold, as a shell reads them
		"git status":                      Allow,
		"rm -rf build":                    Ask, // the broad rule
		"git push origin main":            Ask,
		"op read op://vault/item/field":   Deny,
		"go test ./... && git status":     Allow,
		"go test ./... && op item delete": Deny, // the strictest part wins
		"git status; rm x":                Ask,
		"git status | op read x":          Deny,
		"go test -run 'a|b' ./...":        Allow, // a quoted | isn't a pipe
		"go test $(op read x)":            Ask,   // a substitution needs approval at least
		"go test `whoami`":                Ask,
		"go test ./... > out.txt":         Ask,
		"go test \"$(op read x)\"":        Ask,
		"go test 'unclosed":               Ask,
		"op read x > /dev/null":           Deny, // opaque never softens a deny
	} {
		if got := testTiers().Decide(terminal(cmd)); got.Effect != want {
			t.Errorf("%q: got %s (%s), want %s", cmd, got.Effect, got.Reason, want)
		}
	}
}

func TestTheGateTiersToolsByNameThenLongestPatternThenStar(t *testing.T) {
	for tool, want := range map[string]string{
		"read_file":                  Allow,
		"browser_vault":              Deny,
		"cronjob_manage":             Ask, // "*"
		"mcp__linear__get_issue":     Allow,
		"mcp__calendar__create":      Ask,
		"mcp__calendar__list_events": Allow,
	} {
		if got := testTiers().Decide(Payload{Event: "pre_tool_call", Tool: tool}); got.Effect != want {
			t.Errorf("%s: got %s, want %s", tool, got.Effect, want)
		}
	}
	empty := &Tiers{}
	if got := empty.Decide(Payload{Event: "pre_tool_call", Tool: "read_file"}); got.Effect != Ask {
		t.Errorf("with no tiers a tool asks, got %s", got.Effect)
	}
	if got := empty.Decide(terminal("ls")); got.Effect != Ask {
		t.Errorf("with no rules a command asks, got %s", got.Effect)
	}
}

func TestTheGateRefusesWhatItCantRead(t *testing.T) {
	for _, p := range []Payload{
		{Event: "pre_tool_call"},
		{Event: "pre_tool_call", Tool: "terminal"},
		{Event: "pre_tool_call", Tool: "terminal", Input: map[string]any{"command": 7}},
		// with no event, or another one, nothing says this is a call to tier
		{Tool: "read_file"},
		{Event: "post_tool_call", Tool: "read_file"},
	} {
		if got := testTiers().Decide(p); got.Effect != Deny {
			t.Errorf("%+v: got %s", p, got.Effect)
		}
	}
}

func TestTheReplyIsWhatHermesReads(t *testing.T) {
	for v, want := range map[Verdict]string{
		{Allow, ""}:      "{}\n",
		{Ask, "why"}:     `{"action":"approve","message":"why"}` + "\n",
		{Deny, "no"}:     `{"action":"block","message":"no"}` + "\n",
		{"other", "odd"}: `{"action":"block","message":"odd"}` + "\n",
	} {
		if got := string(v.Reply()); got != want {
			t.Errorf("%+v: got %s, want %s", v, got, want)
		}
	}
	// no rule_key, so an approval never becomes a standing one
	if strings.Contains(string(Verdict{Ask, "x"}.Reply()), "rule_key") {
		t.Error("the reply carries a rule_key")
	}
}

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		pat, s string
		want   bool
	}{
		{"*", "", true},
		{"go test *", "go test ./...", true},
		{"go test *", "go test", false},
		{"docker * --privileged*", "docker run --privileged -it x", true},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"*.env", "x/.env", true},
		{"a*b*c", "aXbYbZc", true},
		{"a*b*c", "aXbY", false},
	} {
		if got := glob(c.pat, c.s); got != c.want {
			t.Errorf("glob(%q, %q) = %v", c.pat, c.s, got)
		}
	}
}
