package hermes

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The file is YAML, written by this small emitter, since bud takes no
// third-party modules (D1): every string double-quoted, keys sorted, so the
// same input always gives the same bytes

// writeYAML writes a map as block-style YAML at the given indent
func writeYAML(b *strings.Builder, m map[string]any, indent int) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	pad := strings.Repeat("  ", indent)
	for _, k := range keys {
		b.WriteString(pad + key(k) + ":")
		if err := writeValue(b, m[k], indent); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}

	return nil
}

// writeValue writes v after a key or a list dash: a scalar or empty value on
// the same line, a map or list on the lines below
func writeValue(b *strings.Builder, v any, indent int) error {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			b.WriteString(" {}\n")
			return nil
		}
		b.WriteString("\n")
		return writeYAML(b, x, indent+1)
	case []any:
		if len(x) == 0 {
			b.WriteString(" []\n")
			return nil
		}
		b.WriteString("\n")
		pad := strings.Repeat("  ", indent+1)
		for _, item := range x {
			switch it := item.(type) {
			case []any:
				return fmt.Errorf("a list holds a list, which this emitter doesn't write")
			case map[string]any:
				if len(it) == 0 {
					b.WriteString(pad + "- {}\n")
					continue
				}
				// the first key goes on the dash's line and the rest line up
				// under it, two spaces in
				var sub strings.Builder
				if err := writeYAML(&sub, it, indent+2); err != nil {
					return err
				}
				b.WriteString(pad + "- " + strings.TrimPrefix(sub.String(), pad+"  "))
				continue
			}
			b.WriteString(pad + "-")
			if err := writeValue(b, item, indent+1); err != nil {
				return err
			}
		}
		return nil
	}
	s, err := scalar(v)
	if err != nil {
		return err
	}
	b.WriteString(" " + s + "\n")

	return nil
}

func scalar(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case string:
		return quote(x), nil
	case int:
		return strconv.Itoa(x), nil
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return "", fmt.Errorf("%v isn't a number YAML can hold", x)
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}

	return "", fmt.Errorf("can't write a %T", v)
}

var plainKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// key writes a map key plain when YAML can't read it as anything but that
// string, and quoted otherwise
func key(k string) string {
	switch strings.ToLower(k) {
	case "y", "n", "yes", "no", "on", "off", "true", "false", "null":
		return quote(k)
	}
	if plainKey.MatchString(k) {
		return k
	}

	return quote(k)
}

// quote is a JSON string, which is also a YAML double-quoted scalar
func quote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes

	return strings.TrimSuffix(b.String(), "\n")
}
