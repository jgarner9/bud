package mcpops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jgarner.dev/bud/internal/approval"
)

// writeFixture is a server with a read tool and a fake approval-tier write tool.
// reviewer decides each prompt; writes counts how often the write's effect ran
type writeFixture struct {
	s      *Server
	writes atomic.Int32
	seen   chan approval.Prompt
}

func newWriteFixture(t *testing.T, storePath string, decide func(ctx context.Context, p approval.Prompt) (approval.Choice, error)) *writeFixture {
	t.Helper()
	f := &writeFixture{seen: make(chan approval.Prompt, 8)}
	f.s = New([]Tool{
		{Name: "read", Run: func(context.Context, json.RawMessage) (any, error) { return "read ok", nil }},
		{Name: "write", Tier: ApprovalTier, Run: func(context.Context, json.RawMessage) (any, error) {
			f.writes.Add(1)
			return "wrote", nil
		}},
	})
	f.s.SetApprovals(Approvals{
		Store: approval.Store{Path: storePath},
		Review: func(scope approval.Scope) approval.Review {
			return func(ctx context.Context, p approval.Prompt) (approval.Reply, error) {
				f.seen <- p
				c, err := decide(ctx, p)
				if err != nil {
					return approval.Reply{}, err
				}
				return approval.Reply{ID: p.ID, Scope: scope, Choice: c}, nil
			}
		},
	})
	return f
}

func storePath(t *testing.T) string { return filepath.Join(t.TempDir(), "approvals.json") }

func allow(context.Context, approval.Prompt) (approval.Choice, error) { return approval.AllowOnce, nil }

func TestReadOnlyToolRunsWithoutAReview(t *testing.T) {
	f := newWriteFixture(t, storePath(t), func(context.Context, approval.Prompt) (approval.Choice, error) {
		t.Error("a read-only tool was reviewed")
		return approval.DenyOnce, nil
	})
	_, out := post(t, f.s, call("read", `{}`))
	if text, isErr := toolText(t, out); isErr || text != `"read ok"` {
		t.Errorf("read = %q, isError %v", text, isErr)
	}
}

func TestApprovalToolRunsOnceAfterAnAllowBoundToItsExactCall(t *testing.T) {
	f := newWriteFixture(t, storePath(t), allow)
	args := `{"b": 1,  "a":2}`
	_, out := post(t, f.s, call("write", args))
	if text, isErr := toolText(t, out); isErr || text != `"wrote"` {
		t.Errorf("write = %q, isError %v", text, isErr)
	}
	if n := f.writes.Load(); n != 1 {
		t.Errorf("effect ran %d times, want 1", n)
	}
	p := <-f.seen
	if p.Invocation.Action != "mcp:ops/write" || string(p.Invocation.Arguments) != args {
		t.Errorf("binding = %q %q", p.Invocation.Action, p.Invocation.Arguments)
	}
}

func TestApprovalToolNeverRunsWhenNotApproved(t *testing.T) {
	tests := []struct {
		name   string
		decide func(ctx context.Context, p approval.Prompt) (approval.Choice, error)
		want   string
	}{
		{"deny", func(context.Context, approval.Prompt) (approval.Choice, error) { return approval.DenyOnce, nil }, "denied"},
		{"expiry", func(ctx context.Context, _ approval.Prompt) (approval.Choice, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}, "in time"},
		{"reviewer unavailable", func(context.Context, approval.Prompt) (approval.Choice, error) {
			return "", approval.ErrReviewUnavailable
		}, "unavailable"},
		{"reviewer error", func(context.Context, approval.Prompt) (approval.Choice, error) {
			return "", errors.New("boom")
		}, "store failed"},
		{"unknown choice", func(context.Context, approval.Prompt) (approval.Choice, error) { return "always", nil }, "didn't match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWriteFixture(t, storePath(t), tt.decide)
			f.s.appr.Timeout = 50 * time.Millisecond
			_, out := post(t, f.s, call("write", `{}`))
			text, isErr := toolText(t, out)
			if !isErr || !strings.Contains(text, tt.want) {
				t.Errorf("result = %q, isError %v, want an error mentioning %q", text, isErr, tt.want)
			}
			if n := f.writes.Load(); n != 0 {
				t.Errorf("effect ran %d times", n)
			}
		})
	}
}

func TestApprovalToolNeverRunsWhenTheStoreFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// the store's parent is a regular file, so Create can't write
	f := newWriteFixture(t, filepath.Join(file, "approvals.json"), allow)
	_, out := post(t, f.s, call("write", `{}`))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "store failed") {
		t.Errorf("result = %q, isError %v", text, isErr)
	}
	if n := f.writes.Load(); n != 0 {
		t.Errorf("effect ran %d times", n)
	}
}

func TestApprovalToolWithoutApprovalsIsRefused(t *testing.T) {
	var ran atomic.Bool
	s := New([]Tool{{Name: "write", Tier: ApprovalTier, Run: func(context.Context, json.RawMessage) (any, error) {
		ran.Store(true)
		return nil, nil
	}}})
	_, out := post(t, s, call("write", `{}`))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "unavailable") || ran.Load() {
		t.Errorf("result = %q, isError %v, ran %v", text, isErr, ran.Load())
	}
}

func TestACallCanProceedWhileAnotherWaitsForReview(t *testing.T) {
	release := make(chan struct{})
	f := newWriteFixture(t, storePath(t), func(ctx context.Context, _ approval.Prompt) (approval.Choice, error) {
		select {
		case <-release:
			return approval.AllowOnce, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	done := make(chan map[string]any, 1)
	go func() {
		_, out := post(t, f.s, call("write", `{}`))
		done <- out
	}()
	<-f.seen // the write is now waiting on Jason

	read := make(chan map[string]any, 1)
	go func() {
		_, out := post(t, f.s, call("read", `{}`))
		read <- out
	}()
	select {
	case out := <-read:
		if text, isErr := toolText(t, out); isErr || text != `"read ok"` {
			t.Errorf("read = %q, isError %v", text, isErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a read call was blocked behind a waiting review")
	}
	if n := f.writes.Load(); n != 0 {
		t.Errorf("effect ran %d times before the answer", n)
	}
	close(release)
	if text, isErr := toolText(t, <-done); isErr || text != `"wrote"` {
		t.Errorf("write = %q, isError %v", text, isErr)
	}
}

func TestToolsListMarksApprovalToolsAsNotReadOnly(t *testing.T) {
	f := newWriteFixture(t, storePath(t), allow)
	_, out := post(t, f.s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, v := range out["result"].(map[string]any)["tools"].([]any) {
		tool := v.(map[string]any)
		ro := tool["annotations"].(map[string]any)["readOnlyHint"].(bool)
		if want := tool["name"] == "read"; ro != want {
			t.Errorf("%s readOnlyHint = %v", tool["name"], ro)
		}
	}
}

func TestAnUnknownTierIsRefused(t *testing.T) {
	var ran atomic.Bool
	s := New([]Tool{{Name: "odd", Tier: Tier(99), Run: func(context.Context, json.RawMessage) (any, error) {
		ran.Store(true)
		return nil, nil
	}}})
	_, out := post(t, s, call("odd", `{}`))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "unknown tier") || ran.Load() {
		t.Errorf("result = %q, isError %v, ran %v", text, isErr, ran.Load())
	}
}

func TestTheApprovalTargetIsOps(t *testing.T) {
	f := newWriteFixture(t, storePath(t), allow)
	post(t, f.s, call("write", `{}`))
	if p := <-f.seen; p.Invocation.Target != "ops" || p.Invocation.Action != "mcp:ops/write" {
		t.Errorf("invocation = %+v, want target ops and action mcp:ops/write", p.Invocation)
	}
}

func TestAnApprovedEffectDoesNotRunAfterTheRequestEndsWhileWaitingForTheLock(t *testing.T) {
	f := newWriteFixture(t, storePath(t), allow)
	ctx, cancel := context.WithCancel(context.Background())
	f.s.mu.Lock() // a read-only call holds the lock
	done := make(chan error, 1)
	go func() {
		_, err := f.s.run(ctx, f.s.tools[1], json.RawMessage(`{}`))
		done <- err
	}()
	<-f.seen
	time.Sleep(50 * time.Millisecond) // the approved effect is now waiting on the lock
	cancel()
	f.s.mu.Unlock()
	if err := <-done; err == nil {
		t.Error("run returned no error after the request ended")
	}
	if n := f.writes.Load(); n != 0 {
		t.Errorf("effect ran %d times after the request ended", n)
	}
}
