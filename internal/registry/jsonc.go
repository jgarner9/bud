package registry

import (
	"bytes"
	"fmt"
)

// StripJSONC turns JSONC into JSON: it blanks // and /* */ comments and drops
// a comma that only precedes a closing } or ], leaving strings untouched.
// Comments become spaces (newlines kept), so encoding/json's error offsets and
// any line numbers worked out from them still point at the original text
func StripJSONC(src []byte) ([]byte, error) {
	out := make([]byte, 0, len(src))
	// pendingComma is where in out a comma waits to learn what follows it
	pendingComma := -1
	line := 1
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '"':
			end, err := stringEnd(src, i)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			pendingComma = -1
			out = append(out, src[i:end]...)
			i = end - 1
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				out = append(out, ' ')
				i++
			}
			i-- // the newline goes through the default case
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return nil, fmt.Errorf("line %d: a /* comment is never closed", line)
			}
			for _, b := range src[i : i+2+end+2] {
				if b == '\n' {
					line++
					out = append(out, '\n')
				} else {
					out = append(out, ' ')
				}
			}
			i += 2 + end + 1
		case c == ',':
			pendingComma = len(out)
			out = append(out, c)
		case c == '}' || c == ']':
			if pendingComma >= 0 {
				out[pendingComma] = ' '
				pendingComma = -1
			}
			out = append(out, c)
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			if c == '\n' {
				line++
			}
			out = append(out, c)
		default:
			pendingComma = -1
			out = append(out, c)
		}
	}

	return out, nil
}

// stringEnd returns the index just past the string that opens at src[start]
func stringEnd(src []byte, start int) (int, error) {
	for i := start + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		case '\n':
			return 0, fmt.Errorf("a string runs past the end of its line")
		}
	}

	return 0, fmt.Errorf("a string is never closed")
}
