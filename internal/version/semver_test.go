package version

import "testing"

func TestParse(t *testing.T) {
	for _, s := range []string{"0.5.1", "v0.5.1", "1.2.3-nightly.202610271200", "1.2.3+dirty", "0.1.1-0.20261026053815-cd37a8e46cdd+dirty"} {
		if _, ok := Parse(s); !ok {
			t.Errorf("%q should parse", s)
		}
	}
	for _, s := range []string{"", "dev", "1.2", "1.2.3.4", "01.2.3", "1.x.3", "1.2.3-", "-1.2.3"} {
		if _, ok := Parse(s); ok {
			t.Errorf("%q shouldn't parse", s)
		}
	}
	v, _ := Parse("v1.2.3-rc.1+build.5")
	if v.Major != 1 || v.Minor != 2 || v.Patch != 3 || len(v.Pre) != 2 || v.Pre[1] != "1" {
		t.Errorf("got %+v", v)
	}
}

func TestCompare(t *testing.T) {
	// each is older than the next (the semver.org precedence example, plus bud's own)
	ordered := []string{
		"0.4.1", "0.5.0", "0.5.1-nightly.202610270100", "0.5.1-nightly.202610271200",
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		a, _ := Parse(ordered[i])
		b, _ := Parse(ordered[i+1])
		if Compare(a, b) != -1 || Compare(b, a) != 1 {
			t.Errorf("%s should be older than %s", ordered[i], ordered[i+1])
		}
	}
	a, _ := Parse("v1.2.3+one")
	b, _ := Parse("1.2.3+two")
	if Compare(a, b) != 0 {
		t.Error("build metadata doesn't count")
	}
}
