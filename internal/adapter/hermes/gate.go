package hermes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// The tier gate (Suite D10, D16, D48): Hermes runs `bud hermes-gate <tiers
// file>` as a pre_tool_call shell hook before every tool call, and the
// answer says whether the call runs alone, waits for Jason's approval in
// Hermes's own gate, or is refused. Terminal commands are tiered by the env
// fragments' shell rules, the same ones the OpenCode adapter renders, so a
// command's tier is written once for both runtimes. Every other tool, MCP
// tools among them (mcp__<server>__<tool>), is tiered by the bud block's
// tiers table

// Effects a rule may give, in rising order of strictness
const (
	Allow = "allow"
	Ask   = "ask"
	Deny  = "deny"
)

// Rule is one permission rule, as in the env fragments
type Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// Tiers is the file bud sync writes for the gate
type Tiers struct {
	// Shell are the fragments' shell rules, in order: the last that matches
	// a command wins
	Shell []Rule `json:"shell"`
	// Tools tiers every other tool by name or pattern: an exact name wins,
	// then the longest pattern that matches, then "*"
	Tools map[string]string `json:"tools"`
	// Private are the tool patterns of the servers only private sessions may
	// use (the calendar): refused to any profile but Hermes's private one,
	// whatever Tools says (Suite D95)
	Private []string `json:"private"`
}

// PrivateProfile is the name Hermes gives its default profile, the private
// one (Suite D95)
const PrivateProfile = "default"

// Payload is what Hermes sends a shell hook on stdin
type Payload struct {
	Event string         `json:"hook_event_name"`
	Tool  string         `json:"tool_name"`
	Input map[string]any `json:"tool_input"`
	// Profile is the Hermes profile the call came from
	Profile string `json:"profile"`
}

// Verdict is the gate's answer for one tool call
type Verdict struct {
	Effect string
	Reason string
}

// Reply is the JSON Hermes reads back: nothing for a call that runs alone,
// an approve directive that sends it to Hermes's human-approval gate, or a
// block. No rule_key is sent, so an approval covers the one call and never
// becomes a standing one
func (v Verdict) Reply() []byte {
	var out map[string]string
	switch v.Effect {
	case Allow:
		out = map[string]string{}
	case Ask:
		out = map[string]string{"action": "approve", "message": v.Reason}
	default:
		out = map[string]string{"action": "block", "message": v.Reason}
	}
	b, _ := json.Marshal(out) // a string map always encodes

	return append(b, '\n')
}

// Decide tiers one tool call. Anything the rules don't cover waits for
// approval, and anything malformed is refused, so a gap never runs alone
func (t *Tiers) Decide(p Payload) Verdict {
	if p.Event != "pre_tool_call" {
		return Verdict{Deny, fmt.Sprintf("bud: the gate only tiers pre_tool_call, and this call came as %q, so it was refused", p.Event)}
	}
	if p.Tool == "" {
		return Verdict{Deny, "bud: the tool call names no tool, so it was refused"}
	}
	for _, pat := range t.Private {
		if glob(pat, p.Tool) && p.Profile != PrivateProfile {
			return Verdict{Deny, fmt.Sprintf("bud: %s holds private data, which only Hermes's private profile may read, and this call came from %q (Suite D95)", p.Tool, p.Profile)}
		}
	}
	if p.Tool == "terminal" {
		cmd, _ := p.Input["command"].(string)
		if strings.TrimSpace(cmd) == "" {
			return Verdict{Deny, "bud: the terminal call has no command, so it was refused"}
		}
		return t.command(cmd)
	}
	effect, ok := t.tool(p.Tool)
	if !ok {
		return Verdict{Ask, fmt.Sprintf("bud: no tier is set for %s, so it needs your approval", p.Tool)}
	}

	return verdict(effect, p.Tool)
}

func verdict(effect, what string) Verdict {
	switch effect {
	case Allow:
		return Verdict{Allow, ""}
	case Ask:
		return Verdict{Ask, fmt.Sprintf("bud: %s needs your approval (Suite D10)", what)}
	}

	return Verdict{Deny, fmt.Sprintf("bud: %s is never allowed (Suite D10)", what)}
}

// tool finds a tool's tier: its exact name, else the longest pattern that
// matches it (the strictest of equally long ones), else "*"
func (t *Tiers) tool(name string) (string, bool) {
	if e, ok := t.Tools[name]; ok {
		return e, true
	}
	best, effect := -1, ""
	for pat, e := range t.Tools {
		if pat == "*" || !strings.ContainsAny(pat, "*?") || !glob(pat, name) {
			continue
		}
		if n := len(pat); n > best || (n == best && rank(e) > rank(effect)) {
			best, effect = n, e
		}
	}
	if best >= 0 {
		return effect, true
	}
	e, ok := t.Tools["*"]

	return e, ok
}

// shell returns the effect of the last shell rule matching a command
func (t *Tiers) shell(cmd string) (string, bool) {
	effect, found := "", false
	for _, r := range t.Shell {
		if glob(r.Resource, cmd) {
			effect, found = r.Effect, true
		}
	}

	return effect, found
}

// CommandPrefix starts the reason of every terminal call the gate sends for
// approval, and a JSON string holding the whole command line follows it.
// Hermes shows a plugin's approval with a label in place of the command, so
// the reason is the only place Jason sees what will run, and bud-signal reads
// the command back from here to match a pre-approval (Suite D42). JSON makes
// the command's end unambiguous whatever it holds; keep this in step with
// COMMAND_PREFIX in server/hermes/plugins/bud-signal/approvals.py
const CommandPrefix = "bud: run "

// askCommand is the approval reason for a terminal command line: the whole
// line, quoted, then why it asks
func askCommand(line, why string) Verdict {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep && and > readable on the card
	_ = enc.Encode(line)     // a string always encodes

	return Verdict{Ask, CommandPrefix + strings.TrimSuffix(b.String(), "\n") + "? " + why}
}

// command tiers a shell command line. Each simple command in it is tiered
// on its own and the strictest wins; anything the split can't see into
// (substitutions, redirections, subshells) needs approval at least
func (t *Tiers) command(line string) Verdict {
	parts, opaque := splitCommand(line)
	worst := Allow
	var why string
	for _, part := range parts {
		effect, ok := t.shell(part)
		if !ok {
			effect = Ask
		}
		if rank(effect) > rank(worst) {
			worst, why = effect, part
		}
	}
	if opaque && rank(worst) < rank(Ask) {
		return askCommand(line, "It uses substitution, redirection or a subshell, which bud can't tier, so it needs your approval.")
	}
	switch worst {
	case Allow:
		return Verdict{Allow, ""}
	case Ask:
		if len(parts) == 1 {
			return askCommand(line, "It needs your approval (Suite D10).")
		}
		return askCommand(line, fmt.Sprintf("Its part %q needs your approval (Suite D10).", why))
	}

	return verdict(worst, fmt.Sprintf("the command %q", why))
}

func rank(effect string) int {
	switch effect {
	case Allow:
		return 0
	case Ask:
		return 1
	}

	return 2
}

// splitCommand splits a command line at ;, &, |, && and || and new lines,
// outside quotes. opaque says the line holds something whose effect the
// parts don't show: $( ), backticks, <( ), >( ), redirections or ( )
func splitCommand(line string) (parts []string, opaque bool) {
	var cur strings.Builder
	flush := func() {
		if s := strings.Join(strings.Fields(cur.String()), " "); s != "" {
			parts = append(parts, s)
		}
		cur.Reset()
	}
	var quote rune
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
			cur.WriteRune(c)
		case quote == '"':
			switch c {
			case '"':
				quote = 0
			case '\\':
				if i+1 < len(rs) {
					cur.WriteRune(c)
					i++
					c = rs[i]
				}
			case '`':
				opaque = true
			case '$':
				if i+1 < len(rs) && rs[i+1] == '(' {
					opaque = true
				}
			}
			cur.WriteRune(c)
		case c == '\\':
			cur.WriteRune(c)
			if i+1 < len(rs) {
				i++
				cur.WriteRune(rs[i])
			}
		case c == '\'' || c == '"':
			quote = c
			cur.WriteRune(c)
		case c == ';' || c == '&' || c == '|' || c == '\n':
			flush()
		case c == '`' || c == '(' || c == ')' || c == '<' || c == '>':
			opaque = true
			cur.WriteRune(c)
		case c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			opaque = true
			cur.WriteRune(c)
		default:
			cur.WriteRune(c)
		}
	}
	if quote != 0 {
		opaque = true
	}
	flush()

	return parts, opaque
}

// glob matches s against a pattern where * is any run of characters and ?
// is one, as OpenCode's permission rules do
func glob(pattern, s string) bool {
	p, r := []rune(pattern), []rune(s)
	// the classic two-pointer match, backtracking to the last star
	pi, si, star, mark := 0, 0, -1, 0
	for si < len(r) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == r[si]):
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}

	return pi == len(p)
}
