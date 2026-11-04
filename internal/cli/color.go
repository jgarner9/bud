package cli

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// ANSI styles for terminal output (JGA-109), standard library only (D1)
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

// colorOn says whether w gets colour: a terminal, TERM isn't dumb, and
// NO_COLOR is unset (no-color.org). Anything else (a pipe, a file, a test's
// buffer) gets plain text
var colorOn = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}

	return isTerminal(f)
}

// paint wraps s in style when w takes colour
func paint(w io.Writer, style, s string) string {
	if s == "" || !colorOn(w) {
		return s
	}

	return style + s + ansiReset
}

// statusStyle is the colour of a doctor or dep status word
func statusStyle(status string) string {
	switch status {
	case "ok":
		return ansiGreen
	case "warn":
		return ansiYellow
	}

	return ansiRed
}

// cell pads s to width (in runes, like fmt's %-*s), then paints it, so the
// escape codes never count toward the column
func cell(w io.Writer, style, s string, width int) string {
	return paint(w, style, s) + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s)))
}

// onStyle is the colour of an enabled/disabled or on/off word
func onStyle(on bool) string {
	if on {
		return ansiGreen
	}

	return ansiDim
}

// paintJSONKeys bolds the object keys in json.MarshalIndent output
func paintJSONKeys(w io.Writer, text string) string {
	if !colorOn(w) {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		body := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(body, `"`) {
			continue
		}
		end := jsonStringEnd(body)
		if end < 0 || !strings.HasPrefix(body[end:], ": ") {
			continue
		}
		lines[i] = line[:len(line)-len(body)] + ansiBold + body[:end] + ansiReset + body[end:]
	}

	return strings.Join(lines, "\n")
}

// jsonStringEnd is the index just past the JSON string s starts with, or -1
func jsonStringEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}

	return -1
}
