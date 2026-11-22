package argrule

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

var shipped = []Rule{
	{Tool: "create_event", Effect: Allow, Args: map[string]Condition{"attendees": {Empty: ptr(true)}}},
	{Tool: "send_message", Effect: Allow, Args: map[string]Condition{
		"to": {Equals: json.RawMessage(`["me@example.com"]`)},
		"cc": {Empty: ptr(true)},
	}},
	{Tool: "cronjob_manage", Effect: Allow, Args: map[string]Condition{"action": {Equals: json.RawMessage(`"list"`)}}},
}

func TestApplyLetsOnlyTheShapesTheRulesNameRunAlone(t *testing.T) {
	cases := []struct {
		name, tool, args string
		want             string
		ok               bool
	}{
		{"event without attendees", "create_event", `{"title":"Dentist","start":"2026-11-23T09:00"}`, Allow, true},
		{"event with empty attendees", "create_event", `{"title":"x","attendees":[]}`, Allow, true},
		{"event with null attendees", "create_event", `{"title":"x","attendees":null}`, Allow, true},
		{"event with attendees", "create_event", `{"title":"x","attendees":["sam@example.com"]}`, "", false},
		{"attendees as an object", "create_event", `{"attendees":{"sam":1}}`, Ask, true},
		{"attendees as a number", "create_event", `{"attendees":3}`, Ask, true},
		{"no arguments", "create_event", ``, Allow, true},
		{"null arguments", "create_event", `null`, Allow, true},
		{"arguments a list", "create_event", `[1]`, Ask, true},
		{"arguments not JSON", "create_event", `{"title":`, Ask, true},
		{"two values", "create_event", `{} {}`, Ask, true},
		{"mail to me", "send_message", `{"to":["me@example.com"],"subject":"s","body":"b"}`, Allow, true},
		{"mail to me with cc", "send_message", `{"to":["me@example.com"],"cc":["sam@example.com"]}`, "", false},
		{"mail to me and sam", "send_message", `{"to":["me@example.com","sam@example.com"]}`, "", false},
		{"mail to sam", "send_message", `{"to":["sam@example.com"]}`, "", false},
		{"mail to me as a string", "send_message", `{"to":"me@example.com"}`, "", false},
		{"mail to my name and address", "send_message", `{"to":["Me <me@example.com>"]}`, "", false},
		{"mail with cc a number", "send_message", `{"to":["me@example.com"],"cc":7}`, Ask, true},
		{"list jobs", "cronjob_manage", `{"action":"list"}`, Allow, true},
		{"list jobs in capitals", "cronjob_manage", `{"action":"LIST"}`, "", false},
		{"create a job", "cronjob_manage", `{"action":"create","schedule":"30m"}`, "", false},
		{"no action", "cronjob_manage", `{}`, "", false},
		{"action a list", "cronjob_manage", `{"action":["list"]}`, "", false},
		{"duplicate key, last wins as in every upstream", "cronjob_manage", `{"action":"list","action":"remove"}`, "", false},
		{"another tool", "delete_event", `{}`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Apply(shipped, c.tool, json.RawMessage(c.args))
			if got != c.want || ok != c.ok {
				t.Fatalf("Apply = %q, %v; want %q, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestAForbidBeatsAPermit(t *testing.T) {
	rules := []Rule{
		{Tool: "send_*", Effect: Allow},
		{Tool: "send_message", Effect: Ask, Args: map[string]Condition{"bcc": {Absent: ptr(false)}}},
		{Tool: "send_message", Effect: Deny, Args: map[string]Condition{"to": {Equals: json.RawMessage(`["boss@example.com"]`)}}},
		{Tool: "send_message", Effect: Deny, Args: map[string]Condition{"subject": {Glob: ptr("*payroll*")}}},
	}
	for args, want := range map[string]string{
		`{"to":["a@example.com"]}`:                Allow,
		`{"to":["a@example.com"],"bcc":["x"]}`:    Ask,
		`{"to":["boss@example.com"],"bcc":["x"]}`: Deny,
		`{"to":["boss@example.com"]}`:             Deny,
		`{"to":["a@example.com"],"bcc":null}`:     Allow,
		`{"to":["a@example.com"],"subject":5}`:    Deny, // an unreadable deny stays a deny
		`{"to":{"x":1},"bcc":["x"]}`:              Ask,  // equals only compares
		`not json`:                                Deny,
	} {
		if got, _ := Apply(rules, "send_message", json.RawMessage(args)); got != want {
			t.Errorf("%s: %q, want %q", args, got, want)
		}
	}
}

func TestEachConditionTestsWhatItSays(t *testing.T) {
	cases := []struct {
		name string
		c    Condition
		args string
		want result
	}{
		{"glob match", Condition{Glob: ptr("list_*")}, `{"f":"list_jobs"}`, yes},
		{"glob miss", Condition{Glob: ptr("list_*")}, `{"f":"remove"}`, no},
		{"glob missing", Condition{Glob: ptr("*")}, `{}`, no},
		{"glob on a number", Condition{Glob: ptr("*")}, `{"f":1}`, unreadable},
		{"glob on a list", Condition{Glob: ptr("*")}, `{"f":["a"]}`, unreadable},
		{"absent missing", Condition{Absent: ptr(true)}, `{}`, yes},
		{"absent null", Condition{Absent: ptr(true)}, `{"f":null}`, yes},
		{"absent there", Condition{Absent: ptr(true)}, `{"f":""}`, no},
		{"present there", Condition{Absent: ptr(false)}, `{"f":0}`, yes},
		{"empty string", Condition{Empty: ptr(true)}, `{"f":""}`, yes},
		{"empty false on a list", Condition{Empty: ptr(false)}, `{"f":[1]}`, yes},
		{"empty on a bool", Condition{Empty: ptr(true)}, `{"f":false}`, unreadable},
		{"equals a number by value", Condition{Equals: json.RawMessage(`1`)}, `{"f":1.0}`, yes},
		{"equals a number", Condition{Equals: json.RawMessage(`1`)}, `{"f":"1"}`, no},
		{"equals null", Condition{Equals: json.RawMessage(`null`)}, `{}`, yes},
		{"equals an object", Condition{Equals: json.RawMessage(`{"a":[1,"b"]}`)}, `{"f":{"a":[1,"b"]}}`, yes},
		{"equals an object with more", Condition{Equals: json.RawMessage(`{"a":1}`)}, `{"f":{"a":1,"b":2}}`, no},
		{"equals a list in order", Condition{Equals: json.RawMessage(`["a","b"]`)}, `{"f":["b","a"]}`, no},
		{"equals true", Condition{Equals: json.RawMessage(`true`)}, `{"f":true}`, yes},
		{"equals a large id exactly", Condition{Equals: json.RawMessage(`9007199254740992`)}, `{"f":9007199254740993}`, no},
		{"equals a large id", Condition{Equals: json.RawMessage(`9007199254740993`)}, `{"f":9007199254740993}`, yes},
		{"equals 1e2 and 100", Condition{Equals: json.RawMessage(`1e2`)}, `{"f":100}`, yes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fields map[string]any
			dec := json.NewDecoder(strings.NewReader(c.args))
			dec.UseNumber()
			if err := dec.Decode(&fields); err != nil {
				t.Fatal(err)
			}
			if got := holds(map[string]Condition{"f": c.c}, fields); got != c.want {
				t.Fatalf("holds = %v, want %v", got, c.want)
			}
		})
	}
}

func TestADottedFieldReadsNestedObjects(t *testing.T) {
	rules := []Rule{{Tool: "t", Effect: Allow, Args: map[string]Condition{"event.kind": {Equals: json.RawMessage(`"reminder"`)}}}}
	for args, want := range map[string]string{
		`{"event":{"kind":"reminder"}}`: Allow,
		`{"event":{"kind":"meeting"}}`:  "",
		`{"event":{}}`:                  "",
		`{}`:                            "",
		`{"event":"reminder"}`:          Ask, // event isn't an object
		`{"event":null}`:                Ask, // null isn't an object either
	} {
		if got, _ := Apply(rules, "t", json.RawMessage(args)); got != want {
			t.Errorf("%s: %q, want %q", args, got, want)
		}
	}
}

// Copilot on #113: a null step can't pass for an absent field
func TestANullStepIsntAnAbsentField(t *testing.T) {
	rules := []Rule{{Tool: "t", Effect: Allow, Args: map[string]Condition{"event.kind": {Absent: ptr(true)}}}}
	if got, _ := Apply(rules, "t", json.RawMessage(`{"event":null}`)); got != Ask {
		t.Fatalf("got %q, want ask", got)
	}
	if got, _ := Apply(rules, "t", json.RawMessage(`{}`)); got != Allow {
		t.Fatalf("a missing event: got %q, want allow", got)
	}
}

func TestApplyFieldsComparesHermesNumbersByValue(t *testing.T) {
	rules := []Rule{{Tool: "t", Effect: Allow, Args: map[string]Condition{"n": {Equals: json.RawMessage(`2`)}}}}
	if got, ok := ApplyFields(rules, "t", map[string]any{"n": float64(2)}); got != Allow || !ok {
		t.Fatalf("ApplyFields = %q, %v", got, ok)
	}
	if _, ok := ApplyFields(rules, "t", map[string]any{"n": float64(3)}); ok {
		t.Fatal("3 matched 2")
	}
	big := []Rule{{Tool: "t", Effect: Allow, Args: map[string]Condition{"n": {Equals: json.RawMessage(`9007199254740993`)}}}}
	if _, ok := ApplyFields(big, "t", map[string]any{"n": json.Number("9007199254740992")}); ok {
		t.Fatal("a json.Number one off matched")
	}
	if got, _ := ApplyFields(big, "t", map[string]any{"n": json.Number("9007199254740993")}); got != Allow {
		t.Fatalf("the same json.Number: %q", got)
	}
}

func TestCheckNamesEveryProblem(t *testing.T) {
	rules := []Rule{
		{Tool: "", Effect: Allow},
		{Tool: "[", Effect: "maybe"},
		{Tool: "ok", Effect: Allow, Args: map[string]Condition{
			"two":     {Glob: ptr("*"), Empty: ptr(true)},
			"none":    {},
			"badglob": {Glob: ptr("[")},
			"badjson": {Equals: json.RawMessage(`{`)},
			"a..b":    {Absent: ptr(true)},
		}},
		{Tool: "terminal", Effect: Allow},
	}
	errs := Check("w", rules, []string{Allow, Ask}, func(g string) string {
		if g == "terminal" {
			return "no"
		}
		return ""
	})
	all := strings.Join(errs, "\n")
	for _, want := range []string{
		`w[0].tool: name the tool`,
		`w[1].tool: "[" isn't a valid glob`,
		`w[1].effect: "maybe" isn't allow or ask`,
		`w[2].args.two: set exactly one`,
		`w[2].args.none: set exactly one`,
		`w[2].args.badglob.glob: "[" isn't a valid glob`,
		`w[2].args.badjson.equals: isn't a JSON value`,
		`w[2].args.a..b: a field is a name or a dotted path`,
		`w[3].tool: no`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if errs := Check("w", shipped, []string{Allow, Ask}, nil); len(errs) > 0 {
		t.Fatalf("shipped rules: %v", errs)
	}
}
