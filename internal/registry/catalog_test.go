package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// goList is a trimmed copy of OpenCode Go's /models answer
const goList = `{"object": "list", "data": [
  {"id": "glm-5.3", "modalities": {"input": ["text"]},
   "capabilities": {"tools": {"supported": true}, "reasoning": {"supported": true}, "structured_output": true},
   "limit": {"context": 1000000, "output": 131072},
   "cost": {"text": {"input": {"price": 1.5}, "output": {"price": 4.4}}}},
  {"id": "mimo-v2.6-flash", "modalities": {"input": ["text", "image/png", "image/jpeg", "audio/wav", "video/mp4"]},
   "capabilities": {"tools": {"supported": true}, "reasoning": {"supported": true}},
   "limit": {"context": 1048576, "output": 131072},
   "cost": {"text": {"input": {"price": 0.14}, "output": {"price": 0.28}}}},
  {"id": "grok-4.7", "modalities": {"input": ["text", "application/pdf"]}}
]}`

func TestParseCatalogReadsGoFacts(t *testing.T) {
	list, err := ParseCatalog(strings.NewReader(goList))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 models, got %d", len(list))
	}
	mimo := list[1].Facts
	if got := strings.Join(mimo.Inputs, ","); got != "text,image,video,audio" {
		t.Errorf("inputs folded and ordered: got %s", got)
	}
	if got := strings.Join(mimo.Caps, ","); got != "tools,reasoning" {
		t.Errorf("caps: got %s", got)
	}
	if got := strings.Join(list[2].Facts.Inputs, ","); got != "text,pdf" {
		t.Errorf("pdf input: got %s", got)
	}
}

func TestParseCatalogTakesAnIDOnlyList(t *testing.T) {
	list, err := ParseCatalog(strings.NewReader(`{"data": [{"id": "qwen-35b", "object": "model"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "qwen-35b" || list[0].Facts != nil {
		t.Fatalf("want an ID with no facts, got %+v", list[0])
	}
	if _, err := ParseCatalog(strings.NewReader(`{"models": []}`)); err == nil {
		t.Fatal("want an error for a list with no data array")
	}
}

func catalogRegistry() *Registry {
	r := valid()
	r.Providers["go"] = Provider{API: "openai", Endpoint: "https://go.example/v1"}
	r.Models["glm-5.3"] = Model{Provider: "go", Name: "glm-5.3", Privacy: []string{"P1", "P2"}, Cost: "C1",
		Inputs: []string{"text"}, Caps: []string{"tools", "reasoning", "json"}, Context: 1000000, Output: 131072,
		Price: &Price{In: 1.4, Out: 4.4}, Grade: "top"}
	r.Models["mimo"] = Model{Provider: "go", Name: "mimo-v2.6-flash", Privacy: []string{"P1", "P2"}, Cost: "C1", Caps: []string{"fim"}}
	r.Models["kimi-k3"] = Model{Provider: "go", Name: "kimi-k3", Privacy: []string{"P1", "P2"}, Cost: "C1"}

	return r
}

func TestCompareFindsDriftGoneAndNew(t *testing.T) {
	list, err := ParseCatalog(strings.NewReader(goList))
	if err != nil {
		t.Fatal(err)
	}
	rep := Compare(catalogRegistry(), "go", list)
	if strings.Join(rep.Checked, ",") != "glm-5.3,kimi-k3,mimo" {
		t.Errorf("checked: %v", rep.Checked)
	}
	if strings.Join(rep.Gone, ",") != "kimi-k3" {
		t.Errorf("gone: %v", rep.Gone)
	}
	if strings.Join(rep.New, ",") != "grok-4.7" {
		t.Errorf("new: %v", rep.New)
	}
	var got []string
	for _, d := range rep.Drift {
		got = append(got, d.Model+"."+d.Field+": "+d.Was+" -> "+d.Now)
	}
	want := []string{
		`glm-5.3.price: { "in": 1.4, "out": 4.4 } -> { "in": 1.5, "out": 4.4 }`,
		`mimo.inputs:  -> ["text", "image", "video", "audio"]`,
		`mimo.caps: ["fim"] -> ["tools", "reasoning", "fim"]`,
		`mimo.context:  -> 1048576`,
		`mimo.output:  -> 131072`,
		`mimo.price:  -> { "in": 0.14, "out": 0.28 }`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("drift:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFetchSendsAnEnvKeyAndReadsTheList(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(goList))
	}))
	defer srv.Close()
	t.Setenv("TEST_GO_KEY", "secret")

	list, err := Fetch(context.Background(), srv.Client(), Provider{Endpoint: srv.URL + "/v1", Key: "{env:TEST_GO_KEY}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || auth != "Bearer secret" {
		t.Fatalf("got %d models, auth %q", len(list), auth)
	}
	if _, err := Fetch(context.Background(), srv.Client(), Provider{Endpoint: srv.URL + "/nope"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want a 404 error, got %v", err)
	}
}

func TestPatchFactsKeepsEverythingElse(t *testing.T) {
	src := `{
  "version": 1,
  // the cloud plan
  "providers": {"go": {"api": "openai", "endpoint": "https://go.example/v1"}},
  "models": {
    "glm-5.3": { "provider": "go", "name": "glm-5.3", "privacy": ["P1", "P2"], "cost": "C1",
                 "price": { "in": 1.4, "out": 4.4 }, "grade": "top" }, // keep me
    "mimo": { "provider": "go", "name": "mimo-v2.6-flash", "privacy": ["P1", "P2"], "cost": "C1", "caps": ["fim"], },
  },
  "roles": {},
}`
	glm := Model{Inputs: []string{"text"}, Price: &Price{In: 1.5, Out: 4.4}}
	mimo := Model{Caps: []string{"tools", "fim"}, Context: 1048576}
	out, err := PatchFacts([]byte(src), map[string]Model{"glm-5.3": glm, "mimo": mimo})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  // the cloud plan
  "providers": {"go": {"api": "openai", "endpoint": "https://go.example/v1"}},
  "models": {
    "glm-5.3": { "provider": "go", "name": "glm-5.3", "privacy": ["P1", "P2"], "cost": "C1",
                 "price": { "in": 1.5, "out": 4.4 }, "grade": "top", "inputs": ["text"] }, // keep me
    "mimo": { "provider": "go", "name": "mimo-v2.6-flash", "privacy": ["P1", "P2"], "cost": "C1", "caps": ["tools", "fim"], "context": 1048576, },
  },
  "roles": {},
}`
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("the patched file must still parse: %v", err)
	}
	if _, err := PatchFacts([]byte(src), map[string]Model{"nope": glm}); err == nil {
		t.Fatal("want an error for a model that isn't in the file")
	}
}

func TestAFactTheListLeavesOutIsKept(t *testing.T) {
	r := catalogRegistry()
	list, err := ParseCatalog(strings.NewReader(`{"data": [{"id": "glm-5.3", "limit": {"context": 2000000}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range Compare(r, "go", list).Drift {
		got = append(got, d.Model+"."+d.Field)
	}
	if strings.Join(got, ",") != "glm-5.3.context" {
		t.Fatalf("only the reported context may drift; got %v", got)
	}
}

func TestCapsTheListNowDeniesAreWrittenEmpty(t *testing.T) {
	src := `{"models": {"m": {"provider": "go", "caps": ["tools", "json"], "inputs": ["text"]}}}`
	list, err := ParseCatalog(strings.NewReader(`{"data": [{"id": "x", "capabilities": {}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := ApplyFacts(Model{Caps: []string{"tools", "json"}, Inputs: []string{"text"}}, *list[0].Facts)
	out, err := PatchFacts([]byte(src), map[string]Model{"m": m})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"models": {"m": {"provider": "go", "caps": [], "inputs": ["text"]}}}`; string(out) != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
