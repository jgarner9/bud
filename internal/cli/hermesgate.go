package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"jgarner.dev/bud/internal/adapter/hermes"
)

func init() {
	RegisterCommand(HermesGate())
}

// HermesGate is the tier gate Hermes's pre_tool_call hook runs before every
// tool call (Suite D10, D48); see docs/internals/sync.md
func HermesGate() *Command {
	return &Command{
		Name:  "hermes-gate",
		Usage: "Tiers one Hermes tool call (run by Hermes's pre_tool_call hook): reads the call on stdin, answers allow, approve or block",
		Run: func(g *Globals, args []string) error {
			if len(args) != 1 {
				return usageErrorf("hermes-gate takes the tiers file bud sync writes, ~/.local/state/bud/hermes-tiers.json")
			}
			var w io.Writer = os.Stdout
			if g.stdout != nil {
				w = g.stdout
			}
			return runHermesGate(gateStdin, w, args[0])
		},
	}
}

// gateStdin is swappable for tests
var gateStdin io.Reader = os.Stdin

// runHermesGate always answers on stdout when it can, refusing the call when
// the tiers or the call can't be read; an error makes Hermes block the call
// too, since the hook is fail_closed
func runHermesGate(in io.Reader, out io.Writer, tiersPath string) error {
	reply := func(v hermes.Verdict) error {
		_, err := out.Write(v.Reply())
		return err
	}
	b, err := os.ReadFile(tiersPath)
	if err != nil {
		return reply(hermes.Verdict{Effect: hermes.Deny, Reason: fmt.Sprintf("bud: can't read the tiers (%v); run bud sync", err)})
	}
	var t hermes.Tiers
	if err := json.Unmarshal(b, &t); err != nil {
		return reply(hermes.Verdict{Effect: hermes.Deny, Reason: fmt.Sprintf("bud: %s is damaged (%v); run bud sync", tiersPath, err)})
	}
	var p hermes.Payload
	if err := json.NewDecoder(io.LimitReader(in, 16<<20)).Decode(&p); err != nil {
		return reply(hermes.Verdict{Effect: hermes.Deny, Reason: fmt.Sprintf("bud: can't read the tool call (%v)", err)})
	}

	return reply(t.Decide(p))
}
