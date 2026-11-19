package run

import (
	"io"
	"strings"
)

// splitter turns a byte stream into lines without holding more than maxLine
// bytes of any one line, so a huge or newline-free output costs no memory.
// It cleans as the bytes arrive, so the cut is made on the cleaned text:
// colour codes (ESC [ ... final) are dropped and a carriage return not
// followed by a newline starts the line over, so a progress bar keeps only its
// final state.
type splitter struct {
	emit  func(text string)
	cur   []byte
	extra bool // the current line was cut at maxLine
	open  bool // bytes have arrived since the last newline
	cr    bool // the last byte was \r; a newline next keeps the line, anything else restarts it
	esc   int  // 0 plain, 1 after ESC, 2 inside ESC [ ...
}

func (s *splitter) Write(p []byte) (int, error) {
	for _, b := range p {
		s.put(b)
	}
	return len(p), nil
}

func (s *splitter) put(b byte) {
	s.open = true
	switch s.esc {
	case 1:
		if b == '[' {
			s.esc = 2
			return
		}
		s.esc = 0 // a lone ESC is dropped; b is an ordinary byte
	case 2:
		switch {
		case b >= 0x20 && b <= 0x3f: // parameters and intermediates
			return
		case b >= 0x40 && b <= 0x7e: // the final byte ends the sequence
			s.esc = 0
			return
		}
		s.esc = 0 // malformed: b is an ordinary byte
	}
	switch b {
	case '\n':
		s.endLine()
	case '\r':
		s.cr = true
	case 0x1b:
		s.cr = s.restart()
		s.esc = 1
	default:
		s.cr = s.restart()
		if len(s.cur) < maxLine {
			s.cur = append(s.cur, b)
		} else {
			s.extra = true
		}
	}
}

// restart drops the line so far if a carriage return came before; it returns
// false, the new value of cr.
func (s *splitter) restart() bool {
	if s.cr {
		s.cur, s.extra = s.cur[:0], false
	}
	return false
}

// flush emits a last line that had no newline.
func (s *splitter) flush() {
	if s.open {
		s.endLine()
	}
}

func (s *splitter) endLine() {
	text := strings.ToValidUTF8(string(s.cur), "") // a cut may split a rune
	if s.extra {
		text += "…"
	}
	s.cur, s.extra, s.open, s.cr, s.esc = s.cur[:0], false, false, false, 0
	s.emit(text)
}

// ring keeps the last n lines.
type ring struct {
	buf   []Line
	start int
	n     int
}

func newRing(n int) *ring {
	if n < 0 {
		n = 0
	}
	return &ring{buf: make([]Line, n)}
}

func (r *ring) push(l Line) {
	if len(r.buf) == 0 {
		return
	}
	if r.n < len(r.buf) {
		r.buf[(r.start+r.n)%len(r.buf)] = l
		r.n++
		return
	}
	r.buf[r.start] = l
	r.start = (r.start + 1) % len(r.buf)
}

func (r *ring) lines() []Line {
	out := make([]Line, 0, r.n)
	for i := 0; i < r.n; i++ {
		out = append(out, r.buf[(r.start+i)%len(r.buf)])
	}
	return out
}

// scanner is the writer the command's output goes to. It copies every byte to
// the log and watches the lines for failures.
type scanner struct {
	splitter
	log    io.Writer
	logErr error // the first log write error; output keeps being read

	lines   int
	tail    *ring
	before  *ring // the last few lines, for context ahead of a failure
	fails   []Line
	matches int
	shown   int // failure lines kept (not counting context)
	after   int // context lines still owed after the last failure
	lastN   int // the last line number put in fails
	maxFail int
	context int
}

func newScanner(log io.Writer, o Options) *scanner {
	s := &scanner{log: log, tail: newRing(o.Tail), before: newRing(o.Context), maxFail: o.MaxFailures, context: o.Context}
	s.emit = s.line
	return s
}

func (s *scanner) Write(p []byte) (int, error) {
	if s.logErr == nil {
		if _, err := s.log.Write(p); err != nil {
			s.logErr = err // keep draining the pipe so the command isn't blocked
		}
	}
	return s.splitter.Write(p)
}

func (s *scanner) finish() { s.flush() }

func (s *scanner) line(text string) {
	s.lines++
	l := Line{N: s.lines, Text: text}
	s.tail.push(l)
	match := pattern.MatchString(text)
	if match {
		s.matches++
	}
	switch {
	case match && s.shown < s.maxFail:
		for _, b := range s.before.lines() {
			if b.N > s.lastN {
				s.fails = append(s.fails, b)
			}
		}
		l.Match = true
		s.fails = append(s.fails, l)
		s.shown++
		s.lastN, s.after = l.N, s.context
	case s.after > 0:
		s.fails = append(s.fails, l)
		s.lastN = l.N
		s.after--
	}
	s.before.push(l)
}
