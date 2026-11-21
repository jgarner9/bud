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
	// cap is the router's top-up cap, saved with the ledger so readers such
	// as bud mcp-ops can report it; nil until the router sets it
	cap *float64
	// failed is the last ledger write's error; while it's set no top-up is
	// admitted, since a charge that isn't on disk would be lost on restart
	failed error

	// slot lets one top-up request run at a time, so requests admitted
	// under the cap can't all land past it together
	slot chan struct{}
}

type ledgerFile struct {
	Month string             `json:"month"`
	Spent map[string]float64 `json:"spent"`
	TopUp float64            `json:"topup"`
	// Cap is the top-up cap of the router that last wrote the file
	Cap *float64 `json:"cap,omitempty"`
}

// NewLedger loads the ledger at path; a missing file is an empty ledger and
// an empty path keeps it in memory.
func NewLedger(path string, models map[string]registry.Model) (*Ledger, error) {
	l := &Ledger{Now: time.Now, path: path, models: models, spent: map[string]float64{}, slot: make(chan struct{}, 1)}
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

// setCap records the cap the router runs with, written with the next charge
func (l *Ledger) setCap(c float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cap = &c
}

// Spend is what a spend ledger file says about the month at a given time
type Spend struct {
	// Month is the UTC calendar month, as 2006-01
	Month string
	// Recorded is false when the file doesn't exist yet
	Recorded bool
	// Spent is each model's dollars this month; models with none are absent
	Spent map[string]float64
	// TopUp is this month's top-up total
	TopUp float64
	// Cap is the top-up cap of the router that last wrote the file; nil
	// when no router that records it has written it yet
	Cap *float64
}

// ReadSpend reads the ledger at path without changing it, the way the
// router would see it at now: a file from an earlier month reads as an
// empty month. A missing file reads as an empty month with Recorded false
func ReadSpend(path string, now time.Time) (Spend, error) {
	out := Spend{Month: now.UTC().Format("2006-01"), Spent: map[string]float64{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return Spend{}, fmt.Errorf("read the spend ledger %s: %w", path, err)
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return Spend{}, fmt.Errorf("the spend ledger %s isn't valid JSON (%v)", path, err)
	}
	out.Recorded, out.Cap = true, f.Cap
	if f.Month == out.Month {
		out.TopUp = f.TopUp
		for id, v := range f.Spent {
			if v > 0 {
				out.Spent[id] = v
			}
		}
	}
	return out, nil
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
	data, err := json.Marshal(ledgerFile{Month: l.month, Spent: l.spent, TopUp: l.topUp, Cap: l.cap})
	if err != nil {
		return fmt.Errorf("encode the spend ledger: %w", err)
	}
	if err := atomicfile.WriteFile(l.path, data, 0o600); err != nil {
		l.failed = fmt.Errorf("write the spend ledger %s: %w", l.path, err)
		return l.failed
	}
	l.failed = nil
	return nil
}

// errTopUpClosed says why no top-up may be spent right now
type errTopUpClosed struct{ reason string }

func (e errTopUpClosed) Error() string { return e.reason }

// admitTopUp waits for the top-up slot, then admits the request if the
// month is under limit and the ledger's last write worked. release frees the
// slot; call it once the response has been charged
func (l *Ledger) admitTopUp(done <-chan struct{}, limit float64) (release func(), err error) {
	select {
	case l.slot <- struct{}{}:
	case <-done:
		return nil, errTopUpClosed{"the request ended while waiting for another top-up request to finish"}
	}
	var once sync.Once
	release = func() { once.Do(func() { <-l.slot }) }
	l.mu.Lock()
	l.roll()
	failed, total := l.failed, l.topUp
	l.mu.Unlock()
	if failed != nil {
		release()
		return nil, errTopUpClosed{"the spend ledger couldn't be saved, so no top-up is spent until it can; check the router's log"}
	}
	if total >= limit {
		release()
		return nil, errTopUpClosed{""}
	}
	return release, nil
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

// usageTail is how much of an oversized JSON response is kept to find usage
const usageTail = 64 << 10

// usageInTail finds the last "usage" object in the tail of a JSON body
func usageInTail(b []byte) *usage {
	i := bytes.LastIndex(b, []byte(`"usage"`))
	if i < 0 {
		return nil
	}
	rest := bytes.TrimLeft(b[i+len(`"usage"`):], " \t\r\n")
	rest, ok := bytes.CutPrefix(rest, []byte(":"))
	if !ok {
		return nil
	}
	var u usage
	if json.NewDecoder(bytes.NewReader(rest)).Decode(&u) != nil {
		return nil
	}
	return &u
}

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
		m.buf.Write(b)
		if m.buf.Len() > maxUsageBody {
			// keep only the tail, where usage comes after the choices or data
			m.over = true
			tail := append([]byte(nil), m.buf.Bytes()[m.buf.Len()-usageTail:]...)
			m.buf.Reset()
			m.buf.Write(tail)
		}
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
	} else if m.over {
		m.last = usageInTail(m.buf.Bytes())
	} else {
		var c struct {
			Usage *usage `json:"usage"`
		}
		if json.Unmarshal(m.buf.Bytes(), &c) == nil {
			m.last = c.Usage
		}
	}
	m.finish(m.last)
}
