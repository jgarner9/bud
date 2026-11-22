package hermes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
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
	// Trusted are the servers Hermes reaches through the MCP gateway without
	// its own untrusted-server prompt, since the gateway asks at the leaf
	// (Suite D115). The gate doesn't read it; bud doctor checks each against
	// the gateway's config
	Trusted []string `json:"trusted"`
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
// block. No rule_key is sent; Hermes may substitute the tool name and allow
// broader standing approvals. This hook does not enforce one-call scope.
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
// on its own and the strictest wins. Under rules whose default is ask (the
// coding rules), anything the split can't see into (substitutions,
// redirections, subshells) needs approval at least. Under rules whose
// default is allow (Hermes's sandbox, Suite D114) it asks only when a
// command inside it does, so the line is split again into every command it
// could run
func (t *Tiers) command(line string) Verdict {
	worst, why, opaque := t.lineEffect(line, 0)
	if opaque && t.defaultShell() != Allow && rank(worst) < rank(Ask) {
		return askCommand(line, "It uses substitution, redirection or a subshell, which bud can't tier, so it needs your approval.")
	}
	switch worst {
	case Allow:
		return Verdict{Allow, ""}
	case Ask:
		if why == strings.Join(strings.Fields(line), " ") {
			return askCommand(line, "It needs your approval (Suite D10).")
		}
		return askCommand(line, fmt.Sprintf("Its part %q needs your approval (Suite D10).", why))
	}

	return verdict(worst, fmt.Sprintf("the command %q", why))
}

// maxDepth bounds how many sh -c and eval layers the gate unwraps; a deeper
// one asks
const maxDepth = 4

// lineEffect returns the strictest effect of the commands in a line, the
// part that gave it, and whether the line holds anything opaque
func (t *Tiers) lineEffect(line string, depth int) (worst, why string, opaque bool) {
	parts, opaque := splitCommand(line, false)
	if opaque && t.defaultShell() == Allow {
		deep, _ := splitCommand(line, true)
		parts = append(parts, deep...)
	}
	worst = Allow
	for _, part := range parts {
		effect := t.partEffect(part, depth)
		if rank(effect) > rank(worst) {
			worst, why = effect, part
		}
	}

	return worst, why, opaque
}

// partEffect tiers one simple command: the strictest of the command as
// written and as it runs once wrappers are taken off (variable assignments,
// env, command, exec, nohup, time, sudo, xargs, nice, timeout, stdbuf), with
// a path-qualified program (./git, /usr/bin/git) also read by its base name,
// and the words also read with their quotes removed. A path never dodges a
// rule for the bare name, and is never matched as the bare one for an
// allow, since the strictest wins. A wrapper's options can take values
// (sudo -u root, nice -n 10), which can't be told from the command without
// knowing each wrapper, so every word after a wrapper is tried as the start
// of the command it runs. sh -c and eval run their argument as a line of its
// own, tiered the same way
func (t *Tiers) partEffect(part string, depth int) string {
	worst := Allow
	raise := func(e string) {
		if rank(e) > rank(worst) {
			worst = e
		}
	}
	match := func(cmd string) {
		e, ok := t.shell(cmd)
		if !ok {
			e = Ask
		}
		raise(e)
	}
	words := strings.Fields(part)
	if plain := dequote(words); !slices.Equal(plain, words) {
		// as the shell reads it once quotes are removed: "g"it, \git
		raise(t.partEffect(strings.Join(plain, " "), depth))
	}
	// each start is where a command may begin: 0, and every word after an
	// assignment or a wrapper; seen keeps the work linear in the words
	seen := make([]bool, len(words))
	starts := []int{0}
	for len(starts) > 0 {
		i := starts[0]
		starts = starts[1:]
		if i >= len(words) || seen[i] {
			continue
		}
		seen[i] = true
		cmd := words[i:]
		match(strings.Join(cmd, " "))
		prog := cmd[0]
		if strings.Contains(prog, "/") {
			prog = path.Base(prog)
			match(strings.Join(append([]string{prog}, cmd[1:]...), " "))
		}
		if script, ok := inlineScript(prog, cmd[1:]); ok {
			if depth >= maxDepth {
				raise(Ask)
			} else {
				e, _, _ := t.lineEffect(unquote(script), depth+1)
				raise(e)
			}
		}
		switch {
		case assignment.MatchString(cmd[0]):
			starts = append(starts, i+1)
		case slices.Contains(wrappers, prog):
			for j := i + 1; j < len(words); j++ {
				starts = append(starts, j)
			}
		}
	}

	return worst
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// wrappers run a command given after their own options
var wrappers = []string{"env", "command", "exec", "nohup", "time", "sudo", "xargs", "nice", "timeout", "stdbuf"}

var shells = []string{"sh", "bash", "zsh", "dash", "ksh"}

// shortC is a cluster of short options holding c, such as -c, -ec or -lc
var shortC = regexp.MustCompile(`^-[A-Za-z]*c[A-Za-z]*$`)

// inlineScript finds the line a shell's -c or eval runs. A shell's other
// options, long ones and ones that take a value (-o pipefail) among them,
// may come before -c, so any short-option cluster holding c counts, wherever
// it is; reading one too many as -c only ever adds a check
func inlineScript(prog string, args []string) (string, bool) {
	if prog == "eval" {
		return strings.Join(args, " "), len(args) > 0
	}
	if !slices.Contains(shells, prog) {
		return "", false
	}
	for i, a := range args {
		if shortC.MatchString(a) && i+1 < len(args) {
			return strings.Join(args[i+1:], " "), true
		}
	}

	return "", false
}

// dequote removes quotes and backslashes from each word, the shell's own
// quote removal, roughly
func dequote(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strings.NewReplacer(`\`, "", `"`, "", "'", "").Replace(w)
	}

	return out
}

// unquote takes one pair of matching outer quotes off s
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}

	return s
}

// defaultShell is the effect the shell rules give a command no other rule
// names: that of the last "*" rule, ask without one
func (t *Tiers) defaultShell() string {
	effect := Ask
	for _, r := range t.Shell {
		if r.Resource == "*" {
			effect = r.Effect
		}
	}

	return effect
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
// parts don't show: $( ), backticks, <( ), >( ), redirections or ( ). deep
// splits at those too, inside double quotes as well, so every command the
// line could run is a part of its own
func splitCommand(line string, deep bool) (parts []string, opaque bool) {
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
			case '`', ')':
				if c == '`' {
					opaque = true
				}
				if deep {
					flush()
					continue
				}
			case '$':
				if i+1 < len(rs) && rs[i+1] == '(' {
					opaque = true
					if deep {
						flush()
						i++
						continue
					}
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
			if deep {
				flush()
				continue
			}
			cur.WriteRune(c)
		case c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			opaque = true
			if deep {
				flush()
				i++
				continue
			}
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
