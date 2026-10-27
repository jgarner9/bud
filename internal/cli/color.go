package cli

import (
	"io"
	"os"
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
func colorOn(w io.Writer) bool {
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
