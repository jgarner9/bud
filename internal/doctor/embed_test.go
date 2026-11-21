package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/registry"
)

func TestCheckEmbeddings(t *testing.T) {
	var asked map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "post only", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/v1/embeddings":
			w.Write([]byte(`{"data": [{"embedding": [0.1, 0.2, 0.3]}]}`))
		case "/empty/embeddings":
			w.Write([]byte(`{"data": []}`))
		case "/junk/embeddings":
			w.Write([]byte(`not json`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	reg := func(endpoint string, dims int) *registry.Registry {
		return &registry.Registry{
			Providers: map[string]registry.Provider{"embed": {Endpoint: endpoint, Local: true}},
			Models:    map[string]registry.Model{"e": {Provider: "embed", Name: "embed-small", Dims: dims}},
			Roles:     map[string]registry.Role{"embedding": {Pool: []string{"e"}}},
		}
	}
	tests := []struct {
		name     string
		endpoint string
		dims     int
		status   Status
		detail   string
	}{
		{"matches", srv.URL + "/v1", 3, OK, "e (3 dims) answered"},
		{"other weights", srv.URL + "/v1", 1024, Warn, "e returned 3 dims but the registry says 1024"},
		{"no vector", srv.URL + "/empty", 3, Warn, "e: " + host + " sent no embedding"},
		{"not json", srv.URL + "/junk", 3, Warn, "e: " + host + " sent an embedding reply bud can't read"},
		{"refused", srv.URL + "/nope", 3, Warn, "e: " + host + " answered 404 Not Found"},
		{"down", downURL + "/v1", 3, Warn, "e: " + strings.TrimPrefix(downURL, "http://") + " didn't answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkEmbeddings(context.Background(), reg(tt.endpoint, tt.dims))
			if len(got) != 1 {
				t.Fatalf("got %d results, want 1", len(got))
			}
			if r := got[0]; r.Status != tt.status || !strings.Contains(r.Detail, tt.detail) {
				t.Fatalf("got %v %q, want %v containing %q", r.Status, r.Detail, tt.status, tt.detail)
			}
		})
	}
	if asked["model"] != "embed-small" || asked["input"] == "" {
		t.Errorf("the request should name the provider's model and carry one input; got %v", asked)
	}

	if got := checkEmbeddings(context.Background(), &registry.Registry{}); got != nil {
		t.Errorf("a registry with no embedding role should give no result; got %v", got)
	}
}
