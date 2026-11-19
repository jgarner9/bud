package mcpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

// PrivacyHeader carries the session's privacy tier. A request without it is
// treated as P0, so a client nobody configured fails closed (Suite D81)
const PrivacyHeader = "X-Bud-Privacy"

// maxBody caps a request body; MCP requests are small, and the gateway has
// to read each one whole to find its tool calls
const maxBody = 4 << 20

// refusedCode is the JSON-RPC error code for a request the P0 tool rule
// refuses, from the range JSON-RPC leaves to servers
const refusedCode = -32001

// Gateway is the http.Handler that serves every configured server at
// /<name>/mcp
type Gateway struct {
	servers map[string]*upstream
	audit   *Audit
	now     func() time.Time
	mux     *http.ServeMux
}

type upstream struct {
	name    string
	privacy []string
	url     *url.URL
	header  string // the key's header, empty for a server with no key
	value   string // the key, with its prefix
	proxy   *httputil.ReverseProxy
}

// New builds the gateway from a validated config, reading each key with
// getenv (os.Getenv outside tests)
func New(c *Config, audit *Audit, getenv func(string) string) (*Gateway, error) {
	keys, err := c.keys(getenv)
	if err != nil {
		return nil, err
	}
	g := &Gateway{servers: map[string]*upstream{}, audit: audit, now: time.Now, mux: http.NewServeMux()}
	for _, name := range sortedNames(c.Servers) {
		s := c.Servers[name]
		u, err := url.Parse(s.URL)
		if err != nil {
			return nil, fmt.Errorf("servers.%s.url: %w", name, err)
		}
		up := &upstream{name: name, privacy: s.Privacy, url: u, value: keys[name]}
		if s.Key != nil {
			up.header = s.Key.Header
		}
		up.proxy = &httputil.ReverseProxy{
			Rewrite:       up.rewrite,
			FlushInterval: -1, // server-sent events go out as they arrive
			ErrorLog:      log.New(io.Discard, "", 0),
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, fmt.Sprintf("mcp-gateway: %s didn't answer; check the upstream service", name), http.StatusBadGateway)
			},
		}
		g.servers[name] = up
	}
	g.mux.HandleFunc("/{server}/mcp", g.serve)
	return g, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) { g.mux.ServeHTTP(w, r) }

// rewrite points the outgoing request at the upstream and swaps the
// client's credentials for the server's key
func (u *upstream) rewrite(pr *httputil.ProxyRequest) {
	out := *u.url
	pr.Out.URL = &out
	pr.Out.Host = ""
	h := pr.Out.Header
	h.Del("Authorization")
	h.Del("Cookie")
	h.Del(PrivacyHeader)
	if u.header != "" {
		h.Set(u.header, u.value)
	}
	// no X-Forwarded-For: the upstream doesn't need the tailnet's addresses
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	up, ok := g.servers[r.PathValue("server")]
	if !ok {
		http.Error(w, "mcp-gateway: no such server", http.StatusNotFound)
		return
	}
	client, ok := peer(r.RemoteAddr)
	if !ok {
		http.Error(w, "mcp-gateway: only loopback and tailnet clients are served", http.StatusForbidden)
		return
	}
	if r.Header.Get("Origin") != "" || !literalHost(r.Host) {
		// a DNS-rebound page names its own domain in Host and may leave
		// Origin out of a GET, so both are checked
		http.Error(w, "mcp-gateway: requests from web pages are refused; reach the gateway by its IP address", http.StatusForbidden)
		return
	}
	tier := r.Header.Get(PrivacyHeader)
	if tier == "" {
		tier = "P0"
	}
	if !slices.Contains([]string{"P0", "P1", "P2"}, tier) {
		http.Error(w, fmt.Sprintf("mcp-gateway: %s is %q; use P0, P1 or P2", PrivacyHeader, tier), http.StatusBadRequest)
		return
	}

	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			http.Error(w, "mcp-gateway: request body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
	}
	msgs, batch := parse(body)
	start := g.now()
	base := Record{Server: up.name, Tier: tier, Client: client.String()}

	if !slices.Contains(up.privacy, tier) {
		g.refuse(w, r, up, msgs, batch, base, start)
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	sw := &statusWriter{ResponseWriter: w}
	// deferred, so a call the upstream ran is logged even when the proxy
	// unwinds with http.ErrAbortHandler (the client left mid-stream)
	defer func() {
		for _, m := range msgs {
			if m.Method == "tools/call" {
				g.log(m.record(base, "allowed", sw.code(), g.now().Sub(start)))
			}
		}
	}()
	up.proxy.ServeHTTP(sw, r)
}

// literalHost says whether the request's Host names the gateway by a
// loopback or tailnet IP address (or localhost). A DNS-rebinding page can
// only send its own domain there
func literalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return insideHost(host)
}

// refuse answers every request in the body with a JSON-RPC error, sending
// nothing upstream, and logs each one (Suite D81)
func (g *Gateway) refuse(w http.ResponseWriter, r *http.Request, up *upstream, msgs []message, batch bool, base Record, start time.Time) {
	reason := fmt.Sprintf("%s isn't open to %s sessions: they may use only tools that never leave the house (Suite D81)", up.name, base.Tier)
	var replies []json.RawMessage
	for _, m := range msgs {
		g.log(m.record(base, "refused", http.StatusForbidden, g.now().Sub(start)))
		if m.ID == nil {
			continue // a notification gets no reply
		}
		b, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      m.ID,
			"error":   map[string]any{"code": refusedCode, "message": reason},
		})
		replies = append(replies, b)
	}
	if len(msgs) == 0 {
		g.log(Record{Server: base.Server, Tier: base.Tier, Client: base.Client, Method: r.Method, Decision: "refused", Status: http.StatusForbidden})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	switch {
	case len(replies) == 0:
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": refusedCode, "message": reason}})
		_, _ = w.Write(b)
	case batch:
		b, _ := json.Marshal(replies)
		_, _ = w.Write(b)
	default:
		_, _ = w.Write(replies[0])
	}
}

func (g *Gateway) log(rec Record) {
	rec.Time = g.now().UTC()
	if err := g.audit.Write(rec); err != nil {
		// the call already happened; losing its record is worth a loud line
		log.Printf("mcp-gateway: audit log: %v", err)
	}
}

// message is the part of a JSON-RPC message the gateway reads
type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	} `json:"params"`
}

func (m message) record(base Record, decision string, status int, took time.Duration) Record {
	rec := base
	rec.Method = m.Method
	rec.Decision = decision
	rec.Status = status
	rec.Millis = took.Milliseconds()
	if m.Method == "tools/call" {
		rec.Tool = m.Params.Name
		for k := range m.Params.Arguments {
			rec.Args = append(rec.Args, k)
		}
		sort.Strings(rec.Args)
	}
	return rec
}

// parse reads one JSON-RPC message or a batch. A body that isn't JSON-RPC
// (empty, for a GET) gives no messages; the upstream answers it
func parse(body []byte) (msgs []message, batch bool) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, false
	}
	if body[0] == '[' {
		var raw []json.RawMessage
		if json.Unmarshal(body, &raw) != nil {
			return nil, true
		}
		for _, r := range raw {
			var m message
			if json.Unmarshal(r, &m) == nil {
				msgs = append(msgs, m)
			}
		}
		return msgs, true
	}
	var m message
	if json.Unmarshal(body, &m) != nil {
		return nil, false
	}
	return []message{m}, false
}

// peer parses the client's address and says whether it's on loopback or
// the tailnet
func peer(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a, insideAddr(a)
}

// statusWriter remembers the status the upstream answered with. Unwrap
// lets http.ResponseController reach the Flusher underneath, which the
// proxy needs to stream events
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	// interim 1xx answers (103 Early Hints) come before the real one
	if s.status == 0 && code >= 200 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusWriter) code() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// Serve listens on the config's address until ctx is done, then gives open
// requests five seconds to finish
func Serve(ctx context.Context, c *Config, h http.Handler) error {
	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w (is the tailnet up, and is the address this machine's?)", c.Listen, err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shut); err != nil {
			return fmt.Errorf("stop the gateway: %w", err)
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
