package mcpgateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is one line of the audit log. Argument values are left out, since
// they can hold private text or keys; their names say what was asked
type Record struct {
	Time   time.Time `json:"time"`
	Server string    `json:"server"`
	Tier   string    `json:"tier"`
	// Raised names the privacy rule that raised Tier, if one did
	Raised   string   `json:"raised,omitempty"`
	Client   string   `json:"client"`
	Method   string   `json:"method"`
	Tool     string   `json:"tool,omitempty"`
	Args     []string `json:"args,omitempty"`
	Decision string   `json:"decision"`
	Status   int      `json:"status"`
	Millis   int64    `json:"ms"`
}

// Audit appends records to a JSONL file, one write per line, so lines from
// concurrent calls never interleave. The shared audit log (JGA-348) takes
// over these records once it exists
type Audit struct {
	mu sync.Mutex
	f  *os.File
}

// OpenAudit opens (or creates, mode 0600) the log at path for appending
func OpenAudit(path string) (*Audit, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("make the audit log's dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the audit log: %w", err)
	}
	// the mode above applies only to a new file; an existing or rotated
	// one is made private too
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("make the audit log private (chmod 600 %s): %w", path, err)
	}
	return &Audit{f: f}, nil
}

// Write appends one record
func (a *Audit) Write(r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err = a.f.Write(append(b, '\n'))
	return err
}

// Close closes the file
func (a *Audit) Close() error { return a.f.Close() }
