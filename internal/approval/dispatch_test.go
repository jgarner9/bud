package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchExecutesFrozenInvocation(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	scope := Scope{Runtime: "opencode", Profile: "local", Session: "s1", Surface: "acp", Actor: "reviewer"}
	call := Invocation{Action: "edit", Target: "example.txt", Arguments: []byte("final")}
	var id string
	toolFailure := errors.New("tool failure")
	err := store.Dispatch(context.Background(), scope, call, time.Time{}, func(_ context.Context, p Prompt) (Reply, error) {
		id = p.ID
		if p.Invocation.Action != call.Action || p.Invocation.Target != call.Target || string(p.Invocation.Arguments) != "final" {
			t.Fatalf("unexpected prompt: %+v", p)
		}
		call.Arguments[0] = 'x'
		p.Invocation.Arguments[0] = 'y'
		return Reply{ID: id, Scope: scope, Choice: AllowOnce}, nil
	}, func(_ context.Context, invocation Invocation) error {
		if string(invocation.Arguments) != "final" {
			t.Fatalf("executor got %q", invocation.Arguments)
		}
		request, err := store.Get(id)
		if err != nil || request.State != Executed {
			t.Fatalf("dispatch did not consume approval first: %+v, %v", request, err)
		}
		return toolFailure
	})
	if !errors.Is(err, toolFailure) {
		t.Fatalf("dispatch = %v, want tool failure", err)
	}
	err = store.Dispatch(context.Background(), scope, call, time.Time{}, func(context.Context, Prompt) (Reply, error) {
		return Reply{ID: id, Scope: scope, Choice: AllowOnce}, nil
	}, unexpectedExecutor(t))
	if !errors.Is(err, ErrReviewMismatch) {
		t.Fatalf("replayed reply = %v, want ErrReviewMismatch", err)
	}
}

func TestDispatchRejectsInvalidReview(t *testing.T) {
	scope := Scope{Runtime: "hermes", Profile: "private", Session: "s1", Surface: "signal", Actor: "reviewer"}
	call := Invocation{Action: "edit", Target: "example.txt"}
	for _, tt := range []struct {
		name   string
		change func(*Reply)
		want   error
		state  State
	}{
		{"request", func(r *Reply) { r.ID = "other" }, ErrReviewMismatch, Canceled},
		{"runtime", func(r *Reply) { r.Scope.Runtime = "opencode" }, ErrReviewMismatch, Canceled},
		{"profile", func(r *Reply) { r.Scope.Profile = "cloud" }, ErrReviewMismatch, Canceled},
		{"session", func(r *Reply) { r.Scope.Session = "other" }, ErrReviewMismatch, Canceled},
		{"surface", func(r *Reply) { r.Scope.Surface = "cli" }, ErrReviewMismatch, Canceled},
		{"actor", func(r *Reply) { r.Scope.Actor = "other" }, ErrReviewMismatch, Canceled},
		{"always", func(r *Reply) { r.Choice = "always" }, ErrReviewMismatch, Canceled},
		{"denial", func(r *Reply) { r.Choice = DenyOnce }, ErrDenied, Denied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
			var id string
			err := store.Dispatch(context.Background(), scope, call, time.Time{}, func(_ context.Context, p Prompt) (Reply, error) {
				id = p.ID
				reply := Reply{ID: id, Scope: scope, Choice: AllowOnce}
				tt.change(&reply)
				return reply, nil
			}, unexpectedExecutor(t))
			if !errors.Is(err, tt.want) {
				t.Fatalf("dispatch = %v, want %v", err, tt.want)
			}
			request, err := store.Get(id)
			if err != nil || request.State != tt.state {
				t.Fatalf("request = %+v, %v, want %v", request, err, tt.state)
			}
		})
	}
}

func TestDispatchFailsClosed(t *testing.T) {
	scope := Scope{Runtime: "hermes", Profile: "private", Session: "s1", Surface: "signal", Actor: "reviewer"}
	call := Invocation{Action: "edit", Target: "example.txt"}
	store := Store{Path: filepath.Join(t.TempDir(), "approvals.json")}
	if err := store.Dispatch(context.Background(), scope, call, time.Time{}, nil, unexpectedExecutor(t)); !errors.Is(err, ErrReviewUnavailable) {
		t.Fatalf("unattended dispatch = %v", err)
	}
	unexpectedReview := func(context.Context, Prompt) (Reply, error) {
		t.Fatal("review started without a valid pending request")
		return Reply{}, nil
	}
	if err := store.Dispatch(context.Background(), scope, call, time.Now().Add(-time.Minute), unexpectedReview, unexpectedExecutor(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired dispatch = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := store.Dispatch(ctx, scope, call, time.Time{}, func(_ context.Context, p Prompt) (Reply, error) {
		cancel()
		return Reply{ID: p.ID, Scope: scope, Choice: AllowOnce}, nil
	}, unexpectedExecutor(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dispatch = %v", err)
	}
	err = store.Dispatch(context.Background(), scope, call, time.Time{}, func(_ context.Context, p Prompt) (Reply, error) {
		if err := os.WriteFile(store.Path, []byte("invalid"), 0600); err != nil {
			t.Fatal(err)
		}
		return Reply{ID: p.ID, Scope: scope, Choice: AllowOnce}, nil
	}, unexpectedExecutor(t))
	if err == nil {
		t.Fatal("dispatch accepted an approval it could not persist")
	}
	if err := store.Dispatch(context.Background(), scope, call, time.Time{}, unexpectedReview, unexpectedExecutor(t)); err == nil {
		t.Fatal("dispatch started with a corrupt store")
	}
}

func unexpectedExecutor(t *testing.T) func(context.Context, Invocation) error {
	t.Helper()
	return func(context.Context, Invocation) error {
		t.Fatal("tool executed without a valid approval")
		return nil
	}
}
