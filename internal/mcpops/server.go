// Package mcpops is the Bud ops MCP server (Suite D83): read-only tools
// over bud doctor and the sync ledger, so Hermes can say whether
// everything is healthy and what's waiting on Jason. It speaks the
// stateless subset of MCP's Streamable HTTP transport; see
// docs/internals/mcp-ops.md
package mcpops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"jgarner.dev/bud/internal/version"
)

// ProtocolVersion is the MCP version answered when the client asks for one
// this server doesn't know
const ProtocolVersion = "2025-06-18"

// protocols are the versions whose stateless HTTP shape this server fits
var protocols = []string{"2025-03-26", "2025-06-18", "2025-11-25"}

// maxBody bounds a request; a tool call here is a few hundred bytes
const maxBody = 1 << 20

// Tool is one MCP tool
type Tool struct {
	Name        string
	Description string
	// Schema is the JSON Schema of the arguments, an object
	Schema map[string]any
	// OpenWorld says whether the tool reaches an external service.
	OpenWorld bool
	// Tier is ReadOnly unless the tool has an effect Jason must approve
	Tier Tier
	// Run returns the value to report as JSON. An error becomes a tool
	// result with isError set; a *ArgError becomes JSON-RPC -32602
	Run func(ctx context.Context, args json.RawMessage) (any, error)
}

// ArgError is a tool call with arguments the tool can't take
type ArgError struct{ Msg string }

func (e *ArgError) Error() string { return e.Msg }

// Server answers MCP requests at /mcp
type Server struct {
	tools              []Tool
	name, instructions string
	// mu runs one call at a time: doctor and sync_status both take the sync
	// lock, and would see each other as a running sync
	mu   sync.Mutex
	mux  *http.ServeMux
	appr Approvals
}

// New serves tools, listed in the order given
func New(tools []Tool) *Server {
	return NewReadOnly("bud-ops", "Read-only reports on Bud itself: doctor for health, sync_status for the content and overlay in use and any drift. approvals for the count of requests waiting on Jason. spend and reviews aren't built yet and say so.", tools)
}

// NewReadOnly serves read-only tools with the same loopback-only MCP transport.
func NewReadOnly(name, instructions string, tools []Tool) *Server {
	s := &Server{name: name, instructions: instructions, tools: tools, mux: http.NewServeMux()}
	s.mux.HandleFunc("/mcp", s.serve)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if !loopbackPeer(r.RemoteAddr) {
		http.Error(w, s.name+": only loopback clients are served", http.StatusForbidden)
		return
	}
	if r.Header.Get("Origin") != "" || !loopbackHost(r.Host) {
		http.Error(w, s.name+": requests from web pages are refused; reach it at its loopback IP address", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, s.name+": only POST; there's no event stream or session", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, s.name+": request body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}

	msgs, batch, ok := parse(body)
	if !ok {
		writeJSON(w, http.StatusBadRequest, failure(nil, -32700, "the body isn't JSON-RPC"))
		return
	}
	var out []response
	for _, m := range msgs {
		notification := m.Method != "" && m.ID == nil
		reply := m.Method == "" && m.ID != nil && (m.Result != nil || m.Error != nil)
		if m.JSONRPC == "2.0" && (notification || reply) {
			// nothing to answer: a notification, or a response to a request
			// this server never sends
			continue
		}
		out = append(out, s.handle(r.Context(), m))
	}
	switch {
	case len(out) == 0:
		w.WriteHeader(http.StatusAccepted)
	case batch:
		writeJSON(w, http.StatusOK, out)
	default:
		writeJSON(w, http.StatusOK, out[0])
	}
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Result and Error mark a response, which is ignored
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func failure(id json.RawMessage, code int, msg string) response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{code, msg}}
}

func success(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

// parse reads one message or a batch; ok is false when the body is neither
func parse(body []byte) (msgs []message, batch, ok bool) {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		if json.Unmarshal(body, &msgs) != nil || len(msgs) == 0 {
			return nil, true, false
		}
		return msgs, true, true
	}
	var m message
	if json.Unmarshal(body, &m) != nil {
		return nil, false, false
	}
	return []message{m}, false, true
}

func (s *Server) handle(ctx context.Context, m message) response {
	if m.JSONRPC != "2.0" || m.Method == "" || m.ID == nil {
		return failure(m.ID, -32600, "not a JSON-RPC 2.0 request")
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		v := ProtocolVersion
		if slices.Contains(protocols, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		return success(m.ID, map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": version.String()},
			"instructions":    s.instructions,
		})
	case "ping":
		return success(m.ID, map[string]any{})
	case "tools/list":
		return success(m.ID, map[string]any{"tools": s.list()})
	case "tools/call":
		return s.call(ctx, m)
	default:
		return failure(m.ID, -32601, fmt.Sprintf("%s has no method %q", s.name, m.Method))
	}
}

func (s *Server) list() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
			"annotations": map[string]any{"readOnlyHint": t.Tier == ReadOnly, "destructiveHint": t.Tier == ApprovalTier, "idempotentHint": t.Tier == ReadOnly, "openWorldHint": t.OpenWorld},
		})
	}
	return out
}

func (s *Server) call(ctx context.Context, m message) response {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return failure(m.ID, -32602, "tools/call needs params with a name")
	}
	i := slices.IndexFunc(s.tools, func(t Tool) bool { return t.Name == p.Name })
	if i < 0 {
		return failure(m.ID, -32602, fmt.Sprintf("%s has no tool %q; tools/list names them", s.name, p.Name))
	}

	v, err := s.run(ctx, s.tools[i], p.Arguments)
	if ae := (*ArgError)(nil); errors.As(err, &ae) {
		return failure(m.ID, -32602, ae.Msg)
	}
	if err != nil {
		return success(m.ID, toolResult(err.Error(), true))
	}
	text, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return success(m.ID, toolResult("encode the result: "+err.Error(), true))
	}
	return success(m.ID, toolResult(string(text), false))
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "mcp-ops: encode the response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func loopbackPeer(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.Unmap().IsLoopback()
}

// loopbackHost says whether Host names this server by a loopback IP
// address or localhost. A DNS-rebinding page can only send its own domain
func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.Unmap().IsLoopback()
}

// CheckListen says whether listen is a loopback IP address and port, the
// only place this server may listen
func CheckListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return fmt.Errorf("listen %q isn't host:port; use a loopback address such as 127.0.0.1:8792", listen)
	}
	a, err := netip.ParseAddr(host)
	if err != nil || !a.Unmap().IsLoopback() {
		return fmt.Errorf("listen %q isn't a loopback IP address; mcp-ops listens on loopback only, and devices reach it through the MCP gateway", listen)
	}
	return nil
}

// Serve listens on listen until ctx is done, then gives open requests five
// seconds to finish
func Serve(ctx context.Context, listen string, h http.Handler) error {
	if err := CheckListen(listen); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w (is another mcp-ops running?)", listen, err)
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
			return fmt.Errorf("stop mcp-ops: %w", err)
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
