// Package router is the home server's single OpenAI-compatible endpoint
// (Suite D70, D71, D98): it lists every registry provider's models and
// forwards chat, completion and embedding requests to the provider that
// serves the one asked for, and only to a model that may see the request's
// privacy tier, tracks spend and caps the month's top-up. Ranking comes later. See docs/internals/router.md
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"jgarner.dev/bud/internal/registry"
)

// maxBody caps a request body; prompts with images can be large, but not
// unbounded
const maxBody = 10 << 20

// RolePrefix starts a model id that names a role instead of a model
const RolePrefix = "bud/"

// endpoints are the paths the router forwards, each appended to the
// provider's endpoint
var endpoints = map[string]string{
	"/v1/chat/completions": "/chat/completions",
	"/v1/completions":      "/completions",
	"/v1/embeddings":       "/embeddings",
}

type target struct {
	base *url.URL
	name string
	key  string
	// provider is the registry provider id, for health
	provider string
}

// Router serves the OpenAI API over a registry's providers
type Router struct {
	reg     *registry.Registry
	targets map[string]target // by registry model id
	proxy   *httputil.ReverseProxy
	ledger  *Ledger
	cap     float64
	logf    func(string, ...any)

	health       *health
	probeEvery   time.Duration
	probeTimeout time.Duration
	provURL      map[string]*url.URL // by provider id
	provKey      map[string]string
	approvals    *Approvals
	p0Wait       time.Duration

	noteMu sync.Mutex
	noted  map[string]bool
}

// Options are what New needs besides the registry and environment
type Options struct {
	// Ledger records spend; nil keeps it in memory only
	Ledger *Ledger
	// TopUpCap is the dollars a month of top-up the router may spend; 0 means none
	TopUpCap float64
	// Logf writes to the router's log; nil uses the standard logger
	Logf func(string, ...any)
	// Approvals reviews a P0 request that only a non-local model can serve
	// (Suite D104); nil refuses such a request at once
	Approvals *Approvals
	// P0Wait is how long a held P0 request waits for Jason; zero means 10 minutes
	P0Wait time.Duration
	// ProbeInterval and ProbeTimeout time the health probes; zero means 10 s and 2 s
	ProbeInterval, ProbeTimeout time.Duration
	// Now is the clock; nil means time.Now
	Now func() time.Time
}

type ctxKey struct{}

// New checks that every keyed provider's key is in the environment, so a
// unit started without its env file fails at once instead of on a request
func New(reg *registry.Registry, getenv func(string) string, opts Options) (*Router, error) {
	keys := map[string]string{}
	for pid, p := range reg.Providers {
		v := registry.KeyVar(pid, p.Key)
		if v == "" {
			continue
		}
		if getenv(v) == "" {
			return nil, fmt.Errorf("provider %s: %s is empty; set it in the router's env file (the unit runs under op run, see server/router/README.md)", pid, v)
		}
		keys[pid] = getenv(v)
	}
	rt := &Router{reg: reg, targets: map[string]target{}, ledger: opts.Ledger, cap: opts.TopUpCap, logf: opts.Logf, noted: map[string]bool{}}
	if rt.logf == nil {
		rt.logf = log.Printf
	}
	rt.approvals, rt.p0Wait = opts.Approvals, opts.P0Wait
	if rt.p0Wait <= 0 {
		rt.p0Wait = 10 * time.Minute
	}
	rt.probeEvery, rt.probeTimeout = opts.ProbeInterval, opts.ProbeTimeout
	if rt.probeEvery <= 0 {
		rt.probeEvery = defaultProbeInterval
	}
	if rt.probeTimeout <= 0 {
		rt.probeTimeout = defaultProbeTimeout
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rt.provURL, rt.provKey = map[string]*url.URL{}, keys
	probed := map[string]bool{}
	if rt.ledger == nil {
		rt.ledger, _ = NewLedger("", reg.Models)
	}
	for id, m := range reg.Models {
		p, ok := reg.Providers[m.Provider]
		if !ok {
			return nil, fmt.Errorf("model %s: no provider %q", id, m.Provider)
		}
		base, err := url.Parse(p.Endpoint)
		if err != nil || base.Host == "" {
			return nil, fmt.Errorf("provider %s: endpoint isn't a URL", m.Provider)
		}
		rt.targets[id] = target{base: base, name: m.Name, key: keys[m.Provider], provider: m.Provider}
		rt.provURL[m.Provider] = base
		// the local providers are the ones that serve P0, and the only ones probed
		if m.Allows("P0") {
			probed[m.Provider] = true
		}
	}
	rt.health = newHealth(now, rt.logf, probed)
	rt.proxy = &httputil.ReverseProxy{
		// Set replaces any upstream X-Bud-Model, so only the router says
		// which model answered
		ModifyResponse: func(resp *http.Response) error {
			id := resp.Request.Context().Value(ctxKey{}).(*resolved).id
			resp.Header.Set("X-Bud-Model", id)
			if f := resp.Request.Context().Value(ctxKey{}).(*resolved).fallback; f != "" {
				resp.Header.Set("X-Bud-Fallback", f)
			}
			// set before the body is charged, so it's the total before this request
			resp.Header.Set("X-Bud-Spend", fmt.Sprintf("topup=%.2f/%.2f", rt.ledger.TopUp(), rt.cap))
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				stream := strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")
				resp.Body = &meter{rc: resp.Body, stream: stream, finish: func(u *usage) { rt.charge(id, u) }}
			}
			return nil
		},
		Rewrite: rt.rewrite,
		// -1 flushes after every write, so SSE chunks arrive as they're made
		FlushInterval: -1,
		// the proxy's own log line names the upstream and the dial error,
		// which shouldn't reach the journal any more than the client
		ErrorLog: log.New(io.Discard, "", 0),
		// nothing is written here: this runs only before any response bytes,
		// so ServeHTTP can retry with another member or write the 502 itself.
		// The error names the upstream's address, which isn't the client's
		// business, so it's kept only as a fact
		ErrorHandler: func(_ http.ResponseWriter, r *http.Request, err error) {
			res := r.Context().Value(ctxKey{}).(*resolved)
			// a client that went away isn't a provider that did
			res.failed = r.Context().Err() == nil
		},
	}
	return rt, nil
}

// charge prices a response's usage and adds it to the ledger
func (rt *Router) charge(id string, u *usage) {
	m := rt.reg.Models[id]
	if m.Cost != "C1" && m.Cost != "C2" {
		return
	}
	if u == nil || m.Price == nil {
		rt.noteUntracked(id)
		return
	}
	cost := u.Prompt*m.Price.In/1e6 + u.Completion*m.Price.Out/1e6
	if err := rt.ledger.Charge(id, cost); err != nil {
		rt.logf("router: %v", err)
	}
}

func (rt *Router) noteUntracked(id string) {
	rt.noteMu.Lock()
	first := !rt.noted[id]
	rt.noted[id] = true
	rt.noteMu.Unlock()
	if first {
		rt.logf("router: spend for model %s isn't tracked: no price or no usage in its responses", id)
	}
}

func (rt *Router) rewrite(pr *httputil.ProxyRequest) {
	t := pr.In.Context().Value(ctxKey{}).(*resolved)
	pr.Out.URL = t.url
	pr.Out.Host = t.url.Host
	// nothing of the client's identity goes on: its key is the router's
	// business, not the provider's
	pr.Out.Header.Del("Authorization")
	pr.Out.Header.Del("Cookie")
	// the tier was this router's business; providers never see it
	pr.Out.Header.Del(registry.PrivacyHeader)
	// the meter reads usage from the body, so it must be plain: without the
	// client's Accept-Encoding the transport asks for gzip itself and hands
	// back the body decoded
	pr.Out.Header.Del("Accept-Encoding")
	if t.key != "" {
		pr.Out.Header.Set("Authorization", "Bearer "+t.key)
	}
	pr.Out.Header.Set("Content-Length", strconv.Itoa(len(t.body)))
	pr.Out.ContentLength = int64(len(t.body))
	pr.Out.Body = io.NopCloser(bytes.NewReader(t.body))
}

type resolved struct {
	id   string
	url  *url.URL
	key  string
	body []byte
	// fallback is the X-Bud-Fallback value to add to the response
	fallback string
	// failed is set by the proxy's error handler on a transport error
	failed bool
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tier, valid := registry.RequestTier(r.Header.Get(registry.PrivacyHeader))
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("%s is %q; use P0, P1 or P2", registry.PrivacyHeader, tier))
		return
	}
	if r.URL.Path == "/v1/models" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "use GET")
			return
		}
		rt.models(w, tier)
		return
	}
	_, ok := endpoints[r.URL.Path]
	if !ok {
		writeError(w, http.StatusNotFound, "invalid_request_error", "no such endpoint; the router serves GET /v1/models, POST /v1/chat/completions, /v1/completions and /v1/embeddings")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "use POST")
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "the request body is over 10 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", "couldn't read the request body")
		return
	}
	// only model is looked at; every other field is kept as it came
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "the request body isn't a JSON object")
		return
	}
	var model string
	if err := json.Unmarshal(fields["model"], &model); err != nil || model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "the request needs a model; GET /v1/models lists them")
		return
	}
	if _, isRole := strings.CutPrefix(model, RolePrefix); !isRole {
		t, ok := rt.targets[model]
		if !ok {
			writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("no model %q; GET /v1/models lists them", model))
			return
		}
		if !rt.reg.Models[model].Allows(tier) {
			writeError(w, http.StatusForbidden, "invalid_request_error", fmt.Sprintf("model %s can't take %s requests; GET /v1/models with %s: %s lists the ones that can", model, tier, registry.PrivacyHeader, tier))
			return
		}
		if rt.health.isDown(t.provider) {
			writeError(w, http.StatusServiceUnavailable, "api_error", fmt.Sprintf("model %s's provider is down; use %s<role> to fall back", model, RolePrefix))
			return
		}
		if rt.forward(w, r, fields, model, "") {
			rt.badGateway(w)
		}
		return
	}

	role := strings.TrimPrefix(model, RolePrefix)
	ro, known := rt.reg.Roles[role]
	if !known || ro.Default() == "" {
		writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("no model %q; GET /v1/models lists them", model))
		return
	}
	if rt.pick(ro, tier) == "" {
		writeError(w, http.StatusForbidden, "invalid_request_error", fmt.Sprintf("no model in role %s takes %s requests; GET /v1/models with %s: %s lists what does", role, tier, registry.PrivacyHeader, tier))
		return
	}
	// a request is tried on at most two members: the first that is up, and
	// after a transport error the next
	tried := map[string]bool{}
	for attempt := 0; attempt < 2; attempt++ {
		id := rt.pickSpend(ro, tier, tried)
		if id == "" {
			switch {
			case attempt > 0:
				rt.badGateway(w)
			case rt.anyUp(ro, tier, nil):
				rt.capReached(w)
			case tier == "P0":
				rt.p0Down(w, r, raw, fields, role, ro)
			default:
				writeError(w, http.StatusServiceUnavailable, "api_error", fmt.Sprintf("every model in role %s that takes %s requests is down", role, tier))
			}
			return
		}
		if !rt.forward(w, r, fields, id, "") {
			return
		}
		tried[id] = true
	}
	rt.badGateway(w)
}

func (rt *Router) badGateway(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "api_error", "the model's provider didn't answer; check the router's provider")
}

// forward sends the request to model id and reports whether the provider
// failed before any response bytes, so the caller may try another. Every
// other outcome, an error answer included, has been written to w. The cap
// check, the charge and the release of the top-up slot belong to one attempt,
// so a retry never charges twice: only a 2xx body is charged
func (rt *Router) forward(w http.ResponseWriter, r *http.Request, fields map[string]json.RawMessage, id, fallback string) (failed bool) {
	t := rt.targets[id]
	if rt.ledger.spendsTopUp(id) {
		release, err := rt.ledger.admitTopUp(r.Context().Done(), rt.cap)
		if err != nil {
			if reason := err.Error(); reason != "" {
				writeError(w, http.StatusServiceUnavailable, "api_error", reason)
			} else {
				rt.capReached(w)
			}
			return false
		}
		// the proxy returns once the body is copied and charged
		defer release()
	}
	// without include_usage a stream carries no usage to charge
	if r.URL.Path == "/v1/chat/completions" {
		var stream bool
		if json.Unmarshal(fields["stream"], &stream) == nil && stream {
			if _, has := fields["stream_options"]; !has {
				fields["stream_options"] = json.RawMessage(`{"include_usage":true}`)
			}
		}
	}
	name, _ := json.Marshal(t.name)
	fields["model"] = name
	// no HTML escaping, so <, > and & in a prompt reach the model as written
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "the request body couldn't be re-encoded")
		return false
	}
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	// joined onto the path, so an endpoint's query string stays a query
	u := *t.base
	u.Path = strings.TrimSuffix(u.Path, "/") + endpoints[r.URL.Path]
	u.RawPath = ""
	res := &resolved{id: id, url: &u, key: t.key, body: body, fallback: fallback}
	rt.proxy.ServeHTTP(w, r.WithContext(contextWith(r, res)))
	if res.failed {
		rt.health.markDown(t.provider)
	}
	return res.failed
}

// eligible lists the members that take the tier, the pin first
func (rt *Router) eligible(ro registry.Role, tier string) []string {
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		if m, ok := rt.reg.Models[id]; ok && id != "" && m.Allows(tier) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	add(ro.Pin)
	for _, id := range ro.Members() {
		add(id)
	}
	return ids
}

// anyUp reports whether an eligible member outside skip has a provider that isn't down
func (rt *Router) anyUp(ro registry.Role, tier string, skip map[string]bool) bool {
	for _, id := range rt.eligible(ro, tier) {
		if !skip[id] && !rt.health.isDown(rt.targets[id].provider) {
			return true
		}
	}
	return false
}

// pick chooses a role's model for a tier: the pin if it may see the tier,
// else the first member that may. "" means none can. It ignores health, so
// listings and the 403 don't change when a provider is down.
func (rt *Router) pick(ro registry.Role, tier string) string {
	if m, ok := rt.reg.Models[ro.Pin]; ok && ro.Pin != "" && m.Allows(tier) {
		return ro.Pin
	}
	for _, id := range ro.Members() {
		if m, ok := rt.reg.Models[id]; ok && m.Allows(tier) {
			return id
		}
	}
	return ""
}

// pickSpend picks among the tier's eligible members whose provider is up and
// that aren't in skip: the first one that spends no top-up, then one that
// does while the month's top-up is under the cap. "" means none is left or the
// cap stops every one.
func (rt *Router) pickSpend(ro registry.Role, tier string, skip map[string]bool) string {
	var ids []string
	for _, id := range rt.eligible(ro, tier) {
		if !skip[id] && !rt.health.isDown(rt.targets[id].provider) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if !rt.ledger.spendsTopUp(id) {
			return id
		}
	}
	if rt.ledger.TopUp() < rt.cap && len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (rt *Router) capReached(w http.ResponseWriter) {
	writeError(w, http.StatusPaymentRequired, "insufficient_quota", fmt.Sprintf("this month's top-up cap of $%.2f is reached; spending past it needs Jason's approval, which isn't built yet; C0 models and models with allowance left still work, and GET /v1/models lists them", rt.cap))
}

func (rt *Router) models(w http.ResponseWriter, tier string) {
	type entry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	data := []entry{}
	for id, m := range rt.reg.Models {
		if !m.Allows(tier) {
			continue
		}
		data = append(data, entry{id, "model", m.Provider})
	}
	for role, ro := range rt.reg.Roles {
		if rt.pick(ro, tier) != "" {
			data = append(data, entry{RolePrefix + role, "model", "bud"})
		}
	}
	sort.Slice(data, func(i, j int) bool { return data[i].ID < data[j].ID })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg, "type": typ}})
}

func contextWith(r *http.Request, v *resolved) context.Context {
	return context.WithValue(r.Context(), ctxKey{}, v)
}
