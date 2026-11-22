// Package argrule matches a tool call's arguments against "when" rules
// (Suite D116): an approval block or Hermes's tiers can let a call run
// alone, or make it ask, by what its arguments say, not only by the tool's
// name. Calendar events with no attendees, mail to Jason's own address and
// listing cron jobs run alone this way, while the same tools otherwise ask.
//
// The matcher is small on purpose (the design's section 3 weighed OPA and
// Cedar): a rule names a tool glob, an effect and conditions on argument
// fields, and every condition must hold for the rule to apply. Of the rules
// that apply, the strictest effect wins, so a forbid (ask or deny) always
// beats a permit (allow). A field the matcher can't read, or arguments that
// aren't a JSON object, make the call ask (or deny, for a deny rule), so a
// shape nobody expected never runs alone.
package argrule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"path"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Effects, in rising order of strictness
const (
	Allow = "allow"
	Ask   = "ask"
	Deny  = "deny"
)

// Rank orders effects by strictness; anything unknown ranks as deny
func Rank(effect string) int {
	switch effect {
	case Allow:
		return 0
	case Ask:
		return 1
	}

	return 2
}

// Rule is one "when" entry
type Rule struct {
	// Tool is a tool-name glob (path.Match syntax)
	Tool string `json:"tool"`
	// Effect is what the call gets when every condition holds
	Effect string `json:"effect"`
	// Args holds one condition per argument field. A field is a name, or a
	// dotted path into nested objects ("event.title"). No conditions means
	// the rule applies to every call of the tool
	Args map[string]Condition `json:"args,omitempty"`
}

// Condition tests one field. Set exactly one of its four tests
type Condition struct {
	// Equals holds when the field is this JSON value exactly: same type,
	// and for a list, the same items in the same order
	Equals json.RawMessage `json:"equals,omitempty"`
	// Glob holds when the field is a string matching this glob. A field
	// that's there but isn't a string can't be read
	Glob *string `json:"glob,omitempty"`
	// Absent true holds when the field is missing or null; false when it's
	// there and not null
	Absent *bool `json:"absent,omitempty"`
	// Empty true holds when the field is missing, null, an empty list or
	// an empty string; false when it's a list or string with something in
	// it. Any other type can't be read
	Empty *bool `json:"empty,omitempty"`
}

// Check reports every problem in rules, each naming the field to fix. at
// prefixes each message (such as "bud.tiers_when"), effects are the effects
// the caller allows, and tool checks a rule's tool glob, returning "" when
// it's fine
func Check(at string, rules []Rule, effects []string, tool func(string) string) []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	for i, r := range rules {
		here := fmt.Sprintf("%s[%d]", at, i)
		if r.Tool == "" {
			add("%s.tool: name the tool or a glob, such as \"calendar_create_*\"", here)
		} else if _, err := path.Match(r.Tool, ""); err != nil {
			add("%s.tool: %q isn't a valid glob", here, r.Tool)
		} else if tool != nil {
			if msg := tool(r.Tool); msg != "" {
				add("%s.tool: %s", here, msg)
			}
		}
		if !slices.Contains(effects, r.Effect) {
			add("%s.effect: %q isn't %s", here, r.Effect, strings.Join(effects, " or "))
		}
		for _, f := range sortedKeys(r.Args) {
			c := r.Args[f]
			if f == "" || strings.HasPrefix(f, ".") || strings.HasSuffix(f, ".") || strings.Contains(f, "..") {
				add("%s.args.%s: a field is a name or a dotted path, such as \"to\" or \"event.title\"", here, f)
			}
			n := 0
			if c.Equals != nil {
				n++
				if !json.Valid(c.Equals) {
					add("%s.args.%s.equals: isn't a JSON value", here, f)
				}
			}
			if c.Glob != nil {
				n++
				if _, err := path.Match(*c.Glob, ""); err != nil {
					add("%s.args.%s.glob: %q isn't a valid glob", here, f, *c.Glob)
				}
			}
			if c.Absent != nil {
				n++
			}
			if c.Empty != nil {
				n++
			}
			if n != 1 {
				add("%s.args.%s: set exactly one of equals, glob, absent and empty", here, f)
			}
		}
	}

	return errs
}

// Apply finds the effect the rules give one call of tool with the given
// arguments (the raw JSON a client sent; nil or null is no arguments). ok
// is false when no rule names the tool, and the caller keeps its own tier.
// Otherwise effect is the strictest of the rules whose conditions hold, at
// least ask when a rule's field can't be read, and ok is false when rules name the
// tool but none applies
func Apply(rules []Rule, tool string, args json.RawMessage) (effect string, ok bool) {
	var fields map[string]any
	readable := true
	if t := bytes.TrimSpace(args); len(t) > 0 && !bytes.Equal(t, []byte("null")) {
		dec := json.NewDecoder(bytes.NewReader(t))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil || dec.More() {
			readable = false
		} else if fields, readable = v.(map[string]any); !readable {
			fields = nil
		}
	}

	return apply(rules, tool, fields, readable)
}

// ApplyFields is Apply for arguments already decoded into a map, as Hermes
// hands its shell hook. Decode them with UseNumber: a float64 has already
// lost the digits past 2^53, so equals compares it as the rounded value
func ApplyFields(rules []Rule, tool string, fields map[string]any) (effect string, ok bool) {
	return apply(rules, tool, fields, true)
}

func apply(rules []Rule, tool string, fields map[string]any, readable bool) (string, bool) {
	effect, ok := "", false
	for _, r := range rules {
		if m, _ := path.Match(r.Tool, tool); !m {
			continue
		}
		got := r.Effect
		if readable {
			switch holds(r.Args, fields) {
			case no:
				continue
			case unreadable:
				got = stricter(r.Effect, Ask)
			}
		} else {
			got = stricter(r.Effect, Ask)
		}
		if !ok || Rank(got) > Rank(effect) {
			effect, ok = got, true
		}
	}

	return effect, ok
}

type result int

const (
	no result = iota
	yes
	unreadable
)

// holds tests every condition; an unreadable field outranks a plain miss,
// so a rule that can't be read always asks
func holds(conds map[string]Condition, fields map[string]any) result {
	out := yes
	for _, f := range sortedKeys(conds) {
		v, present, ok := lookup(fields, f)
		if !ok {
			return unreadable
		}
		switch test(conds[f], v, present) {
		case unreadable:
			return unreadable
		case no:
			out = no
		}
	}

	return out
}

// lookup follows a dotted path. A step that's missing makes the field
// absent; ok is false when a step on the way is there but isn't an object,
// null included, so {"event": null} can't pass for an absent event.kind
func lookup(fields map[string]any, field string) (v any, present, ok bool) {
	cur := fields
	parts := strings.Split(field, ".")
	for i, p := range parts {
		v, present = cur[p]
		if !present {
			return nil, false, true
		}
		if i == len(parts)-1 {
			return v, true, true
		}
		next, isObj := v.(map[string]any)
		if !isObj {
			return nil, false, false
		}
		cur = next
	}

	return nil, false, true
}

func test(c Condition, v any, present bool) result {
	null := !present || v == nil
	switch {
	case c.Absent != nil:
		return boolResult(null == *c.Absent)
	case c.Empty != nil:
		var empty bool
		switch x := v.(type) {
		case nil:
			empty = true
		case []any:
			empty = len(x) == 0
		case string:
			empty = x == ""
		default:
			return unreadable
		}
		return boolResult(empty == *c.Empty)
	case c.Glob != nil:
		if null {
			return no
		}
		s, isStr := v.(string)
		if !isStr {
			return unreadable
		}
		m, _ := path.Match(*c.Glob, s)
		return boolResult(m)
	case c.Equals != nil:
		if null {
			return boolResult(bytes.Equal(bytes.TrimSpace(c.Equals), []byte("null")))
		}
		dec := json.NewDecoder(bytes.NewReader(c.Equals))
		dec.UseNumber()
		var want any
		if dec.Decode(&want) != nil {
			return unreadable
		}
		return boolResult(equal(want, v))
	}

	return unreadable // Check refuses a condition with no test
}

// equal compares decoded JSON, numbers exactly by value whether they came
// as json.Number or float64, so 1 equals 1.0 but 9007199254740993 doesn't
// equal 9007199254740992
func equal(a, b any) bool {
	switch x := a.(type) {
	case json.Number, float64:
		p, q := number(a), number(b)
		return p != nil && q != nil && p.Cmp(q) == 0
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !equal(xv, yv) {
				return false
			}
		}
		return true
	}

	return reflect.DeepEqual(a, b)
}

// number gives a JSON number's exact value, or nil for anything else
func number(v any) *big.Rat {
	switch x := v.(type) {
	case json.Number:
		r, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return nil
		}
		return r
	case float64:
		return new(big.Rat).SetFloat64(x) // nil for NaN and the infinities
	}

	return nil
}

func stricter(a, b string) string {
	if Rank(a) >= Rank(b) {
		return a
	}

	return b
}

func boolResult(b bool) result {
	if b {
		return yes
	}

	return no
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)

	return out
}
