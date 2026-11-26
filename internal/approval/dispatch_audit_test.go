package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/audit"
)

func auditedStore(t *testing.T) (Store, string) {
	t.Helper()
	dir := t.TempDir()
	log, err := audit.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return Store{Path: filepath.Join(dir, "approvals.json"), Audit: log, Source: audit.SourceGateway}, log.Path()
}

func decisions(t *testing.T, path string) []string {
	t.Helper()
	events, err := audit.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range events {
		if e.Kind != audit.KindApproval || e.Source != audit.SourceGateway {
			t.Fatalf("unexpected event %+v", e)
		}
		out = append(out, e.Decision)
	}
	return out
}

func TestDispatchRecordsEachOutcome(t *testing.T) {
	scope := Scope{Runtime: "mcp-gateway", Profile: "P1", Session: "s1", Surface: "mail", Actor: "hermes"}
	call := Invocation{Action: "mcp:mail/send", Target: "mail", Arguments: []byte("secret body")}
	allow := func(_ context.Context, p Prompt) (Reply, error) {
		return Reply{ID: p.ID, Scope: scope, Choice: AllowOnce}, nil
	}
	for _, tt := range []struct {
		name    string
		wait    time.Duration // 0 waits as long as the store allows
		review  Review
		execute func(context.Context, Invocation) error
		want    string
	}{
		{"approved", 0, allow, func(context.Context, Invocation) error { return nil }, "approved"},
		{"approved then the tool failed", 0, allow, func(context.Context, Invocation) error { return errors.New("boom") }, "approved_failed"},
		{"denied", 0, func(_ context.Context, p Prompt) (Reply, error) {
			return Reply{ID: p.ID, Scope: scope, Choice: DenyOnce}, nil
		}, nil, "denied"},
		{"declined", 0, func(_ context.Context, p Prompt) (Reply, error) {
			return Reply{ID: p.ID, Scope: scope, Choice: DenyOnce, Answered: true}, nil
		}, nil, "declined"},
		{"expired", 50 * time.Millisecond, func(ctx context.Context, _ Prompt) (Reply, error) {
			<-ctx.Done()
			return Reply{}, ctx.Err()
		}, nil, "expired"},
		{"review failed", 0, func(context.Context, Prompt) (Reply, error) { return Reply{}, errors.New("no signal") }, nil, "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, path := auditedStore(t)
			exec := tt.execute
			if exec == nil {
				exec = unexpectedExecutor(t)
			}
			var deadline time.Time
			if tt.wait > 0 {
				deadline = time.Now().Add(tt.wait)
			}
			_ = store.Dispatch(context.Background(), scope, call, deadline, tt.review, exec)
			got := decisions(t, path)
			if len(got) != 2 || got[0] != "requested" || got[1] != tt.want {
				t.Fatalf("recorded %v, want [requested %s]", got, tt.want)
			}
		})
	}
}

func TestDispatchAuditNeverHoldsArguments(t *testing.T) {
	store, path := auditedStore(t)
	scope := Scope{Runtime: "mcp-gateway", Profile: "P1", Session: "s1", Surface: "mail", Actor: "hermes"}
	call := Invocation{Action: "mcp:mail/send", Target: "mail", Arguments: []byte("secret body")}
	_ = store.Dispatch(context.Background(), scope, call, time.Time{}, func(_ context.Context, p Prompt) (Reply, error) {
		return Reply{ID: p.ID, Scope: scope, Choice: AllowOnce}, nil
	}, func(context.Context, Invocation) error { return nil })
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if strings.Contains(raw, "secret body") {
		t.Fatalf("the audit log holds the arguments: %s", raw)
	}
	if !strings.Contains(raw, "arguments_hash") {
		t.Fatalf("the audit log should hold the arguments' hash: %s", raw)
	}
}

func TestDispatchWithoutAnAuditLogStillWorks(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	scope := Scope{Runtime: "r", Profile: "p", Session: "s", Surface: "x", Actor: "a"}
	err := store.Dispatch(context.Background(), scope, Invocation{Action: "a", Target: "t"}, time.Time{}, func(_ context.Context, p Prompt) (Reply, error) {
		return Reply{ID: p.ID, Scope: scope, Choice: AllowOnce}, nil
	}, func(context.Context, Invocation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}
