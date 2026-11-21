package registry

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEachPrivacyRuleProblemSaysWhatToFix(t *testing.T) {
	cases := []struct {
		name  string
		rules map[string]PrivacyRule
		want  string
	}{
		{"bad name", map[string]PrivacyRule{"Bad Name": {Match: "x", Tier: "P0"}}, "privacy_rules.Bad Name: an ID is lowercase letters, digits, '.', '_' and '-'"},
		{"empty match", map[string]PrivacyRule{"r": {Tier: "P0"}}, "privacy_rules.r: match is empty; give it a regular expression"},
		{"bad regexp", map[string]PrivacyRule{"r": {Match: "(", Tier: "P0"}}, "privacy_rules.r: match doesn't compile: error parsing regexp: missing closing ): `(`; it is Go's RE2 syntax"},
		{"tier P2", map[string]PrivacyRule{"r": {Match: "x", Tier: "P2"}}, "privacy_rules.r: tier P2 can't raise anything, since P2 is the least private; use P0 or P1"},
		{"other tier", map[string]PrivacyRule{"r": {Match: "x", Tier: "P9"}}, `privacy_rules.r: tier is "P9"; use P0 or P1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := valid()
			r.PrivacyRules = c.rules
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestAnOverlayPrivacyRuleReplacesByNameAndNewOnesAreAdded(t *testing.T) {
	base := valid()
	base.PrivacyRules = map[string]PrivacyRule{
		"keep":    {Match: "a", Tier: "P1"},
		"replace": {Match: "b", Tier: "P1"},
	}
	overlay := &Registry{PrivacyRules: map[string]PrivacyRule{
		"replace": {Match: "c", Tier: "P0"},
		"new":     {Match: "d", Tier: "P0"},
	}}
	got, err := Merge(base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if got.PrivacyRules["keep"].Match != "a" || got.PrivacyRules["replace"] != (PrivacyRule{Match: "c", Tier: "P0"}) || got.PrivacyRules["new"].Match != "d" {
		t.Fatalf("rules = %+v", got.PrivacyRules)
	}
	if base.PrivacyRules["replace"].Match != "b" {
		t.Fatal("Merge changed the base registry")
	}
}

func TestMorePrivateOrdersP0AboveP1AboveP2(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"P0", "P1", true}, {"P0", "P2", true}, {"P1", "P2", true},
		{"P1", "P0", false}, {"P2", "P0", false}, {"P2", "P1", false},
		{"P0", "P0", false}, {"P1", "P1", false}, {"P2", "P2", false},
	}
	for _, c := range cases {
		if got := MorePrivate(c.a, c.b); got != c.want {
			t.Errorf("MorePrivate(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestRaiseOnlyMovesTowardP0AndNamesTheRuleThatDidIt(t *testing.T) {
	reg := &Registry{PrivacyRules: map[string]PrivacyRule{
		"a-p1": {Match: "alpha", Tier: "P1"},
		"b-p0": {Match: "beta", Tier: "P0"},
		"c-p0": {Match: "beta", Tier: "P0"},
	}}
	rules, err := reg.CompileRules()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		tier         string
		rules        []CompiledRule
		texts        []string
		wantT, wantR string
	}{
		{"no rules", "P1", nil, []string{"alpha"}, "P1", ""},
		{"p1 request, p0 rule", "P1", rules, []string{"beta"}, "P0", "b-p0"},
		{"p0 request, p1 rule", "P0", rules[:1], []string{"alpha"}, "P0", ""},
		{"p2 request, both match", "P2", rules, []string{"alpha", "beta"}, "P0", "b-p0"},
		{"p2 request, only p1 matches", "P2", rules, []string{"alpha"}, "P1", "a-p1"},
		{"two p0 rules match", "P2", rules[1:], []string{"beta"}, "P0", "b-p0"},
		{"no match", "P1", rules, []string{"gamma"}, "P1", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gt, gr := Raise(c.tier, c.rules, c.texts)
			if gt != c.wantT || gr != c.wantR {
				t.Fatalf("Raise = %s, %q; want %s, %q", gt, gr, c.wantT, c.wantR)
			}
		})
	}
}

func TestTheShippedPrivacyRulesMatchWhatTheyShould(t *testing.T) {
	reg, err := Load(filepath.Join("..", "..", "content", "env", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := reg.CompileRules()
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]CompiledRule{}
	for _, r := range rules {
		by[r.Name] = r
	}
	cases := []struct {
		rule, text string
		want       bool
	}{
		{"marker", "#private what's my balance", true},
		{"marker", "  #PRIVATE x", true},
		{"marker", "my #private note", false},
		{"marker", "#privateer", false},
		{"private-key", "-----BEGIN OPENSSH PRIVATE KEY-----", true},
		{"private-key", "a public key", false},
	}
	for _, c := range cases {
		r, ok := by[c.rule]
		if !ok {
			t.Fatalf("no shipped rule %s", c.rule)
		}
		if got := r.Re.MatchString(c.text); got != c.want {
			t.Errorf("%s on %q = %v, want %v", c.rule, c.text, got, c.want)
		}
	}
}
