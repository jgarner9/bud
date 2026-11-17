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
