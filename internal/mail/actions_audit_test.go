package mail

import (
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/audit"
)

func TestActionsAlsoGoToTheSharedAuditLogWithoutTheirContent(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLog(filepath.Join(dir, "mail-actions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Audit, err = audit.Open(filepath.Join(dir, "audit.jsonl")); err != nil {
		t.Fatal(err)
	}
	err = l.Append(
		Action{Action: "trash", Folder: "INBOX", ToFolder: "Deleted Messages", MessageID: "<a@x>", From: []string{"boss@example.com"}, Subject: "secret subject"},
		Action{Action: "send", Recipients: []string{"a@example.com", "b@example.com"}, Subject: "secret subject"},
	)
	if err != nil {
		t.Fatal(err)
	}
	events, err := audit.Read(l.Audit.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events", len(events))
	}
	if e := events[0]; e.Source != audit.SourceMail || e.Kind != audit.KindMailAction || e.Action != "trash" || e.Detail["folder"] != "INBOX" || e.Detail["to_folder"] != "Deleted Messages" {
		t.Errorf("trash event %+v", e)
	}
	if events[1].Action != "send" || events[1].Detail["recipients"] != float64(2) {
		t.Errorf("send event %+v", events[1])
	}
	for _, e := range events {
		b := strings.ToLower(e.Action + e.Target)
		for k, v := range e.Detail {
			b += k
			if s, ok := v.(string); ok {
				b += s
			}
		}
		if strings.Contains(b, "secret") || strings.Contains(b, "example.com") || strings.Contains(b, "<a@x>") {
			t.Errorf("event holds message content: %+v", e)
		}
	}
}
