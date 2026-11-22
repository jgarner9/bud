package router

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
)

// ConversationRule is the RaisedHeader value for a request raised because an
// earlier request of the same conversation was (Suite D117, D119)
const ConversationRule = "conversation"

// stickyTTL is how long a conversation stays P0 after the last request of
// it that was raised: long enough for a Hermes chat that goes quiet for
// days, short enough that the file doesn't grow without end
const stickyTTL = 30 * 24 * time.Hour

// sticky remembers the conversations a rule or the provenance check raised
// to P0, by the conversation id Hermes sends (SessionHeader), so the rest
// of the conversation stays P0 even once the evidence has left the
// request. Hermes's context compression and title calls send the
// conversation as plain text, without the tool_calls the provenance check
// reads, and a compressed history no longer holds them either; the id is
// what still ties those requests to the calendar result. Ids are kept as
// SHA-256 hashes, so the file names no conversation
type sticky struct {
	mu    sync.Mutex
	path  string // "" keeps it in memory only
	now   func() time.Time
	until map[string]time.Time
	logf  func(string, ...any)
}

type stickyFile struct {
	Conversations map[string]time.Time `json:"conversations"`
}

// openSticky reads the file at path; a missing file is an empty list. A
// damaged one is an error, since starting without it could send a P0
// conversation's next request to the cloud
func openSticky(path string, now func() time.Time, logf func(string, ...any)) (*sticky, error) {
	s := &sticky{path: path, now: now, until: map[string]time.Time{}, logf: logf}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the P0 conversations: %w", err)
	}
	var f stickyFile
	if err := json.Unmarshal(b, &f); err != nil || f.Conversations == nil {
		return nil, fmt.Errorf("%s is damaged (%v); it lists the conversations that must stay local, so check it, then delete it to start an empty list", path, err)
	}
	s.until = f.Conversations

	return s, nil
}

func conversationKey(conv string) string {
	h := sha256.Sum256([]byte(conv))
	return hex.EncodeToString(h[:])
}

// has says whether the conversation is still held at P0
func (s *sticky) has(conv string) bool {
	if conv == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.until[conversationKey(conv)]

	return ok && s.now().Before(t)
}

// mark holds the conversation at P0 for stickyTTL from now, drops the
// expired ones and saves the list. A failed save is logged: the list in
// memory still holds it until the router restarts
func (s *sticky) mark(conv string) {
	if conv == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, t := range s.until {
		if !now.Before(t) {
			delete(s.until, k)
		}
	}
	s.until[conversationKey(conv)] = now.Add(stickyTTL)
	if s.path == "" {
		return
	}
	b, err := json.Marshal(stickyFile{Conversations: s.until})
	if err == nil {
		err = atomicfile.WriteFile(s.path, b, 0o600)
	}
	if err != nil {
		s.logf("router: couldn't save the P0 conversations to %s, so a restart would forget them: %v", s.path, err)
	}
}
