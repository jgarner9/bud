// Package mailingest summarizes new mail without giving the model anything
// to act with (Suite D106, JGA-317). Fetching is plain code (internal/mail's
// Fresh), the summarizer is a local model asked for text and sent no tools,
// and the result is appended to a store that only a read-only tool reads
package mailingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/mail"
)

// Summary is one stored line; see mail.StoredSummary for what is trusted
type Summary = mail.StoredSummary

// The caps on stored text, so one message can't fill the store
const (
	maxSummary = 1200
	maxSubject = 300
	maxFrom    = 200
	maxFroms   = 5
	maxError   = 200
)

// Clean makes text safe to store as one value: valid UTF-8, no control
// characters but a line break between sentences and a space, cut to n runes
func Clean(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || bidi(r):
			// dropped: an escape sequence or a bidi override is not text
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > n {
		out = string([]rune(out)[:n]) + "…"
	}
	return out
}

// bidi is a character that changes the direction text is shown in, which
// can make a summary read differently from what it holds
func bidi(r rune) bool {
	return r == '\u061c' || r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// a Store is what the mail server's list_summaries reads
var _ mail.SummarySource = (*Store)(nil)

// Store is the append-only file of summaries
type Store struct {
	path string
	mu   sync.Mutex
}

// ErrNotStarted says the store's file isn't there: bud mail-ingest hasn't
// run, so an empty answer would pass for "no new mail"
var ErrNotStarted = errors.New("mail ingestion hasn't run yet: run bud mail-ingest (server/mail-mcp/ has its timer), which creates the store")

// OpenStore opens the store to read it, as the mail server does. It makes
// and changes nothing, so a store that isn't there yet stays not there
func OpenStore(path string) *Store { return &Store{path: path} }

// NewStore makes the store, as bud mail-ingest does: its directory, mode
// 0700, and an empty file, mode 0600, so a reader can tell a store that has
// no summaries yet from one that was never started
func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("make the mail summaries' directory: %w", err)
	}
	// MkdirAll keeps the mode of a directory that exists, so tighten it: the
	// store holds P0 mail
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("set the mail summaries' directory to 0700: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("make the mail summaries: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("set the mail summaries to 0600: %w", err)
	}
	return &Store{path: path}, nil
}

// Append writes one line. It makes the fields safe and the file 0600
func (st *Store) Append(s Summary) error {
	s.Summary = Clean(s.Summary, maxSummary)
	s.Subject = Clean(s.Subject, maxSubject)
	s.Error = Clean(s.Error, maxError)
	var from []string
	for _, f := range s.From {
		if len(from) < maxFroms {
			from = append(from, Clean(f, maxFrom))
		}
	}
	s.From = from
	line, err := json.Marshal(s)
	if err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	f, err := os.OpenFile(st.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open the mail summaries: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("set the mail summaries to 0600: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write the mail summaries: %w", err)
	}
	return f.Sync()
}

// All reads every line, oldest first. A line that doesn't parse (a torn
// last line while the ingest is writing) is skipped
func (st *Store) All() ([]Summary, error) {
	data, err := os.ReadFile(st.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the mail summaries: %w", err)
	}
	var out []Summary
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var s Summary
		if json.Unmarshal(sc.Bytes(), &s) == nil && s.Folder != "" {
			out = append(out, s)
		}
	}
	return out, sc.Err()
}

// Has returns the keys of everything stored, for the idempotence check
func (st *Store) Has() (map[string]bool, error) {
	all, err := st.All()
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(all))
	for _, s := range all {
		out[s.Key()] = true
	}
	return out, nil
}

// Newest returns up to limit summaries received at or after since, newest
// first (by when they were received, then uid), and how many matched
func (st *Store) Newest(since time.Time, limit int) ([]Summary, int, error) {
	if _, err := os.Stat(st.path); errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotStarted
	}
	all, err := st.All()
	if err != nil {
		return nil, 0, err
	}
	all = slices.DeleteFunc(all, func(s Summary) bool { return s.Received.Before(since) })
	slices.SortStableFunc(all, func(a, b Summary) int {
		if c := b.Received.Compare(a.Received); c != 0 {
			return c
		}
		return int(b.UID) - int(a.UID)
	})
	total := len(all)
	if len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

// State is how far each folder has been handled
type State struct {
	Folders map[string]mail.Mark `json:"folders"`
}

// LoadState reads the state file; a missing one is empty
func LoadState(path string) (State, error) {
	st := State{Folders: map[string]mail.Mark{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("read the mail ingest state: %w", err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("the mail ingest state %s isn't valid JSON (%w): fix or remove it, and the next run starts each folder afresh without summarizing old mail", path, err)
	}
	if st.Folders == nil {
		st.Folders = map[string]mail.Mark{}
	}
	return st, nil
}

// Save writes the state atomically, mode 0600
func (s State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, append(data, '\n'), 0o600)
}
