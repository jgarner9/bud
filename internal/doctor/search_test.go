package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("BUD_SEARXNG_URL", srv.URL+"/")
}

func TestSearchCheckIsSkippedWithoutAnEndpoint(t *testing.T) {
	t.Setenv("BUD_SEARXNG_URL", "  ")
	if _, ok := checkSearch(context.Background()); ok {
		t.Fatal("got a result with BUD_SEARXNG_URL unset")
	}
}

func TestSearchCheckPassesWhenTheInstanceAnswersJSON(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	})
	r, ok := checkSearch(context.Background())
	if !ok || r.Status != OK {
		t.Fatalf("got %+v, %v; want OK", r, ok)
	}
}

func TestSearchCheckWarnsWhenTheInstanceAnswersHTML(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html></html>`))
	})
	r, ok := checkSearch(context.Background())
	if !ok || r.Status != Warn || r.Fix == "" {
		t.Fatalf("got %+v, %v; want Warn with a fix", r, ok)
	}
}

func TestSearchCheckWarnsWhenTheInstanceRefuses(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	r, ok := checkSearch(context.Background())
	if !ok || r.Status != Warn {
		t.Fatalf("got %+v, %v; want Warn", r, ok)
	}
}
