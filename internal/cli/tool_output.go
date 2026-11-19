package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// toolOutput is the output contract every command follows: compact plain text,
// --json for scripts, and --max-chars with a last line that says what was cut
// and how to get it.
type toolOutput struct {
	JSON     bool
	MaxChars int // 0 means no cap
}

// Bind registers --json and --max-chars on fs. defMax comes from config.
func (o *toolOutput) Bind(fs *flag.FlagSet, defMax int) {
	o.MaxChars = defMax
	fs.BoolVar(&o.JSON, "json", false, "print JSON instead of text")
	fs.IntVar(&o.MaxChars, "max-chars", defMax, "cap text output at this many characters (0 for no cap)")
}

// Print writes text, or v as JSON when --json is set. JSON is never cut, since
// a cut document would not parse. Text is cut at a line boundary where
// possible; the notice is one extra last line outside the budget.
func (o toolOutput) Print(w io.Writer, text string, v any) error {
	if o.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	text = strings.TrimRight(text, "\n")
	_, err := io.WriteString(w, truncateToolText(text, o.MaxChars)+"\n")
	return err
}

// truncateToolText cuts text to at most max characters and appends a notice line.
// max <= 0 returns text unchanged.
func truncateToolText(text string, max int) string {
	total := utf8.RuneCountInString(text)
	if max <= 0 || total <= max {
		return text
	}
	runes := []rune(text)
	cut, how := runes[:max], ""
	if runes[max] != '\n' { // the cut is not already at a line end
		if i := strings.LastIndexByte(string(cut), '\n'); i > 0 {
			cut = []rune(string(cut)[:i])
		} else {
			how = ", mid-line"
		}
	}
	return fmt.Sprintf("%s\n[cut %d of %d chars%s; rerun with --max-chars 0 for all]", string(cut), total-len(cut), total, how)
}
