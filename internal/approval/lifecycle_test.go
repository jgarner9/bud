package approval

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBindingAndReplay(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := New("1", "session", "send", "target", []byte("message"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Decide(Approved, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ session, action, target, args string }{
		{"other", "send", "target", "message"},
		{"session", "other", "target", "message"},
		{"session", "send", "other", "message"},
		{"session", "send", "target", "changed"},
	} {
		if r.Consume(tc.session, tc.action, tc.target, []byte(tc.args), now) == nil {
			t.Fatal("changed binding accepted")
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var restored Request
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Consume("session", "send", "target", []byte("message"), now); err != nil {
		t.Fatal(err)
	}
	if restored.Consume("session", "send", "target", []byte("message"), now) == nil {
		t.Fatal("replay accepted")
	}
}

func TestExpiryAndTerminalDecisions(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, state := range []State{Denied, Canceled} {
		r, _ := New("1", "s", "a", "t", nil, now)
		if err := r.Decide(state, now); err != nil {
			t.Fatal(err)
		}
		if r.Decide(Approved, now) == nil {
			t.Fatal("terminal decision changed")
		}
	}
	r, _ := New("1", "s", "a", "t", nil, now)
	if r.Decide(Approved, now.Add(time.Hour)) == nil || r.State != Expired {
		t.Fatal("expired request accepted")
	}
	r, _ = New("1", "s", "a", "t", nil, now)
	_ = r.Decide(Approved, now)
	if r.Consume("s", "a", "t", nil, now.Add(time.Hour)) == nil || r.State != Expired {
		t.Fatal("expired approval consumed")
	}
}
