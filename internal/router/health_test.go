package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"jgarner.dev/bud/internal/registry"
)

// upstream is a provider that counts what reaches it and answers 200
type upstream struct {
	*httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	body   string
	header http.Header
	models atomic.Int32 // the status /models answers with, 0 meaning 200
}

func newUpstream(t *testing.T, usage string) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			if s := int(u.models.Load()); s != 0 {
				w.WriteHeader(s)
			}
			return
		}
		u.hits.Add(1)
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.body, u.header = string(b), r.Header.Clone()
		u.mu.Unlock()
		io.WriteString(w, `{"choices":[],"usage":`+usage+`}`)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) last() (string, http.Header) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.body, u.header
}

// fbRegistry has a local provider that serves P0, a cloud one that doesn't,
// and a role over qwen, glm and the P2-only p2
func fbRegistry(local, cloud string) *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{
			"local": {API: "openai", Endpoint: local},
			"cloud": {API: "openai", Endpoint: cloud},
		},
		Models: map[string]registry.Model{
			"qwen": {Provider: "local", Name: "qwen-up", Privacy: allTiers, Cost: "C0"},
			"glm":  {Provider: "cloud", Name: "glm-up", Privacy: []string{"P1", "P2"}, Cost: "C0"},
			"p2":   {Provider: "cloud", Name: "p2-up", Privacy: []string{"P2"}, Cost: "C0"},
		},
		Roles: map[string]registry.Role{"coding": {Pool: []string{"qwen", "glm", "p2"}}},
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func quiet(string, ...any) {}

func TestProbeMarksTheLocalProviderDownThenUp(t *testing.T) {
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	var logs []string
	rt, err := New(fbRegistry(local.URL, cloud.URL), env(nil), Options{Logf: func(f string, a ...any) { logs = append(logs, f) }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rt.probeAll(ctx)
	if rt.health.isDown("local") {
		t.Fatal("local is down after a 200")
	}
	local.models.Store(503)
	rt.probeAll(ctx)
	if !rt.health.isDown("local") {
		t.Error("local is up after a 503")
	}
	local.models.Store(0)
	rt.probeAll(ctx)
	if rt.health.isDown("local") {
		t.Error("local is down after a good probe")
	}
	if len(logs) != 2 {
		t.Errorf("logged %v, want one line per change", logs)
	}
	// the cloud provider serves no P0 model, so it isn't probed
	if rt.health.probed["cloud"] {
		t.Error("cloud is probed")
	}
}

func TestProbeSeesATransportErrorAsDownAndStartProbesBeforeReturning(t *testing.T) {
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	reg := fbRegistry(local.URL, cloud.URL)
	local.Close()
	rt, _ := New(reg, env(nil), Options{Logf: quiet, ProbeInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.Start(ctx)
	if !rt.health.isDown("local") {
		t.Error("local is up though nothing listens")
	}
}

func TestProbeSendsTheProvidersKey(t *testing.T) {
	var auth atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { auth.Store(r.Header.Get("Authorization")) }))
	defer up.Close()
	reg := fbRegistry(up.URL, up.URL)
	p := reg.Providers["local"]
	p.Key = "{env:L_KEY}"
	reg.Providers["local"] = p
	rt, err := New(reg, env(map[string]string{"L_KEY": "k"}), Options{Logf: quiet})
	if err != nil {
		t.Fatal(err)
	}
	rt.probeAll(context.Background())
	if auth.Load() != "Bearer k" {
		t.Errorf("Authorization %v", auth.Load())
	}
}

func TestATransportErrorMarksAProviderDownAndANonProbedOneRecoversAfterTheInterval(t *testing.T) {
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	reg := fbRegistry(local.URL, cloud.URL)
	cloud.Close()
	c := &fakeClock{t: time.Unix(1000, 0)}
	rt, _ := New(reg, env(nil), Options{Logf: quiet, Now: c.now})
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"glm"}`, map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 502 {
		t.Fatalf("status %d", w.Code)
	}
	if !rt.health.isDown("cloud") {
		t.Fatal("cloud isn't down after a transport error")
	}
	c.add(recoverAfter - time.Second)
	if !rt.health.isDown("cloud") {
		t.Error("cloud is up before the interval")
	}
	c.add(2 * time.Second)
	if rt.health.isDown("cloud") {
		t.Error("cloud is still down after the interval")
	}
}

func TestAProbedProviderComesBackOnlyOnAProbe(t *testing.T) {
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	reg := fbRegistry(local.URL, cloud.URL)
	c := &fakeClock{t: time.Unix(1000, 0)}
	rt, _ := New(reg, env(nil), Options{Logf: quiet, Now: c.now})
	rt.health.markDown("local")
	c.add(time.Hour)
	if !rt.health.isDown("local") {
		t.Error("a probed provider came back by the fakeClock")
	}
	rt.probeAll(context.Background())
	if rt.health.isDown("local") {
		t.Error("still down after a good probe")
	}
}

func TestARoleSkipsAMemberWhoseProviderIsDown(t *testing.T) {
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	rt, _ := New(fbRegistry(local.URL, cloud.URL), env(nil), Options{Logf: quiet})
	rt.health.markDown("local")
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"bud/coding"}`, map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 200 || w.Header().Get("X-Bud-Model") != "glm" || local.hits.Load() != 0 || cloud.hits.Load() != 1 {
		t.Errorf("status %d model %q local hits %d cloud hits %d", w.Code, w.Header().Get("X-Bud-Model"), local.hits.Load(), cloud.hits.Load())
	}
}

func TestATransportErrorRetriesOnceOnTheNextMemberWithoutChargingTwice(t *testing.T) {
	dead, live := newUpstream(t, "null"), newUpstream(t, `{"prompt_tokens":1000000,"completion_tokens":0}`)
	reg := fbRegistry(dead.URL, live.URL)
	reg.Models["glm"] = registry.Model{Provider: "cloud", Name: "glm-up", Privacy: []string{"P1", "P2"}, Cost: "C2", Price: &registry.Price{In: 3, Out: 3}}
	dead.Close()
	rt, _ := New(reg, env(nil), Options{Logf: quiet, TopUpCap: 100})
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"bud/coding","messages":[]}`, map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 200 || w.Header().Get("X-Bud-Model") != "glm" {
		t.Fatalf("status %d model %q: %s", w.Code, w.Header().Get("X-Bud-Model"), w.Body)
	}
	body, _ := live.last()
	if !strings.Contains(body, `"glm-up"`) {
		t.Errorf("the retry kept the old model name: %s", body)
	}
	if got := rt.ledger.TopUp(); got != 3 {
		t.Errorf("top-up %v, want 3 charged once", got)
	}
	if !rt.health.isDown("local") {
		t.Error("the failed provider isn't marked down")
	}
}

func TestARoleWithNoNextMemberAfterATransportErrorAnswers502(t *testing.T) {
	dead := newUpstream(t, "null")
	reg := fbRegistry(dead.URL, dead.URL)
	reg.Roles["coding"] = registry.Role{Pool: []string{"qwen"}}
	dead.Close()
	rt, _ := New(reg, env(nil), Options{Logf: quiet})
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"bud/coding"}`, map[string]string{"X-Bud-Privacy": "P1"})
	if w.Code != 502 || strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
}

func TestAnExplicitModelWhoseProviderIsDownAnswers503AndModelsStillListsIt(t *testing.T) {
	local, cloud := newUpstream(t, "null"), newUpstream(t, "null")
	rt, _ := New(fbRegistry(local.URL, cloud.URL), env(nil), Options{Logf: quiet})
	rt.health.markDown("local")
	w := do(t, rt, "POST", "/v1/chat/completions", `{"model":"qwen"}`, nil)
	want := "model qwen's provider is down; use bud/<role> to fall back"
	if w.Code != 503 || errMessage(t, w) != want || !strings.Contains(w.Body.String(), `"api_error"`) {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
	if local.hits.Load() != 0 {
		t.Error("the down provider was called")
	}
	list := do(t, rt, "GET", "/v1/models", "", nil)
	if !strings.Contains(list.Body.String(), `"qwen"`) || !strings.Contains(list.Body.String(), "bud/coding") {
		t.Errorf("models: %s", list.Body)
	}
}

func errMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error struct{ Message string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Error.Message
}
