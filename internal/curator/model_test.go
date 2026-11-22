package curator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/registry"
)

func TestLocalModelsSkipsCloud(t *testing.T) {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"go":   {API: "openai", Endpoint: "https://cloud.example/v1"},
			"qwen": {API: "openai", Endpoint: "http://qwen.invalid:8080/v1/", Local: true},
		},
		Models: map[string]registry.Model{
			"big":  {Provider: "go", Name: "big"},
			"qwen": {Provider: "qwen", Name: "qwen-35b"},
		},
		Roles: map[string]registry.Role{"curator": {Pool: []string{"big", "qwen"}}},
	}
	got, err := LocalModels(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Endpoint{URL: "http://qwen.invalid:8080/v1", Model: "qwen-35b"}) {
		t.Errorf("got %+v", got)
	}
	reg.Roles["curator"] = registry.Role{Pool: []string{"big"}}
	if _, err := LocalModels(reg); err == nil {
		t.Error("a cloud-only role was accepted")
	}
}

func TestParseFacts(t *testing.T) {
	got, err := parseFacts("```json\n" + `{"facts": [
		{"key": "systems/home#router/port", "value": " 8795 ", "statement": "The router\nlistens on 8795.", "page": "Home", "section": "Router"},
		{"key": "Bad Key", "value": "x"},
		{"key": "systems/home#router/token", "value": "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"key": "systems/home#router/empty", "value": ""}
	]}` + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != "8795" || got[0].Statement != "The router listens on 8795." {
		t.Errorf("got %+v", got)
	}
	if _, err := parseFacts("sure! here you go"); err == nil {
		t.Error("prose parsed")
	}
}

func TestChatExtractorFallsThrough(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		asked = append(asked, req.Model)
		if req.Model == "down" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/v1/chat/completions" || !strings.Contains(req.Messages[1].Content, "Record 7, from hermes on 2026-11-21") {
			t.Errorf("request %s %+v", r.URL.Path, req)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{
			"content": `{"facts": [{"key": "systems/home#router/port", "value": "8795", "statement": "s"}]}`}}}})
	}))
	defer srv.Close()
	rec := memory.ExportedRecord{ID: 7, Source: "hermes", Body: "b", CreatedAt: "2026-11-21 10:00:00"}
	ex := ChatExtractor([]Endpoint{{URL: srv.URL + "/v1", Model: "down"}, {URL: srv.URL + "/v1", Model: "up"}}, nil)
	facts, err := ex(context.Background(), rec, nil)
	if err != nil || len(facts) != 1 || strings.Join(asked, ",") != "down,up" {
		t.Fatalf("facts %+v, err %v, asked %v", facts, err, asked)
	}

	ex = ChatExtractor([]Endpoint{{URL: srv.URL + "/v1", Model: "down"}, {URL: "http://127.0.0.1:1/v1", Model: "gone"}}, nil)
	if _, err := ex(context.Background(), rec, nil); !errors.Is(err, ErrNoModel) {
		t.Errorf("err = %v, want ErrNoModel", err)
	}
}
