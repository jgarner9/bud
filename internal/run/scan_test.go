package run

import (
	"strings"
	"testing"
)

func splitAll(chunks ...string) []string {
	var got []string
	s := &splitter{emit: func(t string) { got = append(got, t) }}
	for _, c := range chunks {
		s.Write([]byte(c))
	}
	s.flush()
	return got
}

func TestSplitterCleansAcrossWriteBoundaries(t *testing.T) {
	whole := "a\033[1;31mred\033[0m\n10%\r50%\r100%\r\nwin\r\nlast"
	want := "ared|100%|win|last"
	if got := strings.Join(splitAll(whole), "|"); got != want {
		t.Errorf("whole: %q, want %q", got, want)
	}
	// The same bytes one at a time: escapes and \r\n may be split anywhere.
	var bytewise []string
	for i := range whole {
		bytewise = append(bytewise, whole[i:i+1])
	}
	if got := strings.Join(splitAll(bytewise...), "|"); got != want {
		t.Errorf("bytewise: %q, want %q", got, want)
	}
}

func TestSplitterCutsTheCleanedText(t *testing.T) {
	got := splitAll(strings.Repeat("\033[31m", 400) + strings.Repeat("y", 600) + "\n")
	if len(got) != 1 || len(got[0]) != maxLine+len("…") || !strings.HasSuffix(got[0], "…") || strings.Contains(got[0], "\x1b") {
		t.Errorf("got %d bytes: %q", len(got[0]), got[0][:20])
	}
}

func TestSplitterTreatsMalformedEscapesAsText(t *testing.T) {
	if got := splitAll("a\x1bb\x1b[3\nc"); strings.Join(got, "|") != "ab|c" {
		t.Errorf("got %q", got)
	}
}

func TestSplitterDropsABrokenRuneAtTheCut(t *testing.T) {
	got := splitAll(strings.Repeat("x", maxLine-1) + "é" + "\n") // é straddles the cut
	if len(got) != 1 || !strings.HasSuffix(got[0], strings.Repeat("x", maxLine-1)+"…") {
		t.Errorf("got %q", got[0][len(got[0])-10:])
	}
}
