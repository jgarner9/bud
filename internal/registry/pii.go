package registry

import (
	"math"
	"strings"
)

// Validators are the named checks a privacy rule's validate field picks
// (Suite D118). Each gets the text the rule's pattern matched (its secret
// group when it has one) and says whether it really is what the rule looks
// for, so a pattern can be broad and the checksum or range rules keep it
// from firing on any run of digits. Like the rules, they only ever raise a
// tier
var Validators = map[string]func(string) bool{
	"luhn":  Luhn,
	"iban":  IBAN,
	"ssn":   SSN,
	"phone": Phone,
}

// digits keeps the digits of s, dropping spaces, dashes, dots and brackets;
// any other character makes it fail
func digits(s string, keep string) (string, bool) {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		case strings.ContainsRune(keep, c):
		default:
			return "", false
		}
	}

	return b.String(), true
}

// Luhn says whether s is a card number: 13 to 19 digits, spaces or dashes
// between them, passing the Luhn checksum and not all one digit
func Luhn(s string) bool {
	d, ok := digits(s, " -")
	if !ok || len(d) < 13 || len(d) > 19 || strings.Count(d, d[:1]) == len(d) {
		return false
	}
	sum := 0
	for i := range len(d) {
		n := int(d[len(d)-1-i] - '0')
		if i%2 == 1 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}

	return sum%10 == 0
}

// IBAN says whether s is an IBAN: a country code, two check digits and up
// to 30 letters or digits, spaces allowed, whose mod-97 remainder is 1
// (ISO 13616)
func IBAN(s string) bool {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	for i, c := range s {
		letter, digit := c >= 'A' && c <= 'Z', c >= '0' && c <= '9'
		if (i < 2 && !letter) || (i >= 2 && i < 4 && !digit) || (!letter && !digit) {
			return false
		}
	}
	rem := 0
	for _, c := range s[4:] + s[:4] {
		if c >= 'A' {
			rem = (rem*100 + int(c-'A'+10)) % 97
		} else {
			rem = (rem*10 + int(c-'0')) % 97
		}
	}

	return rem == 1
}

// SSN says whether s is a US Social Security number that could have been
// issued: area not 000, 666 or 900 and up, group not 00, serial not 0000,
// and not one of the numbers printed in ads and on sample cards
func SSN(s string) bool {
	d, ok := digits(s, "- ")
	if !ok || len(d) != 9 {
		return false
	}
	area, group, serial := d[:3], d[3:5], d[5:]
	switch {
	case area == "000" || area == "666" || area[0] == '9', group == "00", serial == "0000":
		return false
	case d == "078051120" || d == "219099999" || d == "123456789":
		return false
	}

	return true
}

// Phone says whether s is a phone number: + and 8 to 15 digits (E.164), or
// a North American number, 10 digits (11 with a leading 1) whose area code
// and exchange don't start with 0 or 1
func Phone(s string) bool {
	intl := strings.HasPrefix(strings.TrimSpace(s), "+")
	d, ok := digits(strings.TrimPrefix(strings.TrimSpace(s), "+"), " -.()")
	if !ok {
		return false
	}
	if intl && !strings.HasPrefix(d, "1") {
		return len(d) >= 8 && len(d) <= 15
	}
	if len(d) == 11 && d[0] == '1' {
		d = d[1:]
	}
	if len(d) != 10 || strings.Count(d, d[:1]) == len(d) {
		return false
	}

	return d[0] >= '2' && d[3] >= '2'
}

// Entropy is the Shannon entropy of s in bits per character, as gitleaks
// computes it for a secret
func Entropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	n := 0
	for _, c := range s {
		counts[c]++
		n++
	}
	h := 0.0
	for _, k := range counts {
		p := float64(k) / float64(n)
		h -= p * math.Log2(p)
	}

	return h
}
