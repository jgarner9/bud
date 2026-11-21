package registry

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
)

// MorePrivate reports whether tier a is more private than b (P0 > P1 > P2)
func MorePrivate(a, b string) bool {
	ia, ib := slices.Index(Tiers, a), slices.Index(Tiers, b)
	return ia >= 0 && ib >= 0 && ia < ib
}

// CompiledRule is a PrivacyRule ready to match
type CompiledRule struct {
	Name, Tier string
	Re         *regexp.Regexp
}

// CompileRules compiles the registry's rules sorted by name; an error
// names the rule (Validate catches these first)
func (r *Registry) CompileRules() ([]CompiledRule, error) {
	names := make([]string, 0, len(r.PrivacyRules))
	for name := range r.PrivacyRules {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]CompiledRule, 0, len(names))
	for _, name := range names {
		rule := r.PrivacyRules[name]
		re, err := regexp.Compile(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("privacy rule %s: match doesn't compile: %w", name, err)
		}
		out = append(out, CompiledRule{Name: name, Tier: rule.Tier, Re: re})
	}

	return out, nil
}

// Raise returns the tier after the rules: the most private of tier and
// every matching rule's tier, and the name of the first rule (by name)
// that gave that tier, or "" when no rule raised it
func Raise(tier string, rules []CompiledRule, texts []string) (string, string) {
	got, by := tier, ""
	for _, rule := range rules {
		if !MorePrivate(rule.Tier, got) {
			continue
		}
		if slices.ContainsFunc(texts, rule.Re.MatchString) {
			got, by = rule.Tier, rule.Name
			if got == "P0" {
				break
			}
		}
	}

	return got, by
}
