// Package router is the home server's single OpenAI-compatible endpoint
// (Suite D70, D71, D98): it lists every registry provider's models and
// forwards chat, completion and embedding requests to the provider that
// serves the one asked for. This slice only routes and proxies; privacy
// tiers, spend caps and ranking come later. See docs/internals/router.md
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
}

// Router serves the OpenAI API over a registry's providers
type Router struct {
	reg     *registry.Registry
	targets map[string]target // by registry model id
	proxy   *httputil.ReverseProxy
}

type ctxKey struct{}

// New checks that every keyed provider's key is in the environment, so a
// unit started without its env file fails at once instead of on a request
func New(reg *registry.Registry, getenv func(string) string) (*Router, error) {
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
	rt := &Router{reg: reg, targets: map[string]target{}}
	for id, m := range reg.Models {
		p, ok := reg.Providers[m.Provider]
		if !ok {
			return nil, fmt.Errorf("model %s: no provider %q", id, m.Provider)
		}
		base, err := url.Parse(p.Endpoint)
		if err != nil || base.Host == "" {
			return nil, fmt.Errorf("provider %s: endpoint isn't a URL", m.Provider)
		}
		rt.targets[id] = target{base: base, name: m.Name, key: keys[m.Provider]}
	}
	rt.proxy = &httputil.ReverseProxy{
		Rewrite: rt.rewrite,
		// -1 flushes after every write, so SSE chunks arrive as they're made
		FlushInterval: -1,
		// the proxy's own log line names the upstream and the dial error,
		// which shouldn't reach the journal any more than the client
		ErrorLog: log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			// the transport error names the upstream's address, which isn't
			// the client's business
			writeError(w, http.StatusBadGateway, "api_error", "the model's provider didn't answer; check the router's provider")
		},
	}
	return rt, nil
}

func (rt *Router) rewrite(pr *httputil.ProxyRequest) {
	t := pr.In.Context().Value(ctxKey{}).(*resolved)
	pr.Out.URL = t.url
	pr.Out.Host = t.url.Host
	// nothing of the client's identity goes on: its key is the router's
	// business, not the provider's
	pr.Out.Header.Del("Authorization")
	pr.Out.Header.Del("Cookie")
	if t.key != "" {
		pr.Out.Header.Set("Authorization", "Bearer "+t.key)
	}
	pr.Out.Header.Set("Content-Length", strconv.Itoa(len(t.body)))
	pr.Out.ContentLength = int64(len(t.body))
	pr.Out.Body = io.NopCloser(bytes.NewReader(t.body))
}

type resolved struct {
	url  *url.URL
	key  string
	body []byte
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/models" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "use GET")
			return
		}
		rt.models(w)
		return
	}
	suffix, ok := endpoints[r.URL.Path]
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
	id := model
	if role, ok := strings.CutPrefix(model, RolePrefix); ok {
		id = rt.reg.Roles[role].Default()
	}
	t, ok := rt.targets[id]
	if !ok {
		writeError(w, http.StatusNotFound, "invalid_request_error", fmt.Sprintf("no model %q; GET /v1/models lists them", model))
		return
	}
	name, _ := json.Marshal(t.name)
	fields["model"] = name
	// no HTML escaping, so <, > and & in a prompt reach the model as written
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err = enc.Encode(fields)
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "the request body couldn't be re-encoded")
		return
	}
	// joined onto the path, so an endpoint's query string stays a query
	u := *t.base
	u.Path = strings.TrimSuffix(u.Path, "/") + suffix
	u.RawPath = ""
	res := &resolved{url: &u, key: t.key, body: body}
	rt.proxy.ServeHTTP(w, r.WithContext(contextWith(r, res)))
}

func (rt *Router) models(w http.ResponseWriter) {
	type entry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	data := []entry{}
	for id, m := range rt.reg.Models {
		data = append(data, entry{id, "model", m.Provider})
	}
	for role, ro := range rt.reg.Roles {
		if ro.Default() != "" {
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
