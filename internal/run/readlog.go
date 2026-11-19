package run

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MaxRead is the most lines one read returns, so a wide range stays cheap.
const MaxRead = 1000

// Range is a 1-based inclusive line range. To is 0 for "to the end".
type Range struct{ From, To int }

// ParseRange reads "a:b", "a:" (a to the end), ":b" (1 to b) or "a" (one line).
func ParseRange(s string) (Range, error) {
	bad := fmt.Errorf("bad --lines %q; use a:b, a: or :b with line numbers from 1, for example 100:140", s)
	from, to, found := strings.Cut(s, ":")
	if s == "" {
		return Range{}, bad
	}
	r := Range{From: 1}
	var err error
	if from != "" {
		if r.From, err = strconv.Atoi(from); err != nil || r.From < 1 {
			return Range{}, bad
		}
	}
	switch {
	case !found:
		r.To = r.From
	case to != "":
		if r.To, err = strconv.Atoi(to); err != nil || r.To < r.From {
			return Range{}, bad
		}
	}
	return r, nil
}

// Slice is a piece of a log.
type Slice struct {
	Log    string `json:"log"`
	Total  int    `json:"total"`  // lines in the whole log
	Lines  []Line `json:"lines"`  // the lines asked for
	Capped bool   `json:"capped"` // the range or tail was cut at MaxRead lines
}

// ReadLog streams the log at path and returns the lines in r, or the last
// tail lines when r is nil. It never holds more than MaxRead lines.
func ReadLog(path string, r *Range, tail int) (Slice, error) {
	f, err := os.Open(path)
	if err != nil {
		return Slice{}, fmt.Errorf("open the log: %w; run `brain run` first, or pass a log path it printed", err)
	}
	defer f.Close()

	out := Slice{Log: path}
	last := newRing(min(tail, MaxRead))
	out.Capped = r == nil && tail > MaxRead
	sp := &splitter{}
	sp.emit = func(text string) {
		out.Total++
		l := Line{N: out.Total, Text: text}
		if r == nil {
			last.push(l)
			return
		}
		if l.N >= r.From && (r.To == 0 || l.N <= r.To) {
			if len(out.Lines) < MaxRead {
				out.Lines = append(out.Lines, l)
			} else {
				out.Capped = true
			}
		}
	}
	if _, err := io.Copy(sp, f); err != nil {
		return Slice{}, fmt.Errorf("read the log: %w", err)
	}
	sp.flush()
	if r == nil {
		out.Lines = last.lines()
	}
	return out, nil
}
