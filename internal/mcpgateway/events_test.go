package mcpgateway

import (
	"os"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/audit"
)

func TestACallWritesASharedAuditEventWithoutItsArgumentValues(t *testing.T) {
	up, _ := fakeUpstream(t)
	g, _ := newGateway(t, up.URL, "P1", "P2")
	shared := g.events
	if w := call(g, "P1", toolCall); w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	events, err := audit.Read(shared.Path())
	if err != nil || len(events) != 1 {
		t.Fatalf("got %+v, %v", events, err)
	}
	e := events[0]
	if e.Source != audit.SourceGateway || e.Kind != audit.KindToolCall || e.Tier != "P1" || e.Action != "mcp:linear/create_issue" || e.Target != "linear" || e.Decision != "allowed" {
		t.Errorf("event %+v", e)
	}
	if args, _ := e.Detail["args"].([]any); len(args) != 2 || args[0] != "team" || args[1] != "title" {
		t.Errorf("args %+v", e.Detail["args"])
	}
	raw, err := os.ReadFile(shared.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private words") {
		t.Fatalf("an argument value reached the shared log: %s", raw)
	}
}
