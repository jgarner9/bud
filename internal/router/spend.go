package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/registry"
)

// Ledger keeps this calendar month's spend per registry model and the
// month's top-up total (Suite D24, D64). Only the current month is kept; a
// new month starts from zero.
type Ledger struct {
	// Now is the clock; tests set it to move between months
	Now func() time.Time

	mu     sync.Mutex
	path   string // "" keeps the ledger in memory only
	models map[string]registry.Model
	month  string
	spent  map[string]float64
	topUp  float64
}

type ledgerFile struct {
	Month string             `json:"month"`
	Spent map[string]float64 `json:"spent"`
	TopUp float64            `json:"topup"`
}

// NewLedger loads the ledger at path; a missing file is an empty ledger and
// an empty path keeps it in memory.
func NewLedger(path string, models map[string]registry.Model) (*Ledger, error) {
	l := &Ledger{Now: time.Now, path: path, models: models, spent: map[string]float64{}}
	if path == "" {
		return l, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the spend ledger %s: %w", path, err)
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("the spend ledger %s isn't valid JSON (%v); move it aside to start a new one", path, err)
	}
	l.month, l.topUp = f.Month, f.TopUp
	if f.Spent != nil {
		l.spent = f.Spent
	}
	return l, nil
}

// roll drops the old month's numbers when the month has changed
func (l *Ledger) roll() {
	m := l.Now().UTC().Format("2006-01")
	if m != l.month {
		l.month, l.spent, l.topUp = m, map[string]float64{}, 0
	}
}

// Charge adds cost dollars to a model's spend. What goes past its allowance
// (all of it for C2, or for C1 with no allowance) counts as top-up. C0
// models are never charged.
func (l *Ledger) Charge(id string, cost float64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	m := l.models[id]
	if (m.Cost != "C1" && m.Cost != "C2") || cost <= 0 {
		return nil
	}
	before := l.spent[id]
	l.spent[id] = before + cost
	over := cost
	if m.Cost == "C1" {
		left := m.Allowance - before
		if left < 0 {
			left = 0
		}
		over = cost - left
		if over < 0 {
			over = 0
		}
	}
	l.topUp += over
	if l.path == "" {
		return nil
	}
	data, err := json.Marshal(ledgerFile{Month: l.month, Spent: l.spent, TopUp: l.topUp})
	if err != nil {
		return fmt.Errorf("encode the spend ledger: %w", err)
	}
	if err := atomicfile.WriteFile(l.path, data, 0o600); err != nil {
		return fmt.Errorf("write the spend ledger %s: %w", l.path, err)
	}
	return nil
}

// Left is the allowance a C1 model has left this month, never below zero
func (l *Ledger) Left(id string) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	left := l.models[id].Allowance - l.spent[id]
	if left < 0 {
		return 0
	}
	return left
}

// TopUp is this month's top-up total
func (l *Ledger) TopUp() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll()
	return l.topUp
}

// spendsTopUp reports whether serving id would spend top-up
func (l *Ledger) spendsTopUp(id string) bool {
	switch l.models[id].Cost {
	case "C2":
		return true
	case "C1":
		return l.Left(id) <= 0
	}
	return false
}

type usage struct {
	Prompt     float64 `json:"prompt_tokens"`
	Completion float64 `json:"completion_tokens"`
}

// maxUsageBody is how much of a JSON response is kept to find its usage
const maxUsageBody = 10 << 20

// meter passes a response body through unchanged and, when it ends or is
// closed, charges the usage it saw
type meter struct {
	rc     io.ReadCloser
	stream bool
	finish func(*usage)

	buf  bytes.Buffer // JSON body, or the unfinished SSE line
	last *usage
	over bool
	done bool
}

func (m *meter) Read(p []byte) (int, error) {
	n, err := m.rc.Read(p)
	if n > 0 {
		m.feed(p[:n])
	}
	if err != nil {
		m.end()
	}
	return n, err
}

func (m *meter) Close() error {
	m.end()
	return m.rc.Close()
}

func (m *meter) feed(b []byte) {
	if !m.stream {
		if m.over {
			return
		}
		if m.buf.Len()+len(b) > maxUsageBody {
			m.over = true
			m.buf.Reset()
			return
		}
		m.buf.Write(b)
		return
	}
	m.buf.Write(b)
	for {
		i := bytes.IndexByte(m.buf.Bytes(), '\n')
		if i < 0 {
			break
		}
		line := string(m.buf.Next(i + 1))
		m.line(line)
	}
	if m.buf.Len() > maxUsageBody {
		m.buf.Reset()
	}
}

func (m *meter) line(line string) {
	data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
	if !ok {
		return
	}
	data = strings.TrimSpace(data)
	if !strings.Contains(data, `"usage"`) {
		return
	}
	var c struct {
		Usage *usage `json:"usage"`
	}
	if json.Unmarshal([]byte(data), &c) == nil && c.Usage != nil {
		m.last = c.Usage
	}
}

func (m *meter) end() {
	if m.done {
		return
	}
	m.done = true
	if m.stream {
		if m.buf.Len() > 0 {
			m.line(m.buf.String())
		}
	} else if !m.over {
		var c struct {
			Usage *usage `json:"usage"`
		}
		if json.Unmarshal(m.buf.Bytes(), &c) == nil {
			m.last = c.Usage
		}
	}
	m.finish(m.last)
}
