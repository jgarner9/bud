// Package audit is the shared, append-only audit log (Suite D140): one JSONL
// file every Bud-owned leaf (the MCP gateway, the ops server, the router,
// mail and memory-review) appends to. It records what happened, never the
// private content of a call: no argument values, bodies or keys.
//
// The log is a record, not state. Code that needs a record back to enforce
// something (the approval store, the router's spend ledger, mail's restore
// log) keeps its own file and emits an event here beside it.
package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// The leaves that write the log
const (
	SourceGateway      = "gateway"
	SourceOps          = "ops"
	SourceRouter       = "router"
	SourceMail         = "mail"
	SourceMemoryReview = "memory-review"
	SourceImprove      = "improve"
	// SourceSecrets is bud secrets run, which gives a command named secrets
	SourceSecrets = "secrets"
)

// The kinds of event
const (
	// KindToolCall is a call the MCP gateway forwarded or refused
	KindToolCall = "tool_call"
	// KindApproval is an approval-tier action asked for, decided or run
	KindApproval = "approval"
	// KindSpend is money the router spent on a request
	KindSpend = "spend"
	// KindMailAction is a mail move, restore or send
	KindMailAction = "mail_action"
	// KindMailIngest is one run of bud mail-ingest, with counts only
	KindMailIngest = "mail_ingest"
	// KindSecretRun is a command run with secrets, or refused them
	KindSecretRun = "secret_run"
)

// maxEvent is the largest line Write accepts. One write per line only stays
// whole between processes while the line is small
const maxEvent = 32 << 10

// Event is one line of the log
type Event struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Kind   string    `json:"kind"`
	// Tier is the privacy tier the event ran at (P0, P1, P2), if it has one
	Tier     string `json:"tier,omitempty"`
	Actor    string `json:"actor,omitempty"`
	Action   string `json:"action,omitempty"`
	Target   string `json:"target,omitempty"`
	Decision string `json:"decision,omitempty"`
	// Detail holds the event's own fields. Callers must leave out private
	// values: names and counts are fine, text and keys are not
	Detail map[string]any `json:"detail,omitempty"`
}

// Log appends events to one file. A nil *Log drops events, so a leaf with no
// log configured needs no checks
type Log struct {
	path string
	now  func() time.Time
}

// Open makes the log's directory (0700) and its file (0600), or tightens
// them if they exist, so a log that can't be written fails here and not at
// the first event
func Open(path string) (*Log, error) {
	if path == "" {
		return nil, errors.New("the audit log needs a path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("make the audit log's directory: %w", err)
	}
	// MkdirAll leaves an existing directory's mode alone
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("make the audit log's directory private (chmod 700 %s): %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the audit log: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("make the audit log private (chmod 600 %s): %w", path, err)
	}
	return &Log{path: path, now: time.Now}, nil
}

// Path is where the log is, or "" for a nil log
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Write appends one event as a single write, so lines from concurrent calls
// and processes never interleave. It stamps a zero time and refuses an event
// with no source or kind, which no reader could place
func (l *Log) Write(e Event) error {
	if l == nil {
		return nil
	}
	if e.Source == "" || e.Kind == "" {
		return errors.New("an audit event needs a source and a kind")
	}
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	e.Time = e.Time.UTC()
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode the audit event: %w", err)
	}
	if len(b) >= maxEvent {
		return fmt.Errorf("the audit event is %d bytes, over the %d byte limit; keep detail to names and counts", len(b), maxEvent)
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open the audit log: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("make the audit log private (chmod 600 %s): %w", l.path, err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write the audit log: %w", err)
	}
	return f.Close()
}

// Record is Write for a caller whose own work has already happened or must
// not depend on the log: a failure is logged and dropped, never returned
func (l *Log) Record(e Event) {
	if err := l.Write(e); err != nil {
		log.Printf("audit: %v", err)
	}
}

// Read returns every event in the file at path, oldest first, skipping lines
// it can't parse. A missing file is an empty log
func Read(path string) ([]Event, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open the audit log: %w", err)
	}
	defer f.Close()
	var out []Event
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var e Event
			if json.Unmarshal(line, &e) == nil && e.Source != "" && e.Kind != "" {
				out = append(out, e)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read the audit log: %w", err)
		}
	}
	return out, nil
}
