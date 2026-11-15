package registry

import (
	"fmt"
	"slices"
)

// PatchFacts rewrites the fact fields (inputs, caps, context, output, price)
// of the given models inside a JSONC registry file, in place. Everything
// else in the file stays byte for byte, comments and layout included, so a
// refresh's diff shows only the facts that changed (Suite D69). A field the
// model entry lacks is added after its last field
func PatchFacts(src []byte, models map[string]Model) ([]byte, error) {
	// StripJSONC keeps every byte offset, so spans found in the plain JSON
	// point into src as well
	plain, err := StripJSONC(src)
	if err != nil {
		return nil, err
	}
	root, err := scanObject(plain, skipSpace(plain, 0))
	if err != nil {
		return nil, fmt.Errorf("patch the registry: %w", err)
	}
	ms, ok := root.fields["models"]
	if !ok {
		return nil, fmt.Errorf("patch the registry: it has no models")
	}
	modelsObj, err := scanObject(plain, ms.start)
	if err != nil {
		return nil, fmt.Errorf("patch the registry: models: %w", err)
	}

	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, id := range sortedKeys(models) {
		at, ok := modelsObj.fields[id]
		if !ok {
			return nil, fmt.Errorf("patch the registry: models.%s isn't in the file", id)
		}
		obj, err := scanObject(plain, at.start)
		if err != nil {
			return nil, fmt.Errorf("patch the registry: models.%s: %w", id, err)
		}
		var added string
		for _, field := range factFields {
			text := factText(models[id], field)
			if text == "" {
				continue
			}
			if v, ok := obj.fields[field]; ok {
				if string(src[v.start:v.end]) != text {
					edits = append(edits, edit{v.start, v.end, text})
				}
				continue
			}
			added += fmt.Sprintf(", %q: %s", field, text)
		}
		if added != "" {
			edits = append(edits, edit{obj.lastEnd, obj.lastEnd, added})
		}
	}

	slices.SortFunc(edits, func(a, b edit) int { return b.start - a.start })
	out := slices.Clone(src)
	for _, e := range edits {
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}

	return out, nil
}

type span struct{ start, end int }

// object is a JSON object's value spans by key, and where its last value ends
type object struct {
	fields  map[string]span
	lastEnd int
}

// scanObject reads the object that opens at b[i]
func scanObject(b []byte, i int) (object, error) {
	o := object{fields: map[string]span{}}
	if i >= len(b) || b[i] != '{' {
		return o, fmt.Errorf("expected an object at byte %d", i)
	}
	o.lastEnd = i + 1
	i = skipSpace(b, i+1)
	for i < len(b) && b[i] != '}' {
		if b[i] == ',' {
			i = skipSpace(b, i+1)
			continue
		}
		if b[i] != '"' {
			return o, fmt.Errorf("expected a key at byte %d", i)
		}
		end, err := stringEnd(b, i)
		if err != nil {
			return o, err
		}
		key := string(b[i+1 : end-1])
		i = skipSpace(b, end)
		if i >= len(b) || b[i] != ':' {
			return o, fmt.Errorf("expected ':' after %q", key)
		}
		start := skipSpace(b, i+1)
		vend, err := skipValue(b, start)
		if err != nil {
			return o, fmt.Errorf("%s: %w", key, err)
		}
		o.fields[key] = span{start, vend}
		o.lastEnd = vend
		i = skipSpace(b, vend)
	}
	if i >= len(b) {
		return o, fmt.Errorf("an object is never closed")
	}

	return o, nil
}

// skipValue returns the index just past the value that starts at b[i]
func skipValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, fmt.Errorf("expected a value")
	}
	switch b[i] {
	case '"':
		return stringEnd(b, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(b); j++ {
			switch b[j] {
			case '"':
				end, err := stringEnd(b, j)
				if err != nil {
					return 0, err
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("a value is never closed")
	}
	j := i
	for j < len(b) && !slices.Contains([]byte(",}] \t\r\n"), b[j]) {
		j++
	}
	if j == i {
		return 0, fmt.Errorf("expected a value at byte %d", i)
	}

	return j, nil
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}

	return i
}
