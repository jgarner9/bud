package memorymcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/memory"
)

// fakeVectors holds records that need vectors; one may change under the
// embedder every time it's read
type fakeVectors struct {
	pending  []memory.Unembedded
	stored   map[int64]bool
	changing int64
	err      error
}

func (f *fakeVectors) Pending(_ context.Context, _ string, limit int) ([]memory.Unembedded, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []memory.Unembedded
	for _, p := range f.pending {
		if !f.stored[p.ID] && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeVectors) PutVector(_ context.Context, id int64, _ string, _ []float64, _ string) error {
	if id == f.changing {
		return fmt.Errorf("memory vector_put: record %d: %w", id, memory.ErrStale)
	}
	f.stored[id] = true
	return nil
}

func TestBackfillDrainsInBatchesAndSkipsAChangingRecord(t *testing.T) {
	v := &fakeVectors{stored: map[int64]bool{}, changing: 2}
	for i := int64(1); i <= 5; i++ {
		v.pending = append(v.pending, memory.Unembedded{ID: i, Body: fmt.Sprint("body ", i)})
	}
	emb := &fakeEmbedder{}
	b := &Backfill{Vectors: v, Embed: emb, Model: "m", Batch: 2}
	n, err := b.Round(context.Background())
	if err != nil || n != 4 {
		t.Fatalf("Round = %d, %v; want 4 stored and record 2 skipped", n, err)
	}
	for _, texts := range emb.texts {
		if len(texts) > 2 {
			t.Errorf("a batch of %d, want at most 2", len(texts))
		}
	}
	if v.stored[2] {
		t.Error("the changing record should stay pending")
	}

	v.err = memory.ErrUnavailable
	if _, err := b.Round(context.Background()); !errors.Is(err, memory.ErrUnavailable) {
		t.Errorf("Round with memory down = %v", err)
	}
	v.err = nil
	emb.err = errors.New("down")
	v.stored = map[int64]bool{}
	if n, err := b.Round(context.Background()); err == nil || n != 0 {
		t.Errorf("Round with the embedder down = %d, %v; want an error and nothing stored", n, err)
	}
}

func TestHTTPEmbedder(t *testing.T) {
	var asked map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&asked)
		switch r.URL.Path {
		case "/v1/embeddings":
			// out of order on purpose: the index decides
			w.Write([]byte(`{"data":[{"index":1,"embedding":[0,1,0]},{"index":0,"embedding":[1,0,0]}]}`))
		case "/short/embeddings":
			w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]},{"index":1,"embedding":[0,1]}]}`))
		case "/few/embeddings":
			w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0]}]}`))
		default:
			http.Error(w, "too long", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	e, err := NewHTTPEmbedder(srv.URL+"/v1", "qwen3-embedding-0.6b", 3)
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil || vecs[0][0] != 1 || vecs[1][1] != 1 {
		t.Fatalf("Embed = %v, %v", vecs, err)
	}
	if asked["model"] != "qwen3-embedding-0.6b" || len(asked["input"].([]any)) != 2 {
		t.Errorf("asked %v", asked)
	}
	for path, want := range map[string]string{
		"/short": "sent 2 dims where the registry says 3",
		"/few":   "sent 1 vectors for 2 texts",
		"/bad":   "answered 400 Bad Request: too long",
	} {
		e, _ := NewHTTPEmbedder(srv.URL+path, "m", 3)
		if _, err := e.Embed(context.Background(), []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", path, err, want)
		}
	}
	if _, err := NewHTTPEmbedder("ftp://x", "m", 3); err == nil {
		t.Error("a non-http endpoint should be refused")
	}
}
