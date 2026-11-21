package mail

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Action is one line of the action log: a move to the trash, a restore or
// a send. It names messages by Message-ID and never holds a body
type Action struct {
	Time       time.Time `json:"time"`
	Action     string    `json:"action"`              // trash, restore, send
	Folder     string    `json:"folder,omitempty"`    // where the message was
	ToFolder   string    `json:"to_folder,omitempty"` // where it went
	MessageID  string    `json:"message_id,omitempty"`
	From       []string  `json:"from,omitempty"`
	Recipients []string  `json:"recipients,omitempty"`
	Subject    string    `json:"subject,omitempty"`
}

// Log is the append-only action log (paths.MailActions), mode 0600
type Log struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
}

// NewLog opens the log at path, making its directory
func NewLog(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("make the mail action log's directory: %w", err)
	}
	// a log from before, or one made by hand, is private from the start,
	// not only once something is appended
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("set the mail action log to 0600: %w", err)
	}
	return &Log{path: path, now: time.Now}, nil
}

// Append writes actions, stamping any without a time
func (l *Log) Append(acts ...Action) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open the mail action log: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("set the mail action log to 0600: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, a := range acts {
		if a.Time.IsZero() {
			a.Time = l.now().UTC()
		}
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("write the mail action log: %w", err)
	}
	return nil
}

// Read returns every action, oldest first, skipping lines it can't parse
func (l *Log) Read() ([]Action, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the mail action log: %w", err)
	}
	defer f.Close()
	var out []Action
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var a Action
		if json.Unmarshal(sc.Bytes(), &a) == nil {
			out = append(out, a)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the mail action log: %w", err)
	}
	return out, nil
}
