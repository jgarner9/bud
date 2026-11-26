package registry

import (
	"fmt"

	"github.com/tidwall/jsonc"
)

// StripJSONC turns JSONC into JSON: it blanks // and /* */ comments and a
// comma that only precedes a closing } or ], leaving strings untouched.
// Comments become spaces (newlines kept), so encoding/json's error offsets
// and any line numbers worked out from them still point at the original
// text. The scanning is github.com/tidwall/jsonc's ToJSON: it is the one
// exception to D1's standard-library-only rule (D153), granted by Jason,
// and the only established library whose output keeps the input's byte
// length and line breaks, which PatchFacts' in-place edits and Parse's
// line-numbered errors depend on. It reports no error of its own: an
// unterminated string or comment is left for encoding/json to report, at
// the right line
func StripJSONC(src []byte) []byte {
	return jsonc.ToJSON(src)
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
