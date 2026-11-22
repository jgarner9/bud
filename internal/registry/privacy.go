package registry

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
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
	// Entropy and Check are the rule's entropy and validate, both met by
	// the matched text (its secret group when it has one)
	Entropy float64
	Check   func(string) bool
	// Keywords are lowercase; one must be in the text before Re is tried
	Keywords []string
}

// maxMatches bounds how many matches of one rule in one text are checked
// against its entropy and validator; a text with more is treated as matched,
// which only raises the tier
const maxMatches = 64

// Matches says whether the rule matches text. A rule with neither entropy
// nor a check matches on its pattern alone. Otherwise one match must meet
// both: the secret is the group named secret, else the first group that
// took part, else the whole match, as in gitleaks
func (c CompiledRule) Matches(text string) bool { return c.matches(text, strings.ToLower(text)) }

// matches is Matches with the text's lowercase form, made once per text
func (c CompiledRule) matches(text, lower string) bool {
	if len(c.Keywords) > 0 && !slices.ContainsFunc(c.Keywords, func(k string) bool { return strings.Contains(lower, k) }) {
		return false
	}
	if c.Entropy == 0 && c.Check == nil {
		return c.Re.MatchString(text)
	}
	all := c.Re.FindAllStringSubmatchIndex(text, maxMatches+1)
	if len(all) > maxMatches {
		return true
	}
	named := c.Re.SubexpIndex("secret")
	for _, m := range all {
		secret := text[m[0]:m[1]]
		switch {
		case named > 0 && m[2*named] >= 0:
			secret = text[m[2*named]:m[2*named+1]]
		case named < 0 && len(m) > 2 && m[2] >= 0:
			secret = text[m[2]:m[3]]
		}
		if Entropy(secret) >= c.Entropy && (c.Check == nil || c.Check(secret)) {
			return true
		}
	}

	return false
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
		c := CompiledRule{Name: name, Tier: rule.Tier, Re: re, Entropy: rule.Entropy, Keywords: rule.Keywords}
		if rule.Validate != "" {
			check, ok := Validators[rule.Validate]
			if !ok {
				return nil, fmt.Errorf("privacy rule %s: no validator %q", name, rule.Validate)
			}
			c.Check = check
		}
		out = append(out, c)
	}

	return out, nil
}

// Raise returns the tier after the rules: the most private of tier and
// every matching rule's tier, and the name of the first rule (by name)
// that gave that tier, or "" when no rule raised it
func Raise(tier string, rules []CompiledRule, texts []string) (string, string) {
	got, by := tier, ""
	var lower []string
	for _, rule := range rules {
		if !MorePrivate(rule.Tier, got) {
			continue
		}
		if lower == nil {
			lower = make([]string, len(texts))
			for i, t := range texts {
				lower[i] = strings.ToLower(t)
			}
		}
		if matchesAny(rule, texts, lower) {
			got, by = rule.Tier, rule.Name
			if got == "P0" {
				break
			}
		}
	}

	return got, by
}

func matchesAny(rule CompiledRule, texts, lower []string) bool {
	for i, t := range texts {
		if rule.matches(t, lower[i]) {
			return true
		}
	}

	return false
}
