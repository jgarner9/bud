package registry

import (
	"bytes"
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
		{"bad input", func(r *Registry) { m := r.Models["big"]; m.Inputs = []string{"smell"}; r.Models["big"] = m }, `models.big.inputs: "smell" isn't one of text, image, video, audio, pdf`},
		{"bad cap", func(r *Registry) { m := r.Models["big"]; m.Caps = []string{"telepathy"}; r.Models["big"] = m }, `models.big.caps: "telepathy" isn't one of tools, reasoning, json, fim, embed`},
		{"bad grade", func(r *Registry) { m := r.Models["big"]; m.Grade = "best"; r.Models["big"] = m }, `models.big.grade: "best" isn't one of top, mid, light`},
		{"negative price", func(r *Registry) { m := r.Models["big"]; m.Price = &Price{In: -1}; r.Models["big"] = m }, `models.big: output, allowance, price and retention can't be negative`},
		{"trains on P1", func(r *Registry) { m := r.Models["big"]; m.Trains = true; r.Models["big"] = m }, `models.big.privacy: the provider trains on its requests, so it may see only P2`},
		{"30-day retention on P1", func(r *Registry) { m := r.Models["big"]; d := 30; m.Retention = &d; r.Models["big"] = m }, `models.big.privacy: the provider keeps requests 30 days, so it may see only P2`},
		{"pool and model", func(r *Registry) { r.Roles["coding"] = Role{Model: "big", Pool: []string{"big"}} }, `roles.coding: has both a pool and model/fallbacks`},
		{"empty role", func(r *Registry) { r.Roles["coding"] = Role{} }, `roles.coding: lists no models`},
		{"unknown pool member", func(r *Registry) { r.Roles["coding"] = Role{Pool: []string{"big", "huge"}} }, `roles.coding.pool[1]: "huge" isn't in models`},
		{"repeated pool member", func(r *Registry) { r.Roles["coding"] = Role{Pool: []string{"big", "big"}} }, `roles.coding.pool[1]: "big" is listed twice`},
		{"cloud in a private pool", func(r *Registry) { r.Roles["classifier"] = Role{Pool: []string{"local", "big"}} }, `roles.classifier.pool[1]: "big" isn't allowed P0`},
		{"bad need", func(r *Registry) { r.Roles["coding"] = Role{Pool: []string{"big"}, Needs: []string{"magic"}} }, `roles.coding.needs: "magic" isn't a capability`},
		{"unmet need", func(r *Registry) { r.Roles["vision"] = Role{Pool: []string{"big"}, Needs: []string{"image"}} }, `roles.vision.pool[0]: "big" lacks "image", which vision needs`},
		{"pin outside the pool", func(r *Registry) { r.Roles["coding"] = Role{Pool: []string{"big"}, Pin: "local"} }, `roles.coding.pin: "local" isn't in the role's pool`},
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

func TestAPoolWithMetNeedsAndAPinPasses(t *testing.T) {
	r := valid()
	m := r.Models["big"]
	zero := 0
	m.Inputs, m.Caps, m.Grade, m.Retention = []string{"text", "image"}, []string{"tools"}, "top", &zero
	m.Price = &Price{In: 1.4, Out: 4.4}
	r.Models["big"] = m
	r.Roles["vision"] = Role{Pool: []string{"big"}, Needs: []string{"image", "tools"}}
	r.Roles["coding"] = Role{Pool: []string{"big", "local"}, Pin: "local"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMembersAndDefault(t *testing.T) {
	for _, tc := range []struct {
		role    Role
		members string
		def     string
	}{
		{Role{Model: "a", Fallbacks: []string{"b"}}, "a,b", "a"},
		{Role{Pool: []string{"c", "d"}}, "c,d", "c"},
		{Role{Pool: []string{"c", "d"}, Pin: "d"}, "c,d", "d"},
		{Role{}, "", ""},
	} {
		if got := strings.Join(tc.role.Members(), ","); got != tc.members {
			t.Errorf("%+v: members %q, want %q", tc.role, got, tc.members)
		}
		if got := tc.role.Default(); got != tc.def {
			t.Errorf("%+v: default %q, want %q", tc.role, got, tc.def)
		}
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
		if got := StripJSONC([]byte(tc.in)); string(got) != tc.want {
			t.Errorf("StripJSONC(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseRejectsAnUnterminatedStringOrComment(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an unclosed string", "{\n  \"version\": 1,\n  \"providers\": {\"a\": \"open\n}", ""},
		{"an unclosed block comment", "{\n  /* never closed\n}", "line 2"},
		{"a string that runs past its line", "{\n  \"models\": {\"a\": \"line\nbreak\"}\n}", ""},
	} {
		_, err := Parse([]byte(tc.src))
		if err == nil {
			t.Errorf("%s: want an error", tc.name)
			continue
		}
		if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want the error to say %q, got %v", tc.name, tc.want, err)
		}
	}
}

// FuzzStripJSONC guards the contract the package leans on: the plain JSON
// is exactly as long as the source and keeps its line breaks, so byte
// offsets and error lines still point into the original file
func FuzzStripJSONC(f *testing.F) {
	for _, seed := range []string{
		`{"a": "x\"//y"}`, "[1, 2, ]", "/*\n*/1", "{\n  // c\n}", `["//\""]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		out := StripJSONC(src)
		if len(out) != len(src) {
			t.Fatalf("the plain JSON is %d bytes, the source is %d", len(out), len(src))
		}
		if got, want := bytes.Count(out, []byte("\n")), bytes.Count(src, []byte("\n")); got != want {
			t.Fatalf("the plain JSON has %d newlines, the source has %d", got, want)
		}
	})
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

func TestProviderKeyVariableCollisions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		keys      map[string]string
		wantError bool
	}{
		{"normalized names collide", map[string]string{"one-two": "op://vault/one/key", "one_two": "op://vault/two/key"}, true},
		{"generated name collides with env", map[string]string{"one-two": "op://vault/one/key", "other": "{env:BUD_ONE_TWO_API_KEY}"}, true},
		{"same reference can be shared", map[string]string{"one-two": "op://vault/one/key", "one_two": "op://vault/one/key"}, false},
		{"distinct generated names", map[string]string{"one": "op://vault/one/key", "two": "op://vault/two/key"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			for id, key := range tc.keys {
				r.Providers[id] = Provider{API: "openai", Endpoint: "https://example.invalid/v1", Key: key}
			}
			err := r.Validate()
			if tc.wantError && (err == nil || !strings.Contains(err.Error(), "conflicting key sources")) {
				t.Fatalf("Validate() = %v, want key collision", err)
			}
			if !tc.wantError && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpRefsAllowSpacesInsideNamesButNotAtTheEdges(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		want bool
	}{
		{"op://vault/item/field", true},
		{"op://example vault/API Key/credential", true},
		{"op://vault/Mail Login/section/password", true},
		{"op://vault/item", false},
		{"op://vault /item/field", false},
		{"op://vault/ item/field", false},
		{"op://vault/item/field ", false},
		{"op://vault/two  spaces/field", false},
		{"op://vault/item/field\nEXTRA=1", false},
		{"sk-live-123", false},
	} {
		if got := IsOpRef(tc.ref); got != tc.want {
			t.Errorf("IsOpRef(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}
