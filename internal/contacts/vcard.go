package contacts

import (
	"slices"
	"strings"
	"unicode"
)

// Contact is the minimal information needed to draft an address or number.
type Contact struct {
	Name      string   `json:"name"`
	Nicknames []string `json:"nicknames,omitempty"`
	Emails    []string `json:"emails,omitempty"`
	Phones    []string `json:"phones,omitempty"`
}

var unescapeText = strings.NewReplacer(`\\`, `\`, `\,`, ",", `\;`, ";", `\n`, " ", `\N`, " ")

func parseCard(raw, query string) (Contact, bool) {
	var out Contact
	var structured string
	var card, group bool
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\n ", "")
	raw = strings.ReplaceAll(raw, "\n\t", "")
	for line := range strings.SplitSeq(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, _, _ = strings.Cut(key, ";")
		if _, prop, grouped := strings.Cut(key, "."); grouped {
			key = prop
		}
		value = strings.TrimSpace(value)
		switch strings.ToUpper(key) {
		case "BEGIN":
			card = strings.EqualFold(value, "VCARD")
		case "END":
			if !strings.EqualFold(value, "VCARD") {
				continue
			}
			if out.Name == "" {
				out.Name = structured
			}
			match := func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(query)) }
			return out, card && !group && out.Name != "" && (match(out.Name) || match(structured) || slices.ContainsFunc(out.Nicknames, match))
		case "KIND", "X-ADDRESSBOOKSERVER-KIND":
			group = group || strings.EqualFold(value, "group")
		case "FN":
			out.Name = clean(value)
		case "N":
			names := splitText(value, ';')
			if len(names) >= 2 {
				names[0], names[1] = names[1], names[0]
			}
			structured = strings.Join(slices.DeleteFunc(names, func(name string) bool { return name == "" }), " ")
		case "NICKNAME":
			for _, nickname := range splitText(value, ',') {
				addValue(&out.Nicknames, nickname)
			}
		case "EMAIL":
			addValue(&out.Emails, strings.TrimPrefix(clean(value), "mailto:"))
		case "TEL":
			addValue(&out.Phones, strings.TrimPrefix(clean(value), "tel:"))
		}
	}
	return Contact{}, false
}

func addValue(values *[]string, value string) {
	if value != "" && len(*values) < 12 && !slices.Contains(*values, value) {
		*values = append(*values, value)
	}
}

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, unescapeText.Replace(s))
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return ""
	}
	return s
}

// splitText ignores separators escaped by a backslash.
func splitText(value string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			i++
		} else if value[i] == sep {
			out = append(out, clean(value[start:i]))
			start = i + 1
		}
	}
	return append(out, clean(value[start:]))
}
