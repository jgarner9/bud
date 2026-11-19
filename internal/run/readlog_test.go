package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLog(t *testing.T, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseRange(t *testing.T) {
	for in, want := range map[string]Range{"3:5": {3, 5}, "3:": {3, 0}, ":5": {1, 5}, "7": {7, 7}, "1:1": {1, 1}} {
		if got, err := ParseRange(in); err != nil || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0:3", "5:3", "a:b", "-1:2", "1:x", ":0"} {
		if _, err := ParseRange(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}

func TestReadLogRange(t *testing.T) {
	p := writeLog(t, 100)
	r, _ := ParseRange("10:12")
	s, err := ReadLog(p, &r, 0)
	if err != nil || s.Total != 100 || len(s.Lines) != 3 || s.Lines[0].N != 10 || s.Lines[2].Text != "line 12" {
		t.Errorf("got %+v, %v", s, err)
	}
	r, _ = ParseRange("99:")
	if s, _ = ReadLog(p, &r, 0); len(s.Lines) != 2 || s.Lines[1].N != 100 {
		t.Errorf("open end: %+v", s)
	}
	r, _ = ParseRange("500:600")
	if s, _ = ReadLog(p, &r, 0); len(s.Lines) != 0 || s.Total != 100 {
		t.Errorf("past the end: %+v", s)
	}
}

func TestReadLogTailAndCap(t *testing.T) {
	p := writeLog(t, MaxRead+500)
	s, _ := ReadLog(p, nil, 3)
	if len(s.Lines) != 3 || s.Lines[2].N != MaxRead+500 {
		t.Errorf("tail: %+v", s.Lines)
	}
	r := Range{From: 1}
	s, _ = ReadLog(p, &r, 0)
	if len(s.Lines) != MaxRead || !s.Capped || s.Total != MaxRead+500 {
		t.Errorf("cap: %d lines, capped %v, total %d", len(s.Lines), s.Capped, s.Total)
	}
}

func TestReadLogMissingFileSaysWhatToDo(t *testing.T) {
	if _, err := ReadLog(filepath.Join(t.TempDir(), "nope.log"), nil, 5); err == nil || !strings.Contains(err.Error(), "bud run") {
		t.Errorf("err = %v", err)
	}
}

func TestReadLogTailAboveTheLimitSaysItWasCapped(t *testing.T) {
	p := writeLog(t, MaxRead+10)
	s, _ := ReadLog(p, nil, MaxRead+5)
	if !s.Capped || len(s.Lines) != MaxRead {
		t.Errorf("capped %v, %d lines", s.Capped, len(s.Lines))
	}
	if s, _ = ReadLog(p, nil, 5); s.Capped {
		t.Error("a small tail is not capped")
	}
	p = writeLog(t, 10)
	if s, _ = ReadLog(p, nil, MaxRead+1); s.Capped {
		t.Error("a tail request above the limit is not capped when no lines are discarded")
	}
}
