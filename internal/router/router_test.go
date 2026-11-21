package router

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/registry"
)

func testRegistry(endpoint, key string) *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{"go": {API: "openai", Endpoint: endpoint, Key: key}},
		Models: map[string]registry.Model{
			"glm-5.3": {Provider: "go", Name: "glm-upstream", Privacy: []string{"P0", "P1", "P2"}},
			"other":   {Provider: "go", Name: "other-upstream", Privacy: []string{"P0", "P1", "P2"}},
		},
		Roles: map[string]registry.Role{
			"coding": {Pool: []string{"glm-5.3", "other"}},
			"vision": {Pool: []string{"other"}, Pin: "glm-5.3"},
		},
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func do(t *testing.T, h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestModelsListsEveryModelAndRoleAlias(t *testing.T) {
	rt, err := New(testRegistry("http://x.invalid/v1", ""), env(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, rt, "GET", "/v1/models", "", nil)
	var out struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range out.Data {
		got[d.ID] = d.OwnedBy
		if d.Object != "model" {
			t.Errorf("%s object = %q", d.ID, d.Object)
		}
	}
	want := map[string]string{"glm-5.3": "go", "other": "go", "bud/coding": "bud", "bud/vision": "bud"}
	if out.Object != "list" || len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s owned_by %q, want %q", k, got[k], v)
		}
	}
}

func TestRequestsGoToTheProviderWithTheModelRenamedAndTheRestUntouched(t *testing.T) {
	var gotPath, gotAuth, gotCookie string
	var gotBody []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotCookie = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Cookie")
		gotBody, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer up.Close()
	rt, err := New(testRegistry(up.URL+"/v1/", "{env:GO_KEY}"), env(map[string]string{"GO_KEY": "sekret"}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	const messages = `[{"role":"user","content":"a<b && c>d \u00e9 \n"}]`
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"bud/coding","messages":`+messages+`}`,
		map[string]string{"Authorization": "Bearer client-key", "Cookie": "a=b"})
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path %q", gotPath)
	}
	if gotAuth != "Bearer sekret" {
		t.Errorf("Authorization %q", gotAuth)
	}
	if gotCookie != "" {
		t.Errorf("cookie forwarded: %q", gotCookie)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["model"]) != `"glm-upstream"` {
		t.Errorf("model %s", fields["model"])
	}
	if string(fields["messages"]) != messages {
		t.Errorf("messages changed: %s", fields["messages"])
	}
}

func TestRoleAliasResolvesToTheRolesDefault(t *testing.T) {
	var gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer up.Close()
	rt, _ := New(testRegistry(up.URL, ""), env(nil), Options{})
	// vision pins glm-5.3 although its pool lists only "other"
	do(t, rt, "POST", "/v1/embeddings", `{"model":"bud/vision"}`, nil)
	if gotBody != `{"model":"glm-upstream"}` {
		t.Errorf("body %s", gotBody)
	}
}

func TestStreamChunksArriveBeforeTheUpstreamFinishes(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "data: two\n\n")
	}))
	defer up.Close()
	defer close(release)
	rt, _ := New(testRegistry(up.URL, ""), env(nil), Options{})
	front := httptest.NewServer(rt)
	defer front.Close()

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"glm-5.3","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- l
	}()
	select {
	case l := <-line:
		if l != "data: one\n" {
			t.Errorf("first line %q", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first chunk didn't arrive while the upstream was still open")
	}
}

func TestUnknownModelIs404NamingItAndPointingAtTheList(t *testing.T) {
	rt, _ := New(testRegistry("http://x.invalid", ""), env(nil), Options{})
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"nope"}`, nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), `\"nope\"`) || !strings.Contains(w.Body.String(), "GET /v1/models") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	if w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"bud/nothing"}`, nil); w.Code != 404 {
		t.Errorf("unknown role: %d", w.Code)
	}
}

func TestBadJSONIs400InOpenAIShape(t *testing.T) {
	rt, _ := New(testRegistry("http://x.invalid", ""), env(nil), Options{})
	w := do(t, rt, "POST", "/v1/chat/completions", `{nope`, nil)
	var e struct {
		Error struct{ Message, Type string }
	}
	if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message == "" || e.Error.Type == "" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	if w := do(t, rt, "POST", "/v1/chat/completions", `{"messages":[]}`, nil); w.Code != 400 {
		t.Errorf("no model: %d", w.Code)
	}
}

func TestBodyOverTenMiBIs413(t *testing.T) {
	rt, _ := New(testRegistry("http://x.invalid", ""), env(nil), Options{})
	big := `{"model":"glm-5.3","pad":"` + strings.Repeat("a", maxBody) + `"}`
	if w := do(t, rt, "POST", "/v1/chat/completions", big, nil); w.Code != 413 {
		t.Errorf("status %d", w.Code)
	}
}

func TestNewFailsWhenAProvidersKeyVariableIsEmpty(t *testing.T) {
	_, err := New(testRegistry("http://x.invalid", "op://vault/item/field"), env(nil), Options{})
	if err == nil || !strings.Contains(err.Error(), "BUD_GO_API_KEY") || !strings.Contains(err.Error(), "op run") {
		t.Errorf("err = %v", err)
	}
}

func TestUpstreamDownIs502WithoutTheDialError(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	addr := up.URL
	up.Close()
	rt, _ := New(testRegistry(addr, ""), env(nil), Options{})
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"glm-5.3"}`, nil)
	if w.Code != 502 {
		t.Fatalf("status %d", w.Code)
	}
	if b := w.Body.String(); strings.Contains(b, "dial") || strings.Contains(b, "refused") || strings.Contains(b, strings.TrimPrefix(addr, "http://")) {
		t.Errorf("body leaks the transport error: %s", b)
	}
}

func TestEndpointQueryStaysAQuery(t *testing.T) {
	var path, query string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
	}))
	defer up.Close()
	rt, err := New(testRegistry(up.URL+"/v1/?tenant=home", ""), env(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	do(t, rt, "POST", "/v1/chat/completions", `{"model":"glm-5.3"}`, nil)
	if path != "/v1/chat/completions" || query != "tenant=home" {
		t.Errorf("path %q query %q, want /v1/chat/completions and tenant=home", path, query)
	}
}

// tierRegistry has one model per tier ceiling, so each rule has a model to hit
func tierRegistry(endpoint string) *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{"go": {API: "openai", Endpoint: endpoint}},
		Models: map[string]registry.Model{
			"local":  {Provider: "go", Name: "local-up", Privacy: []string{"P0", "P1", "P2"}},
			"p1":     {Provider: "go", Name: "p1-up", Privacy: []string{"P1", "P2"}},
			"cloud":  {Provider: "go", Name: "cloud-up", Privacy: []string{"P2"}},
			"cloud2": {Provider: "go", Name: "cloud2-up", Privacy: []string{"P2"}},
		},
		Roles: map[string]registry.Role{
			"pinned": {Pool: []string{"p1", "local"}, Pin: "cloud"},
			"cloudy": {Pool: []string{"cloud", "cloud2"}},
		},
	}
}

func tierServer(t *testing.T) (*Router, *http.Header, *string) {
	t.Helper()
	var hdr http.Header
	var body string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("X-Bud-Model", "liar")
		io.WriteString(w, "{}")
	}))
	t.Cleanup(up.Close)
	rt, err := New(tierRegistry(up.URL), env(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rt, &hdr, &body
}

func chat(model string) string { return `{"model":"` + model + `","messages":[]}` }

func TestNoPrivacyHeaderMeansP0(t *testing.T) {
	rt, _, _ := tierServer(t)
	if w := do(t, rt, "POST", "/v1/chat/completions", chat("p1"), nil); w.Code != 403 {
		t.Fatalf("p1 without header = %d, want 403", w.Code)
	}
	if w := do(t, rt, "POST", "/v1/chat/completions", chat("local"), nil); w.Code != 200 {
		t.Fatalf("local without header = %d, want 200", w.Code)
	}
}

func TestP1HeaderLetsAP1ModelAnswer(t *testing.T) {
	rt, _, body := tierServer(t)
	w := do(t, rt, "POST", "/v1/chat/completions", chat("p1"), map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 200 || !strings.Contains(*body, "p1-up") {
		t.Fatalf("code %d body %q", w.Code, *body)
	}
}

func TestExplicitModelOfTheWrongTierIs403PointingAtTheList(t *testing.T) {
	rt, _, _ := tierServer(t)
	w := do(t, rt, "POST", "/v1/chat/completions", chat("cloud"), map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 403 || !strings.Contains(w.Body.String(), "can't take P1 requests") || !strings.Contains(w.Body.String(), "GET /v1/models") {
		t.Fatalf("code %d body %s", w.Code, w.Body)
	}
}

func TestRoleFallsThroughAnIneligiblePinToTheFirstEligiblePoolMember(t *testing.T) {
	rt, _, body := tierServer(t)
	w := do(t, rt, "POST", "/v1/chat/completions", chat("bud/pinned"), map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 200 || !strings.Contains(*body, "p1-up") || w.Header().Get("X-Bud-Model") != "p1" {
		t.Fatalf("code %d model %q body %q", w.Code, w.Header().Get("X-Bud-Model"), *body)
	}
	w = do(t, rt, "POST", "/v1/chat/completions", chat("bud/pinned"), map[string]string{"X-Bud-Privacy": "P2"})
	if w.Header().Get("X-Bud-Model") != "cloud" {
		t.Fatalf("an eligible pin should win, got %q", w.Header().Get("X-Bud-Model"))
	}
}

func TestRoleWithNoEligibleMemberIs403(t *testing.T) {
	rt, _, _ := tierServer(t)
	w := do(t, rt, "POST", "/v1/chat/completions", chat("bud/cloudy"), map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 403 || !strings.Contains(w.Body.String(), "no model in role cloudy takes P1 requests") {
		t.Fatalf("code %d body %s", w.Code, w.Body)
	}
}

func TestInvalidPrivacyHeaderIs400NamingTheValidValues(t *testing.T) {
	rt, _, _ := tierServer(t)
	w := do(t, rt, "POST", "/v1/chat/completions", chat("local"), map[string]string{"X-Bud-Privacy": "P9"})
	if w.Code != 400 || !strings.Contains(w.Body.String(), "P0, P1 or P2") {
		t.Fatalf("code %d body %s", w.Code, w.Body)
	}
}

func TestModelsListIsFilteredByTier(t *testing.T) {
	rt, _, _ := tierServer(t)
	ids := func(tier string) string {
		hdr := map[string]string{}
		if tier != "" {
			hdr["X-Bud-Privacy"] = tier
		}
		var out struct{ Data []struct{ ID string } }
		json.Unmarshal(do(t, rt, "GET", "/v1/models", "", hdr).Body.Bytes(), &out)
		var s []string
		for _, d := range out.Data {
			s = append(s, d.ID)
		}
		return strings.Join(s, ",")
	}
	if got := ids(""); got != "bud/pinned,local" {
		t.Errorf("P0 = %s", got)
	}
	if got := ids("P1"); got != "bud/pinned,local,p1" {
		t.Errorf("P1 = %s", got)
	}
	if got := ids("P2"); got != "bud/cloudy,bud/pinned,cloud,cloud2,local,p1" {
		t.Errorf("P2 = %s", got)
	}
}

func TestPrivacyHeaderIsNotForwardedAndTheAnsweringModelIsNamed(t *testing.T) {
	rt, hdr, _ := tierServer(t)
	w := do(t, rt, "POST", "/v1/chat/completions", chat("p1"), map[string]string{"X-Bud-Privacy": "P1"})
	if v := (*hdr).Get("X-Bud-Privacy"); v != "" {
		t.Errorf("upstream saw X-Bud-Privacy %q", v)
	}
	if got := w.Result().Header.Values("X-Bud-Model"); len(got) != 1 || got[0] != "p1" {
		t.Errorf("X-Bud-Model = %v, want [p1]", got)
	}
}
