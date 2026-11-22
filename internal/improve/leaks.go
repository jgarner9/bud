package improve

import (
	"fmt"
	"regexp"
	"strings"
)

// Leaks finds personal details a public proposal must not hold: the shapes
// scripts/leak-check.sh refuses (a tailnet name, a real 1Password vault, a
// home directory) plus Jason's own BUD_LEAK_PATTERNS. Placeholders pass, as
// they do there
type Leaks struct {
	own []*regexp.Regexp
}

var shapes = []struct {
	what    string
	find    *regexp.Regexp
	allowed *regexp.Regexp
}{
	{"a Tailscale name", regexp.MustCompile(`[A-Za-z0-9.-]*\.ts\.net`), regexp.MustCompile(`^\.ts\.net$|(^|\.)(example|your-tailnet|tailnet|tail)\.ts\.net$`)},
	{"a 1Password reference", regexp.MustCompile("op:" + "//" + "[^/\\s\"`)<>]+"), regexp.MustCompile("^op:" + "//" + "(vault|example)$")},
	{"a home directory", regexp.MustCompile(`/(Users|home)/[A-Za-z0-9._-]+`), regexp.MustCompile(`^/(Users|home)/(a|b|j|me|you|user|runner|example|name|someone)$`)},
}

// NewLeaks compiles patterns, one extended regex per line (the
// BUD_LEAK_PATTERNS format); blank lines are skipped. A pattern that doesn't
// compile is an error that doesn't show it, since it is personal
func NewLeaks(patterns string) (*Leaks, error) {
	l := &Leaks{}
	n := 0
	for line := range strings.SplitSeq(patterns, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n++
		re, err := regexp.Compile(line)
		if err != nil {
			return nil, fmt.Errorf("pattern %d of BUD_LEAK_PATTERNS doesn't compile; fix it", n)
		}
		l.own = append(l.own, re)
	}

	return l, nil
}

// Find returns what kind of personal detail the texts hold, or ""
func (l *Leaks) Find(texts ...string) string {
	for _, t := range texts {
		for _, s := range shapes {
			for _, m := range s.find.FindAllString(t, -1) {
				if !s.allowed.MatchString(m) {
					return s.what
				}
			}
		}
		if l == nil {
			continue
		}
		for _, re := range l.own {
			if re.MatchString(t) {
				return "one of your BUD_LEAK_PATTERNS"
			}
		}
	}

	return ""
}
