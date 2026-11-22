package registry

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// rnd is a deterministic string of n characters from alphabet, varied
// enough to pass a secret rule's entropy. Samples are built at test time so
// no token-shaped string sits in the repo for a secret scanner to trip on
func rnd(alphabet string, n int) string {
	var b strings.Builder
	x := &rndState
	for range n {
		*x ^= *x << 13
		*x ^= *x >> 17
		*x ^= *x << 5
		b.WriteByte(alphabet[*x%uint32(len(alphabet))])
	}

	return b.String()
}

// rndState carries on from call to call, so two samples never repeat
var rndState = uint32(2463534242)

const (
	upperNum = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	lowerNum = "abcdefghijklmnopqrstuvwxyz0123456789"
	alnum    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	word     = alnum + "_"
	dash     = word + "-"
	hex      = "0123456789abcdef"
	b64      = alnum + "+/"
)

// packSamples give each shipped rule a text it must match and one it
// mustn't (Suite D118). Every shipped rule needs an entry, so a rule added
// to the registry comes with its samples
func packSamples() map[string][2]string {
	gh := func(p string) [2]string { return [2]string{p + rnd(alnum, 36), p + strings.Repeat("a", 36)} }

	return map[string][2]string{
		"marker":      {"#private what's my balance", "my #private note"},
		"private-key": {"-----BEGIN OPENSSH PRIVATE KEY-----", "a public key"},

		"pii-card":  {"card 4111 1111 1111 1111 exp 12/29", "card 4111 1111 1111 1112, or epoch ms 1763800000000"},
		"pii-iban":  {"pay GB82 WEST 1234 5698 7654 32 today, or de89370400440532013000", "pay GB82 WEST 1234 5698 7654 33 today"},
		"pii-ssn":   {"ssn 123-45-6788", "ssn 000-12-3456, 666-12-3456, 900-12-3456, 123-45-6789, date 2026-11-22"},
		"pii-phone": {"call (415) 555-2671 or +44 20 7946 0958", "call (015) 555-2671 at 1.2.3, or build 123-456-7890"},

		"secret-1password-secret-key":            {"A3-" + rnd(upperNum, 6) + "-" + rnd(upperNum, 11) + "-" + rnd(upperNum, 5) + "-" + rnd(upperNum, 5) + "-" + rnd(upperNum, 5), "A3-AAAAAA-AAAAAAAAAAA-AAAAA-AAAAA-AAAAA"},
		"secret-1password-service-account-token": {"ops_eyJ" + rnd(b64, 260), "ops_eyJ" + rnd(b64, 40)},
		"secret-age-secret-key":                  {"AGE-SECRET-KEY-1" + rnd("QPZRY9X8GF2TVDW0S3JN54KHCE6MUA7L", 58), "AGE-SECRET-KEY-1" + rnd("QPZRY9X8GF2TVDW0S3JN54KHCE6MUA7L", 20)},
		"secret-anthropic-admin-api-key":         {"sk-ant-admin01-" + rnd(dash, 93) + "AA ", "sk-ant-admin01-" + rnd(dash, 20) + "AA "},
		"secret-anthropic-api-key":               {"sk-ant-api03-" + rnd(dash, 93) + "AA ", "sk-ant-api03-" + rnd(dash, 20) + "AA "},
		"secret-aws-access-token":                {"AKIA" + rnd("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", 16), "AKIAAAAAAAAAAAAAAAAA"},
		"secret-digitalocean-pat":                {"dop_v1_" + rnd(hex, 64) + " ", "dop_v1_" + rnd(hex, 20) + " "},
		"secret-doppler-api-token":               {"dp.pt." + rnd(lowerNum, 43), "dp.pt." + rnd(lowerNum, 10)},
		"secret-gcp-api-key":                     {"AIza" + rnd(dash, 35) + " ", "AIza" + strings.Repeat("a", 35) + " "},
		"secret-github-app-token":                gh("ghs_"),
		"secret-github-fine-grained-pat":         {"github_pat_" + rnd(word, 82), "github_pat_" + rnd(word, 30)},
		"secret-github-oauth":                    gh("gho_"),
		"secret-github-pat":                      gh("ghp_"),
		"secret-github-refresh-token":            gh("ghr_"),
		"secret-gitlab-pat":                      {"glpat-" + rnd(dash, 20), "glpat-aaaaaaaaaaaaaaaaaaaa"},
		"secret-hashicorp-tf-api-token":          {rnd(lowerNum, 14) + ".atlasv1." + rnd(dash, 65), "aaaaaaaaaaaaaa.atlasv1." + rnd(dash, 20)},
		"secret-huggingface-access-token":        {"hf_" + rnd("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", 34) + " ", "hf_" + strings.Repeat("a", 34) + " "},
		"secret-jwt":                             {"eyJ" + rnd(alnum, 20) + ".eyJ" + rnd(alnum, 30) + "." + rnd(alnum, 30) + " ", "eyJ.eyJ.x"},
		"secret-linear-api-key":                  {"lin_api_" + rnd(alnum, 40), "lin_api_" + strings.Repeat("a", 40)},
		"secret-npm-access-token":                {"npm_" + rnd(lowerNum, 36) + " ", "npm_" + rnd(lowerNum, 10) + " "},
		"secret-openai-api-key":                  {"sk-proj-" + rnd(dash, 74) + "T3BlbkFJ" + rnd(dash, 74) + " ", "sk-proj-" + rnd(dash, 30) + " "},
		"secret-perplexity-api-key":              {"pplx-" + rnd(alnum, 48) + " ", "pplx-" + strings.Repeat("a", 48) + " "},
		"secret-postman-api-token":               {"PMAK-" + rnd(hex, 24) + "-" + rnd(hex, 34) + " ", "PMAK-" + rnd(hex, 10) + " "},
		"secret-pypi-upload-token":               {"pypi-AgEIcHlwaS5vcmc" + rnd(dash, 60), "pypi-AgEIcHlwaS5vcmc" + rnd(dash, 10)},
		"secret-sendgrid-api-token":              {"SG." + rnd(alnum, 66) + " ", "SG." + rnd(alnum, 20) + " "},
		"secret-shopify-access-token":            {"shpat_" + rnd(hex, 32), "shpat_" + strings.Repeat("a", 32)},
		"secret-slack-app-token":                 {"xapp-1-A" + rnd(upperNum, 10) + "-1234567890-" + rnd(lowerNum, 30), "xapp-x"},
		"secret-slack-bot-token":                 {"xoxb-1234567890-1234567890" + rnd(alnum, 24), "xoxb-12-34"},
		"secret-slack-user-token":                {"xoxp-1234567890-1234567890-1234567890-" + rnd(alnum, 32), "xoxp-1-2-3-" + rnd(alnum, 32)},
		"secret-slack-webhook-url":               {"https://hooks.slack.com/services/" + rnd(alnum, 44), "https://hooks.slack.com/services/T0"},
		"secret-stripe-access-token":             {"sk_live_" + rnd(alnum, 30) + " ", "sk_live_" + strings.Repeat("a", 30) + " "},
		"secret-vault-service-token":             {"hvs." + rnd(dash, 100) + " ", "hvs." + rnd(dash, 20) + " "},
	}
}

func shippedRules(t testing.TB) []CompiledRule {
	t.Helper()
	reg, err := Load(filepath.Join("..", "..", "content", "env", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := reg.CompileRules()
	if err != nil {
		t.Fatal(err)
	}

	return rules
}

func TestEveryShippedRuleHasSamplesAndMatchesThem(t *testing.T) {
	samples := packSamples()
	for _, r := range shippedRules(t) {
		s, ok := samples[r.Name]
		if !ok {
			t.Errorf("%s has no samples; add a text it must match and one it mustn't to packSamples", r.Name)
			continue
		}
		if !r.Matches(s[0]) {
			t.Errorf("%s doesn't match %q", r.Name, s[0])
		}
		if r.Matches(s[1]) {
			t.Errorf("%s matches %q, which it mustn't", r.Name, s[1])
		}
		if got, by := Raise("P1", []CompiledRule{r}, []string{"hello", s[0]}); got != r.Tier || by != r.Name {
			t.Errorf("%s: Raise gave %s by %q", r.Name, got, by)
		}
		delete(samples, r.Name)
	}
	for name := range samples {
		t.Errorf("samples for %s, which isn't a shipped rule", name)
	}
}

func TestTheValidators(t *testing.T) {
	for _, c := range []struct {
		check func(string) bool
		s     string
		want  bool
	}{
		{Luhn, "4111111111111111", true},
		{Luhn, "4111-1111-1111-1111", true},
		{Luhn, "378282246310005", true}, // Amex test number, 15 digits
		{Luhn, "4111111111111112", false},
		{Luhn, "0000000000000000", false}, // passes the checksum, but one digit
		{Luhn, "411111111111", false},     // too short
		{Luhn, "4111x111111111111", false},
		{IBAN, "GB82 WEST 1234 5698 7654 32", true},
		{IBAN, "de89370400440532013000", true},
		{IBAN, "GB82WEST12345698765433", false},
		{IBAN, "1282WEST12345698765432", false},
		{IBAN, "GB82", false},
		{SSN, "123-45-6788", true},
		{SSN, "000-45-6788", false},
		{SSN, "666-45-6788", false},
		{SSN, "950-45-6788", false},
		{SSN, "123-00-6788", false},
		{SSN, "123-45-0000", false},
		{SSN, "078-05-1120", false},
		{Phone, "(415) 555-2671", true},
		{Phone, "1 415 555 2671", true},
		{Phone, "+1 415-555-2671", true},
		{Phone, "+44 20 7946 0958", true},
		{Phone, "(015) 555-2671", false},
		{Phone, "415 155 2671", false},
		{Phone, "222-222-2222", false},
		{Phone, "+44 12", false},
		{Phone, "415-555-267a", false},
	} {
		if got := c.check(c.s); got != c.want {
			t.Errorf("%q: got %v, want %v", c.s, got, c.want)
		}
	}
	if e := Entropy("aaaa"); e != 0 {
		t.Errorf("Entropy(aaaa) = %v", e)
	}
	if e := Entropy("abcd"); e != 2 {
		t.Errorf("Entropy(abcd) = %v", e)
	}
}

func TestABadEntropyValidatorOrKeywordIsRefused(t *testing.T) {
	for _, c := range []struct {
		rule PrivacyRule
		bad  bool
	}{
		{PrivacyRule{Match: "x", Tier: "P0", Entropy: 9}, true},
		{PrivacyRule{Match: "x", Tier: "P0", Validate: "crc32"}, true},
		{PrivacyRule{Match: "x", Tier: "P0", Keywords: []string{"Ghp_"}}, true},
		{PrivacyRule{Match: "x", Tier: "P0", Keywords: []string{""}}, true},
		{PrivacyRule{Match: "x", Tier: "P0", Validate: "luhn", Entropy: 3.5, Keywords: []string{"ghp_"}}, false},
	} {
		var got []string
		checkPrivacyRule("r", c.rule, func(f string, a ...any) { got = append(got, f) })
		if c.bad != (len(got) > 0) {
			t.Errorf("%+v: problems %q", c.rule, got)
		}
	}
}

// a match passes only past the entropy, every match is tried, and the
// secret is the first group that took part and isn't empty
func TestMatchesPicksTheSecretAndTriesEveryMatch(t *testing.T) {
	r := CompiledRule{Name: "r", Tier: "P0", Re: regexp.MustCompile(`k(x*)=([a-z0-9]+);`), Entropy: 2}
	if r.Matches("k=abcd;") {
		t.Error("abcd has entropy 2 exactly, which isn't over 2")
	}
	if !r.Matches("k=abcde;") {
		t.Error("abcde is over 2 and is the first non-empty group")
	}
	card := CompiledRule{Name: "c", Tier: "P0", Re: regexp.MustCompile(`\d{16}`), Check: Luhn}
	text := strings.Repeat("4111111111111112 ", 100) + "4111111111111111"
	if !card.Matches(text) {
		t.Error("the valid number after 100 near misses should count")
	}
	if card.Matches(strings.Repeat("4111111111111112 ", 100)) {
		t.Error("100 near misses aren't a hit")
	}
}

// a keyword must be in the text, in any case, before the pattern is tried
func TestKeywordsGateTheMatch(t *testing.T) {
	re := regexp.MustCompile(`(?i)tok_[a-z]{4}`)
	r := CompiledRule{Name: "r", Tier: "P0", Re: re, Keywords: []string{"tok_"}}
	if !r.Matches("TOK_abcd") || r.Matches("tak_abcd") {
		t.Error("the keyword should gate the pattern, in any case")
	}
	r.Keywords = []string{"other"}
	if r.Matches("tok_abcd") {
		t.Error("without its keyword the rule must not match")
	}
}

// a chat the size of a long Signal thread: every shipped rule over it must
// stay well under the router's 5 ms budget (Suite D118)
func chatTexts() []string {
	var texts []string
	for i := range 40 {
		texts = append(texts, strings.Repeat("Could you look at the build log from this morning and tell me what failed? ", 3)+rnd(alnum, 20)+" step "+strings.Repeat("7", i%5+1))
	}

	return texts
}

func BenchmarkTheShippedRulesOverAChat(b *testing.B) {
	rules, texts := shippedRules(b), chatTexts()
	b.ResetTimer()
	for range b.N {
		Raise("P1", rules, texts)
	}
}

func TestTheShippedRulesStayInBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	rules, texts := shippedRules(t), chatTexts()
	start := time.Now()
	const runs = 20
	for range runs {
		if got, by := Raise("P1", rules, texts); got != "P1" {
			t.Fatalf("an ordinary chat was raised by %s", by)
		}
	}
	// 5 ms is the budget; the check allows four times that, so a slow CI
	// machine doesn't fail it, and still catches a rule gone quadratic
	if per := time.Since(start) / runs; per > 20*time.Millisecond {
		t.Errorf("the rules took %v per chat, over budget", per)
	}
}
