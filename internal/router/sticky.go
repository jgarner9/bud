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
// SHA-256 hashes, so the file holds no conversation id in plain text.
//
// When the file can't be written, every request that carries a
// conversation id is held at P0 until a write succeeds, since a restart
// would otherwise forget a raised conversation
type sticky struct {
	mu    sync.Mutex
	path  string // "" keeps it in memory only
	now   func() time.Time
	until map[string]time.Time
	logf  func(string, ...any)
	// failed is set while the last write failed
	failed bool
}

// stickyRefresh is how much later a held conversation's expiry must move
// before it's written again, so a busy conversation's requests don't each
// write the file
const stickyRefresh = 24 * time.Hour

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

// decide is the one step that reads and updates the list, under one lock:
// raised says a rule or the provenance check raised this request to P0, and
// it then holds the conversation for stickyTTL from now. held says the
// conversation was already held (or the list can't be saved), so the
// request is P0 too. A request with no conversation id is never held
func (s *sticky) decide(conv string, raised bool) (held bool) {
	if conv == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	key := conversationKey(conv)
	t, ok := s.until[key]
	held = (ok && now.Before(t)) || s.failed
	if !raised && !held {
		return false
	}
	next := now.Add(stickyTTL)
	if ok && now.Before(t) && next.Sub(t) < stickyRefresh && !s.failed {
		return held // written recently enough
	}
	s.until[key] = next
	s.save(now)

	return held
}

// save drops the expired entries and writes the list. A failed write holds
// every conversation at P0 until one succeeds
func (s *sticky) save(now time.Time) {
	for k, t := range s.until {
		if !now.Before(t) {
			delete(s.until, k)
		}
	}
	if s.path == "" {
		return
	}
	b, err := json.Marshal(stickyFile{Conversations: s.until})
	if err == nil {
		err = atomicfile.WriteFile(s.path, b, 0o600)
	}
	if err != nil {
		if !s.failed {
			s.logf("router: couldn't save the P0 conversations to %s, so every conversation stays P0 until it can: %v", s.path, err)
		}
		s.failed = true
		return
	}
	if s.failed {
		s.logf("router: the P0 conversations are saved again")
	}
	s.failed = false
}
