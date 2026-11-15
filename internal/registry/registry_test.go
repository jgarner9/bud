package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// valid is a small complete registry the tests break one field at a time
func valid() *Registry {
	return &Registry{
		Version: 1,
		Providers: map[string]Provider{
			"cloud": {API: "openai", Endpoint: "https://cloud.example/v1", Key: "{env:CLOUD_KEY}"},
			"home":  {API: "openai", Endpoint: "http://home.invalid:8080/v1", Local: true},
		},
		Models: map[string]Model{
			"big":   {Provider: "cloud", Name: "big-1", Privacy: []string{"P1", "P2"}, Cost: "C1"},
			"local": {Provider: "home", Name: "local-1", Privacy: []string{"P0", "P1", "P2"}, Cost: "C0"},
			"vec":   {Provider: "home", Name: "vec-1", Privacy: []string{"P0"}, Cost: "C0", Dims: 1024},
		},
		Roles: map[string]Role{
			"manager":    {Model: "big", Fallbacks: []string{"local"}},
			"coding":     {Model: "big"},
			"planning":   {Model: "big"},
			"classifier": {Model: "local"},
			"fim":        {Model: "local", Fallbacks: []string{"big"}},
			"embedding":  {Model: "vec"},
			"fallback":   {Model: "local"},
			"curator":    {Model: "local"},
			"vision":     {Model: "big"},
		},
	}
}

func TestTheShippedRegistryIsValid(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "content", "env", "registry.jsonc")); err != nil {
		t.Fatal(err)
	}
}

func TestAValidRegistryPasses(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEachProblemSaysWhatToFix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(r *Registry)
		want   string
	}{
		{"wrong version", func(r *Registry) { r.Version = 2 }, `version: is 2, but this bud reads version 1`},
		{"unknown api", func(r *Registry) { p := r.Providers["cloud"]; p.API = "grpc"; r.Providers["cloud"] = p }, `providers.cloud.api: "grpc" isn't one of openai`},
		{"bad endpoint", func(r *Registry) { p := r.Providers["cloud"]; p.Endpoint = "cloud.example"; r.Providers["cloud"] = p }, `providers.cloud.endpoint: "cloud.example" isn't an http or https URL`},
		{"a key value", func(r *Registry) { p := r.Providers["cloud"]; p.Key = "sk-live-123"; r.Providers["cloud"] = p }, `providers.cloud.key: must be an op://vault/item/field reference or {env:VAR}, never a key itself`},
		{"bad id", func(r *Registry) { r.Providers["Cloud Two"] = r.Providers["cloud"] }, `providers.Cloud Two: an ID is lowercase`},
		{"missing provider", func(r *Registry) { m := r.Models["big"]; m.Provider = "nope"; r.Models["big"] = m }, `models.big.provider: "nope" isn't in providers`},
		{"no name", func(r *Registry) { m := r.Models["big"]; m.Name = ""; r.Models["big"] = m }, `models.big.name: empty`},
		{"bad tier", func(r *Registry) { m := r.Models["big"]; m.Privacy = []string{"P9"}; r.Models["big"] = m }, `models.big.privacy: "P9" isn't one of P0, P1, P2`},
		{"no tiers", func(r *Registry) { m := r.Models["big"]; m.Privacy = nil; r.Models["big"] = m }, `models.big.privacy: empty`},
		{"P0 in the cloud", func(r *Registry) { m := r.Models["big"]; m.Privacy = []string{"P0"}; r.Models["big"] = m }, `models.big.privacy: allows P0, but provider "cloud" isn't local`},
		{"bad cost", func(r *Registry) { m := r.Models["big"]; m.Cost = "free"; r.Models["big"] = m }, `models.big.cost: "free" isn't one of C0, C1, C2`},
		{"missing role", func(r *Registry) { delete(r.Roles, "vision") }, `roles.vision: missing`},
		{"unknown role", func(r *Registry) { r.Roles["chef"] = Role{Model: "big"} }, `roles.chef: not a role`},
		{"unknown model", func(r *Registry) { r.Roles["coding"] = Role{Model: "huge"} }, `roles.coding.model: "huge" isn't in models`},
		{"unknown fallback", func(r *Registry) { r.Roles["coding"] = Role{Model: "big", Fallbacks: []string{"huge"}} }, `roles.coding.fallbacks[0]: "huge" isn't in models`},
		{"repeated fallback", func(r *Registry) { r.Roles["coding"] = Role{Model: "big", Fallbacks: []string{"big"}} }, `roles.coding.fallbacks[0]: "big" is listed twice`},
		{"cloud classifier", func(r *Registry) { r.Roles["classifier"] = Role{Model: "big"} }, `roles.classifier.model: "big" isn't allowed P0, but classifier sees private data`},
		{"cloud curator fallback", func(r *Registry) { r.Roles["curator"] = Role{Model: "local", Fallbacks: []string{"big"}} }, `roles.curator.fallbacks[0]: "big" isn't allowed P0`},
		{"embedding without dims", func(r *Registry) { r.Roles["embedding"] = Role{Model: "local"} }, `roles.embedding.model: "local" has no dims`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.break_(r)
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing\n  %s\ngot\n  %v", tc.want, err)
			}
		})
	}
}

func TestEveryProblemIsReportedAtOnceInOrder(t *testing.T) {
	r := valid()
	delete(r.Roles, "vision")
	delete(r.Roles, "coding")
	err := r.Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "2 problems:") || strings.Index(msg, "roles.coding") > strings.Index(msg, "roles.vision") {
		t.Fatalf("want both, sorted:\n%s", msg)
	}
}

func TestParseTakesCommentsAndTrailingCommas(t *testing.T) {
	src := `// top
{
  /* block
     comment */
  "version": 1, // trailing
  "providers": {"a": {"api": "openai", "endpoint": "https://x.example/v1?a=//b",},},
  "models": {},
  "roles": {},
}`
	r, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Providers["a"].Endpoint; got != "https://x.example/v1?a=//b" {
		t.Fatalf("a // inside a string is not a comment: got %q", got)
	}
}

func TestParseRejectsUnknownFieldsWithTheLine(t *testing.T) {
	src := "{\n  \"version\": 1,\n  \"provders\": {}\n}"
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), `line 3`) || !strings.Contains(err.Error(), `provders`) {
		t.Fatalf("want the typo and its line, got %v", err)
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	if _, err := Parse([]byte(`{"version": 1} {}`)); err == nil {
		t.Fatal("want an error for a second value")
	}
}

func TestStripJSONCKeepsLinesAndStrings(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"a": "x\"//y"}`, `{"a": "x\"//y"}`},
		{"[1, 2, ]", "[1, 2  ]"},
		{"[1, /* c */ ]", "[1          ]"},
		{"{\"a\":1 // c\n}", "{\"a\":1     \n}"},
		{"/*\n*/1", "  \n  1"},
	} {
		got, err := StripJSONC([]byte(tc.in))
		if err != nil || string(got) != tc.want {
			t.Errorf("StripJSONC(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{`{"a": "open`, "/* never closed", "{\"a\": \"line\nbreak\"}"} {
		if _, err := StripJSONC([]byte(bad)); err == nil {
			t.Errorf("StripJSONC(%q): want an error", bad)
		}
	}
}

func TestTheOverlayReplacesEntriesByID(t *testing.T) {
	base := valid()
	overlay := &Registry{
		Providers: map[string]Provider{"home": {API: "openai", Endpoint: "http://box.lan:8080/v1", Local: true}},
		Roles:     map[string]Role{"coding": {Model: "local"}},
	}
	got, err := Merge(base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers["home"].Endpoint != "http://box.lan:8080/v1" || got.Roles["coding"].Model != "local" {
		t.Fatalf("overlay didn't win: %+v %+v", got.Providers["home"], got.Roles["coding"])
	}
	if got.Providers["cloud"].Endpoint != "https://cloud.example/v1" || got.Roles["manager"].Model != "big" {
		t.Fatal("entries the overlay doesn't name should be kept")
	}
	if base.Providers["home"].Endpoint != "http://home.invalid:8080/v1" {
		t.Fatal("Merge changed the base registry")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAnOverlayThatDropsLocalFailsSafe(t *testing.T) {
	overlay := &Registry{Providers: map[string]Provider{"home": {API: "openai", Endpoint: "http://box.lan:8080/v1"}}}
	got, err := Merge(valid(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err == nil || !strings.Contains(err.Error(), `isn't local`) {
		t.Fatalf("a provider that loses local must stop carrying P0, got %v", err)
	}
}

func TestMergeRefusesAnotherVersion(t *testing.T) {
	if _, err := Merge(valid(), &Registry{Version: 2}); err == nil {
		t.Fatal("want an error")
	}
}

func TestLoadNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonc")
	os.WriteFile(path, []byte(`{"version": 1}`), 0o644)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("want the path in the error, got %v", err)
	}
}
