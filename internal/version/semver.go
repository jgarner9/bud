package version

import (
	"strconv"
	"strings"
)

// Semver is a parsed MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD] version
type Semver struct {
	Major, Minor, Patch int
	Pre                 []string // dot-separated prerelease identifiers, nil for a release
}

// Parse reads a version with or without a leading "v". Build metadata (+...) is
// ignored, as semver says it doesn't affect precedence
func Parse(s string) (Semver, bool) {
	s = strings.TrimPrefix(s, "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Semver{}, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return Semver{}, false
		}
		nums[i] = n
	}

	v := Semver{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if hasPre {
		if pre == "" {
			return Semver{}, false
		}
		v.Pre = strings.Split(pre, ".")
	}

	return v, true
}

// Compare returns -1, 0 or 1 as a is older than, the same as, or newer than b,
// by semver precedence: a prerelease comes before its release
func Compare(a, b Semver) int {
	for _, d := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			return cmpInt(d[0], d[1])
		}
	}
	switch {
	case a.Pre == nil && b.Pre == nil:
		return 0
	case a.Pre == nil:
		return 1
	case b.Pre == nil:
		return -1
	}

	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		if c := comparePre(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}

	return cmpInt(len(a.Pre), len(b.Pre))
}

// comparePre orders two prerelease identifiers: numbers numerically and below
// words, words by ASCII
func comparePre(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return cmpInt(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}

	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}
