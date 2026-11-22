package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesGateAnswersOnStdout(t *testing.T) {
	tiers := filepath.Join(t.TempDir(), "tiers.json")
	if err := os.WriteFile(tiers, []byte(`{"shell": [{"action": "shell", "resource": "*", "effect": "ask"}, {"action": "shell", "resource": "go test *", "effect": "allow"}], "tools": {"*": "ask", "browser_vault": "deny"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		`{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": {"command": "go test ./..."}}`: "{}\n",
		`{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": {"command": "rm -rf x"}}`:      `"action":"approve"`,
		`{"hook_event_name": "pre_tool_call", "tool_name": "browser_vault", "tool_input": {}}`:                      `"action":"block"`,
		`not json`: `"action":"block"`,
		// a second call after the first can't ride on its verdict
		`{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": {"command": "go test ./..."}}{"tool_name": "terminal", "tool_input": {"command": "op read x"}}`: `"action":"block"`,
		`{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": {"command": "go test ./..."}}` + "\n":                                                           "{}\n",
	} {
		var out bytes.Buffer
		if err := runHermesGate(strings.NewReader(in), &out, tiers); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("%s: got %s, want %s", in, out.String(), want)
		}
	}
	// no tiers file refuses every call, saying what to run
	var out bytes.Buffer
	if err := runHermesGate(strings.NewReader(`{"tool_name": "read_file"}`), &out, filepath.Join(t.TempDir(), "none.json")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"action":"block"`) || !strings.Contains(out.String(), "run bud sync") {
		t.Errorf("got %s", out.String())
	}
}

func TestHermesGateTakesOneArgument(t *testing.T) {
	cmd := HermesGate()
	if err := cmd.Run(&Globals{}, nil); err == nil {
		t.Error("no argument was accepted")
	}
}

// Copilot on #113: the gate keeps a tool call's numbers as written, so an
// argument rule's equals tells two large ids apart
func TestHermesGateComparesLargeNumbersExactly(t *testing.T) {
	tiers := filepath.Join(t.TempDir(), "tiers.json")
	if err := os.WriteFile(tiers, []byte(`{"tools": {"*": "ask"}, "when": [{"tool": "job_run", "effect": "allow", "args": {"id": {"equals": 9007199254740993}}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"9007199254740993": "{}\n", "9007199254740992": `"action":"approve"`} {
		var out bytes.Buffer
		if err := runHermesGate(strings.NewReader(`{"hook_event_name": "pre_tool_call", "tool_name": "job_run", "tool_input": {"id": `+id+`}}`), &out, tiers); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("id %s: got %s, want %s", id, out.String(), want)
		}
	}
}
