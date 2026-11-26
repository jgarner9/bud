package router

import (
	"path/filepath"
	"testing"
	"time"

	"jgarner.dev/bud/internal/audit"
)

func TestAChargedResponseWritesASpendEvent(t *testing.T) {
	up := jsonUpstream(`{"prompt_tokens":1000000,"completion_tokens":500000}`)
	defer up.Close()
	reg := spendRegistry(up.URL)
	l, err := NewLedger("", reg.Models)
	if err != nil {
		t.Fatal(err)
	}
	l.Now = (&clock{time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}).now
	log, err := audit.Open(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := New(reg, env(nil), Options{Ledger: l, TopUpCap: 10, Logf: func(string, ...any) {}, Audit: log})
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, rt, "POST", "/v1/chat/completions", chatBody("inc1"), nil); w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	// a local model costs nothing and leaves no event
	if w := do(t, rt, "POST", "/v1/chat/completions", chatBody("local"), nil); w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	events, err := audit.Read(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	e := events[0]
	if e.Source != audit.SourceRouter || e.Kind != audit.KindSpend || e.Target != "inc1" {
		t.Fatalf("event %+v", e)
	}
	// 1 + 0.5*2 dollars
	if usd, _ := e.Detail["usd"].(float64); usd < 1.999 || usd > 2.001 {
		t.Errorf("usd = %v", e.Detail["usd"])
	}
	if e.Detail["prompt_tokens"] != float64(1000000) || e.Detail["completion_tokens"] != float64(500000) {
		t.Errorf("tokens %+v", e.Detail)
	}
}
