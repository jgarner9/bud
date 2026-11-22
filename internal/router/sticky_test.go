package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/registry"
)

const (
	calendarChat = `{"model":"bud/manager","messages":[{"role":"user","content":"what's on today"},{"role":"assistant","content":null,"tool_calls":[{"id":"1","type":"function","function":{"name":"mcp__calendar__list_events","arguments":"{}"}}]},{"role":"tool","tool_call_id":"1","content":"dentist at 3"}]}`
	// what Hermes's compression sends: the same conversation as plain text
	compressed = `{"model":"bud/manager","messages":[{"role":"user","content":"Summarize: [TOOL RESULT] dentist at 3"}]}`
)

func session(id string) map[string]string {
	return map[string]string{"X-Bud-Privacy": "P1", SessionHeader: id}
}

// Suite D117, D119: once a request of a conversation is raised, the rest of
// it stays P0 even when the evidence is gone, as in a compression call;
// another conversation isn't held
func TestARaisedConversationStaysP0WithoutTheEvidence(t *testing.T) {
	rig := newProvRig(t)
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", compressed, session("c1")); w.Code != 200 || rig.cloudBytes == 0 {
		t.Fatalf("before any raise: code %d, cloud bytes %d", w.Code, rig.cloudBytes)
	}
	rig.cloudBytes = 0
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", calendarChat, session("c1")); w.Header().Get(RaisedHeader) != ProvenanceRule {
		t.Fatalf("raised %q", w.Header().Get(RaisedHeader))
	}
	w := do(t, rig.rt, "POST", "/v1/chat/completions", compressed, session("c1"))
	if w.Code != 200 || w.Header().Get(RaisedHeader) != ConversationRule || rig.cloudBytes != 0 {
		t.Fatalf("after: code %d, raised %q, cloud bytes %d", w.Code, w.Header().Get(RaisedHeader), rig.cloudBytes)
	}
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", compressed, session("c2")); w.Header().Get(RaisedHeader) != "" || rig.cloudBytes == 0 {
		t.Fatalf("another conversation: raised %q, cloud bytes %d", w.Header().Get(RaisedHeader), rig.cloudBytes)
	}
	rig.cloudBytes = 0
	if w := do(t, rig.rt, "POST", "/v1/chat/completions", compressed, map[string]string{"X-Bud-Privacy": "P1"}); w.Header().Get(RaisedHeader) != "" || rig.cloudBytes == 0 {
		t.Fatalf("no conversation id: raised %q, cloud bytes %d", w.Header().Get(RaisedHeader), rig.cloudBytes)
	}
}

func TestAMarkerRaiseIsStickyToo(t *testing.T) {
	rig := newProvRig(t)
	rig.rt.rules = mustRules(t, `(?i)^\s*#private\b`)
	do(t, rig.rt, "POST", "/v1/chat/completions", `{"model":"bud/manager","messages":[{"role":"user","content":"#private my balance"}]}`, session("c1"))
	rig.cloudBytes = 0
	w := do(t, rig.rt, "POST", "/v1/chat/completions", compressed, session("c1"))
	if w.Header().Get(RaisedHeader) != ConversationRule || rig.cloudBytes != 0 {
		t.Fatalf("raised %q, cloud bytes %d", w.Header().Get(RaisedHeader), rig.cloudBytes)
	}
}

func TestTheHeldConversationsSurviveARestartAndExpire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conv.json")
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	rig := newProvRigWith(t, Options{StickyPath: path, Now: clock})
	do(t, rig.rt, "POST", "/v1/chat/completions", calendarChat, session("secret-conversation-id"))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret-conversation-id") {
		t.Fatalf("the file names the conversation: %s", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}

	again := newProvRigWith(t, Options{StickyPath: path, Now: clock})
	if w := do(t, again.rt, "POST", "/v1/chat/completions", compressed, session("secret-conversation-id")); w.Header().Get(RaisedHeader) != ConversationRule || again.cloudBytes != 0 {
		t.Fatalf("after a restart: raised %q, cloud bytes %d", w.Header().Get(RaisedHeader), again.cloudBytes)
	}
	now = now.Add(stickyTTL + time.Second)
	if w := do(t, again.rt, "POST", "/v1/chat/completions", compressed, session("secret-conversation-id")); w.Header().Get(RaisedHeader) != "" || again.cloudBytes == 0 {
		t.Fatalf("after the TTL: raised %q, cloud bytes %d", w.Header().Get(RaisedHeader), again.cloudBytes)
	}
}

func TestADamagedConversationsFileStopsTheRouter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conv.json")
	for _, bad := range []string{`nope`, `{}`} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := New(tierRegistry("http://127.0.0.1:1"), env(nil), Options{StickyPath: path})
		if err == nil || !strings.Contains(err.Error(), "damaged") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

func mustRules(t *testing.T, marker string) []registry.CompiledRule {
	t.Helper()
	reg := &registry.Registry{PrivacyRules: map[string]registry.PrivacyRule{"marker": {Match: marker, Tier: "P0"}}}
	rules, err := reg.CompileRules()
	if err != nil {
		t.Fatal(err)
	}
	return rules
}
