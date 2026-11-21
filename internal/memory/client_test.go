package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeService serves the memory API on a Unix socket, answering each action
// with a canned reply and recording what it was sent
type fakeService struct {
	got     map[string]map[string]any
	replies map[string]string
	status  int
}

func serve(t *testing.T, f *fakeService) string {
	t.Helper()
	// short, because a Unix socket path is limited to about 100 bytes
	dir, err := os.MkdirTemp("", "mem")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "unauthorized"}`))
			return
		}
		action := r.URL.Path[1:]
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.got[action] = body
		if f.status != 0 {
			w.WriteHeader(f.status)
			w.Write([]byte(`{"error": "forbidden"}`))
			return
		}
		w.Write([]byte(f.replies[action]))
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func TestClientActions(t *testing.T) {
	f := &fakeService{got: map[string]map[string]any{}, replies: map[string]string{
		"retrieve":       `{"records": [{"id": 3, "source": "hermes", "kind": "fact", "privacy": "P0", "body": "b", "created_at": "2026-11-21 10:00:00", "matches": ["text", "vector"], "score": 0.03}]}`,
		"search":         `{"records": [{"id": 4, "source": "s", "kind": "fact", "privacy": "P1", "body": "t", "created_at": "c"}]}`,
		"stage":          `{"id": 7}`,
		"vector_pending": `{"records": [{"id": 1, "privacy": "P2", "body": "x", "body_hash": "h"}]}`,
		"vector_put":     `{"stored": true}`,
	}}
	c := New(serve(t, f), "tok")
	ctx := context.Background()

	recs, err := c.Retrieve(ctx, Query{Query: "q", Model: "m", Values: []float64{0.5}, Since: "2026-11-01", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{{ID: 3, Source: "hermes", Kind: "fact", Privacy: "P0", Body: "b", CreatedAt: "2026-11-21 10:00:00", Matches: []string{"text", "vector"}}}
	if !reflect.DeepEqual(recs, want) {
		t.Errorf("Retrieve = %+v, want %+v", recs, want)
	}
	// optional filters that are unset aren't sent, since the service refuses unknown or empty ones
	if got := f.got["retrieve"]; got["since"] != "2026-11-01" || got["limit"] != 5.0 || got["source"] != nil || got["until"] != nil {
		t.Errorf("retrieve sent %v", got)
	}

	recs, err = c.Search(ctx, "q", 0)
	if err != nil || len(recs) != 1 || !reflect.DeepEqual(recs[0].Matches, []string{"text"}) {
		t.Errorf("Search = %+v, %v; want one record matched by text", recs, err)
	}
	if got := f.got["search"]; !reflect.DeepEqual(got, map[string]any{"query": "q"}) {
		t.Errorf("search sent %v; a zero limit should be left out", got)
	}

	id, err := c.Stage(ctx, Draft{Source: "s", DedupeKey: "k", Kind: "fact", Privacy: "P1", Body: "b"})
	if err != nil || id != 7 {
		t.Errorf("Stage = %d, %v; want 7", id, err)
	}
	if got := f.got["stage"]; len(got) != 5 || got["dedupe_key"] != "k" {
		t.Errorf("stage sent %v", got)
	}

	pend, err := c.Pending(ctx, "m", 16)
	if err != nil || !reflect.DeepEqual(pend, []Unembedded{{ID: 1, Privacy: "P2", Body: "x", BodyHash: "h"}}) {
		t.Errorf("Pending = %+v, %v", pend, err)
	}
	if err := c.PutVector(ctx, 1, "m", []float64{0.1, 0.2}, "h"); err != nil {
		t.Fatal(err)
	}
	if got := f.got["vector_put"]; got["id"] != 1.0 || got["model"] != "m" || got["body_hash"] != "h" || len(got["values"].([]any)) != 2 {
		t.Errorf("vector_put sent %v", got)
	}
}

func TestClientErrors(t *testing.T) {
	f := &fakeService{got: map[string]map[string]any{}, status: http.StatusForbidden}
	sock := serve(t, f)
	_, err := New(sock, "tok").Stage(context.Background(), Draft{})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 403 || se.Reason != "forbidden" || se.Action != "stage" {
		t.Errorf("a refusal = %v, want a 403 StatusError for stage", err)
	}

	_, err = New(sock, "wrong").Search(context.Background(), "q", 0)
	if !errors.As(err, &se) || se.Status != 401 {
		t.Errorf("a bad token = %v, want a 401", err)
	}

	_, err = New(filepath.Join(t.TempDir(), "none"), "tok").Search(context.Background(), "q", 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("a missing socket = %v, want ErrUnavailable", err)
	}

	f.status = http.StatusConflict
	if err := New(sock, "tok").PutVector(context.Background(), 1, "m", []float64{1}, "old"); !errors.Is(err, ErrStale) {
		t.Errorf("a 409 = %v, want ErrStale", err)
	}

	f.status = 0
	f.replies = map[string]string{"search": "not json"}
	if _, err := New(sock, "tok").Search(context.Background(), "q", 0); err == nil {
		t.Error("an unreadable reply should be an error")
	}
}
