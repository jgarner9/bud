package hermes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/argrule"
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
	// The reply omits rule_key; Hermes may still substitute a broader key.
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

// Suite D95: a private server's tools are refused outside the private
// profile, whatever their tier, and a call naming no profile is refused too
func TestPrivateToolsRunOnlyInThePrivateProfile(t *testing.T) {
	tiers := testTiers()
	tiers.Private = []string{"mcp__calendar__*"}
	call := func(tool, profile string) Payload {
		return Payload{Event: "pre_tool_call", Tool: tool, Profile: profile}
	}
	for _, c := range []struct {
		p    Payload
		want string
	}{
		{call("mcp__calendar__list_events", PrivateProfile), Allow},
		{call("mcp__calendar__create_event", PrivateProfile), Ask},
		{call("mcp__calendar__list_events", "cloud"), Deny},
		{call("mcp__calendar__list_events", ""), Deny},
		{call("mcp__linear__get_issue", "cloud"), Allow},
	} {
		v := tiers.Decide(c.p)
		if v.Effect != c.want {
			t.Errorf("%s from %q: got %s (%s), want %s", c.p.Tool, c.p.Profile, v.Effect, v.Reason, c.want)
		}
		if v.Effect == Deny && !strings.Contains(v.Reason, "private profile") {
			t.Errorf("the refusal should say why: %s", v.Reason)
		}
	}
}

// The profile comes from Hermes v0.23.2's own hook wire
// (agent/shell_hooks.py _payload_fields: tool_name, tool_input, session_id,
// cwd, profile, extra), so the private profile's calls carry "default"
func TestTheGateReadsTheProfileFromHermessWire(t *testing.T) {
	tiers := testTiers()
	tiers.Private = []string{"mcp__calendar__*"}
	for wire, want := range map[string]string{
		`{"hook_event_name": "pre_tool_call", "tool_name": "mcp__calendar__list_events", "tool_input": {}, "session_id": "s1", "cwd": "/home/you", "profile": "default", "extra": {"task_id": "t"}}`: Allow,
		`{"hook_event_name": "pre_tool_call", "tool_name": "mcp__calendar__list_events", "tool_input": {}, "session_id": "s1", "cwd": "/home/you", "profile": "cloud", "extra": {}}`:                 Deny,
	} {
		var p Payload
		if err := json.Unmarshal([]byte(wire), &p); err != nil {
			t.Fatal(err)
		}
		if v := tiers.Decide(p); v.Effect != want {
			t.Errorf("%s: got %s (%s), want %s", wire, v.Effect, v.Reason, want)
		}
	}
}

// Hermes shows a plugin's approval with a label in place of the command, so
// a terminal ask carries the whole line in its reason, as a JSON string after
// CommandPrefix, for Jason to read and bud-signal to match (Suite D42)
func TestATerminalAskCarriesTheWholeCommand(t *testing.T) {
	for line, why := range map[string]string{
		"rm -rf build":                     "It needs your approval (Suite D10).",
		"  git push origin main ":          "It needs your approval (Suite D10).",
		"go test ./... && git push origin": `Its part "git push origin" needs your approval (Suite D10).`,
		"go test ./... > out.txt":          "It uses substitution, redirection or a subshell, which bud can't tier, so it needs your approval.",
		// lines that could trip a reader: quotes, backticks, new lines, the reason's own words
		"echo \"a\\\"b\" `x` \n touch y": "",
		`git push 'bud: run "x"? ok'`:    "It needs your approval (Suite D10).",
		"printf 'héllo\u2028' | rm z":    "",
	} {
		v := testTiers().Decide(terminal(line))
		if v.Effect != Ask {
			t.Errorf("%q: got %s (%s), want ask", line, v.Effect, v.Reason)
			continue
		}
		rest, ok := strings.CutPrefix(v.Reason, CommandPrefix)
		if !ok {
			t.Errorf("%q: the reason doesn't start with %q: %s", line, CommandPrefix, v.Reason)
			continue
		}
		dec := json.NewDecoder(strings.NewReader(rest))
		var got string
		if err := dec.Decode(&got); err != nil {
			t.Errorf("%q: the reason holds no JSON string: %v (%s)", line, err, v.Reason)
			continue
		}
		if got != line {
			t.Errorf("the reason names %q, want the whole line %q", got, line)
		}
		if tail := rest[dec.InputOffset():]; !strings.HasPrefix(tail, "? ") || why != "" && tail != "? "+why {
			t.Errorf("%q: the reason ends %q, want %q", line, tail, "? "+why)
		}
	}
	// readable on the card: && and > aren't escaped
	if v := testTiers().Decide(terminal("go test ./... && git push > log")); !strings.Contains(v.Reason, `"go test ./... && git push > log"`) {
		t.Errorf("the command should read as typed: %s", v.Reason)
	}
}

// shippedSandbox renders the shipped base and server fragments' tiers, as
// a server device's sync does
func shippedSandbox(t *testing.T) *Tiers {
	t.Helper()
	var frags []string
	for _, name := range []string{"base", "server"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "content", "env", name+".jsonc"))
		if err != nil {
			t.Fatal(err)
		}
		frags = append(frags, string(b))
	}
	out, err := Render(input(t, frags...))
	if err != nil {
		t.Fatal(err)
	}
	var tiers Tiers
	if err := json.Unmarshal(out.Tiers, &tiers); err != nil {
		t.Fatal(err)
	}

	return &tiers
}

// Suite D114: Hermes's sandboxed terminal runs alone, and only commands
// that reach out of the sandbox ask, however they're wrapped
func TestTheSandboxedTerminalAsksOnlyForTheAskList(t *testing.T) {
	tiers := shippedSandbox(t)
	for cmd, want := range map[string]string{
		// sandbox-local work runs alone
		"ls -la":                             Allow,
		"cat notes.txt | grep todo":          Allow,
		"curl -s https://example.com | jq .": Allow,
		"python3 script.py > out.txt":        Allow, // a redirect alone no longer asks
		"echo $(date) `whoami`":              Allow,
		"pip install requests":               Allow,
		"git status && git commit -m x":      Allow, // commits stay in the sandbox
		// every ask-list entry
		"git push":                     Ask,
		"git push origin main":         Ask,
		"ssh archie":                   Ask,
		"ssh":                          Ask,
		"scp a host:b":                 Ask,
		"sftp host":                    Ask,
		"rsync -a . host:dir":          Ask,
		"npm publish":                  Ask,
		"npm publish --tag next":       Ask,
		"pnpm publish --access x":      Ask,
		"yarn publish --new-version 1": Ask,
		"cargo publish --dry-run":      Ask,
		"twine upload dist/*":          Ask,
		"docker push img:1":            Ask,
		"gh release create v1":         Ask,
		// the bare forms, with no arguments
		"pnpm publish":  Ask,
		"yarn publish":  Ask,
		"cargo publish": Ask,
		"docker push":   Ask,
		"gh release":    Ask,
		// wrapper options that take a value
		"sudo -u root git push":      Ask,
		"nice -n 10 ssh host":        Ask,
		"timeout -s KILL 5 ssh host": Ask,
		"env -u HOME git push":       Ask,
		"xargs -I {} scp {} host:":   Ask,
		// shell options before -c, long ones and ones that take a value
		"bash --noprofile -c 'op read x'":     Deny,
		"bash -o pipefail -c 'git push'":      Ask,
		"bash --norc --noprofile -ec 'ssh h'": Ask,
		"bash script.sh":                      Allow,
		// compound commands: the strictest part wins
		"ls; git push":           Ask,
		"go test ./... && ssh x": Ask,
		"ls | xargs scp a host:": Ask,
		// subshells and substitutions are read into
		"echo $(git push)":          Ask,
		"echo \"$(ssh host ls)\"":   Ask,
		"echo `git push`":           Ask,
		"(cd x && git push)":        Ask,
		"diff <(ssh h cat a) b":     Ask,
		"bash -c 'git push origin'": Ask,
		"sh -ec \"ssh host\"":       Ask,
		"eval git push":             Ask,
		// wrappers and assignments don't hide the command
		"env GIT_SSH=x git push": Ask,
		"FOO=1 git push":         Ask,
		"sudo -n git push":       Ask,
		"nohup ssh host &":       Ask,
		"timeout 5 ssh host":     Ask,
		"command git push":       Ask,
		// a path-qualified program is read by its base name too
		"./git push":             Ask,
		"/usr/bin/ssh host":      Ask,
		"/usr/local/bin/op read": Deny,
		// op stays refused, wherever it hides
		"op read op://vault/item/field": Deny,
		"echo $(op read x)":             Deny,
		"bash -c 'op item get'":         Deny,
		"env op whoami":                 Deny,
		// quotes and backslashes the shell removes
		`"git" push`:               Ask,
		`g''it push`:               Ask,
		`\ssh host`:                Ask,
		`bash -c "git push \"x\""`: Ask,
	} {
		if got := tiers.Decide(terminal(cmd)); got.Effect != want {
			t.Errorf("%q: got %s (%s), want %s", cmd, got.Effect, got.Reason, want)
		}
	}
	// a sh -c nested past the limit asks rather than running unread
	deep := strings.Repeat("eval ", maxDepth+1) + "ls"
	if got := tiers.Decide(terminal(deep)); got.Effect != Ask {
		t.Errorf("a deeply nested sh -c: got %s, want ask", got.Effect)
	}
}

// The coding rules' default is ask, so a path-qualified program still
// isn't the bare one: ./go test isn't go test's allow
func TestAPathQualifiedProgramIsNeverTheBareOnesAllow(t *testing.T) {
	for cmd, want := range map[string]string{
		"./go test ./...":      Ask,
		"/usr/bin/git status":  Ask,
		"env go test ./...":    Ask, // the wrapper's own match is ask
		"/opt/op/op read x":    Deny,
		"bash -c 'op read x'":  Deny,
		"sudo git push origin": Ask,
		"go test ./... | cat":  Ask,
		"FOO=1 git status":     Ask,
	} {
		if got := testTiers().Decide(terminal(cmd)); got.Effect != want {
			t.Errorf("%q: got %s (%s), want %s", cmd, got.Effect, got.Reason, want)
		}
	}
}

// Suite D116: a when rule decides over the tool's tier by its arguments,
// but never loosens a deny, and a field it can't read asks
func TestWhenRulesTierAToolByItsArguments(t *testing.T) {
	tiers := testTiers()
	tiers.When = []argrule.Rule{
		{Tool: "cronjob_manage", Effect: Allow, Args: map[string]argrule.Condition{"action": {Equals: json.RawMessage(`"list"`)}}},
		{Tool: "browser_vault", Effect: Allow},
		{Tool: "read_file", Effect: Ask, Args: map[string]argrule.Condition{"path": {Glob: ptr("/run/secrets/*")}}},
	}
	for _, c := range []struct {
		name, tool string
		input      map[string]any
		want       string
	}{
		{"list jobs", "cronjob_manage", map[string]any{"action": "list"}, Allow},
		{"create a job", "cronjob_manage", map[string]any{"action": "create", "schedule": "30m"}, Ask},
		{"no action", "cronjob_manage", nil, Ask},
		{"a deny stays", "browser_vault", nil, Deny},
		{"a forbid tightens an allow", "read_file", map[string]any{"path": "/run/secrets/op"}, Ask},
		{"elsewhere it reads", "read_file", map[string]any{"path": "/work/a.go"}, Allow},
		{"an unreadable path asks", "read_file", map[string]any{"path": []any{"/work/a.go"}}, Ask},
	} {
		if got := tiers.Decide(Payload{Event: "pre_tool_call", Tool: c.tool, Input: c.input}); got.Effect != c.want {
			t.Errorf("%s: %s, want %s", c.name, got.Effect, c.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// Suite D116: with the shipped fragments, listing cron jobs runs alone and
// every other action asks, read from the payload Hermes sends
func TestTheShippedTiersLetOnlyAJobListingRunAlone(t *testing.T) {
	tiers := shippedSandbox(t)
	for input, want := range map[string]string{
		`{"action":"list"}`:                                 Allow,
		`{"action":"list","include_disabled":true}`:         Allow,
		`{"action":"create","schedule":"30m","prompt":"x"}`: Ask,
		`{"action":"remove","job_id":"a1"}`:                 Ask,
		`{"action":" list"}`:                                Ask,
		`{}`:                                                Ask,
	} {
		var p Payload
		if err := json.Unmarshal([]byte(`{"hook_event_name":"pre_tool_call","tool_name":"cronjob_manage","tool_input":`+input+`}`), &p); err != nil {
			t.Fatal(err)
		}
		if got := tiers.Decide(p); got.Effect != want {
			t.Errorf("%s: %s, want %s", input, got.Effect, want)
		}
	}
}
