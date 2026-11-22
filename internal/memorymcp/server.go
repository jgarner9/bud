// Package memorymcp is bud memory-mcp (Suite D102, D108): the MCP server
// through which every runtime reaches Bud's working memory, behind the MCP
// gateway. It searches and stages records in the memory service
// (internal/memory) and embeds them on the home server's embedding server,
// so no runtime keeps a memory store of its own (JGA-313). See
// docs/internals/memory-mcp.md
package memorymcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/registry"
)

// Searcher finds approved records within one token's tiers
type Searcher interface {
	Retrieve(ctx context.Context, q memory.Query) ([]memory.Record, error)
}

// Stager stages records for review
type Stager interface {
	Stage(ctx context.Context, d memory.Draft) (int64, error)
}

// Vectors lists and stores the vectors a model still needs
type Vectors interface {
	Pending(ctx context.Context, model string, after int64, limit int) ([]memory.Unembedded, error)
	PutVector(ctx context.Context, id int64, model string, values []float64, bodyHash string) error
}

// Server answers MCP over Streamable HTTP, one JSON answer per POST and no
// sessions, since every tool call stands alone
type Server struct {
	// Search holds one identity per session tier, each allowed only what a
	// session of that tier may see: P0 sees P0 to P2, P1 sees P1 and P2,
	// P2 sees P2. The memory service enforces it, not this server
	Search map[string]Searcher
	// Stage is the identity that stages, allowed every tier, since a
	// session may stage a record more private than itself
	Stage Stager
	// Embed makes query vectors with Model, the registry's embedding model
	Embed Embedder
	Model string
}

// the MCP versions this server speaks, newest first
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

const maxBody = 1 << 20

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// MCP clients send no Origin, and the gateway names this server by its
	// loopback address; a page that rebinds its domain to loopback does
	// neither
	if r.Header.Get("Origin") != "" || !loopbackHost(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "only POST: this server sends no event stream", http.StatusMethodNotAllowed)
		return
	}
	// the gateway sets the tier it checked (Suite D108); none is P0, so a
	// caller that skipped the gateway sees at most what a P0 session may
	tier, ok := registry.RequestTier(r.Header.Get(registry.PrivacyHeader))
	if !ok {
		http.Error(w, fmt.Sprintf("%s is %q; use P0, P1 or P2", registry.PrivacyHeader, tier), http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		http.Error(w, "the body is unreadable or over 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}

	var out any
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
			writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "the body isn't a JSON-RPC message"))
			return
		}
		var answers []any
		for _, m := range batch {
			if a := s.handle(r.Context(), tier, m); a != nil {
				answers = append(answers, a)
			}
		}
		if len(answers) > 0 {
			out = answers
		}
	} else if a := s.handle(r.Context(), tier, body); a != nil {
		out = a
	}
	if out == nil {
		// only notifications, which take no answer
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcError(id json.RawMessage, code int, msg string) response {
	if id == nil {
		id = json.RawMessage("null")
	}

	return response{JSONRPC: "2.0", ID: id, Error: &rpcErr{Code: code, Message: msg}}
}

// handle answers one message, or returns nil for a notification
func (s *Server) handle(ctx context.Context, tier string, raw json.RawMessage) any {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		return rpcError(nil, -32600, "not a JSON-RPC 2.0 request")
	}
	if req.ID == nil {
		return nil
	}
	ok := func(v any) any { return response{JSONRPC: "2.0", ID: req.ID, Result: v} }
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := protocolVersions[0]
		if slices.Contains(protocolVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		return ok(map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "bud-memory", "version": "1"},
			"instructions":    "Bud's shared working memory, the same for every runtime. Search it before asking Jason something he may have said before. Stage a fact worth keeping; it becomes searchable once Jason approves it. Don't keep a memory file of your own.",
		})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": toolList})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcError(req.ID, -32602, "tools/call takes a name and arguments")
		}
		var v any
		var err error
		switch p.Name {
		case "memory_search":
			v, err = s.search(ctx, tier, p.Arguments)
		case "memory_stage":
			v, err = s.stage(ctx, tier, p.Arguments)
		default:
			return rpcError(req.ID, -32602, fmt.Sprintf("no tool is named %q", p.Name))
		}
		if err != nil {
			return ok(map[string]any{"content": []any{textContent(err.Error())}, "isError": true})
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return ok(map[string]any{"content": []any{textContent(string(b))}, "structuredContent": v})
	default:
		return rpcError(req.ID, -32601, fmt.Sprintf("method %q isn't supported", req.Method))
	}
}

func textContent(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
}

func object(required []string, props map[string]any) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}

	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

const (
	maxQuery    = 4096
	maxBodyText = 16000
	maxLimit    = 20
	defLimit    = 8
)

var toolList = []tool{
	{
		Name:        "memory_search",
		Title:       "Search memory",
		Description: "Searches Bud's approved memory by meaning and by words together. Each result carries where it came from (source), its privacy tier, when it was saved and how it matched. Only records this session's privacy tier may see come back.",
		InputSchema: object([]string{"query"}, map[string]any{
			"query":  str("What to look for, in plain words."),
			"source": str("Only records saved by this source, such as hermes or opencode."),
			"since":  str("Only records saved on or after this date, YYYY-MM-DD."),
			"until":  str("Only records saved on or before this date, YYYY-MM-DD."),
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxLimit, "description": "How many results. Default 8."},
		}),
		Annotations: map[string]any{"readOnlyHint": true},
	},
	{
		Name:        "memory_stage",
		Title:       "Save to memory",
		Description: "Stages a record for Jason to review; it's searchable once he approves it. Saving the same record again changes nothing, so a retry is safe. Its privacy defaults to this session's tier, and may be set more private (P0 is the most private), never less.",
		InputSchema: object([]string{"source", "kind", "body"}, map[string]any{
			"source":  str("Who is saving it: the runtime, such as hermes or opencode. Lowercase letters, digits and '-'."),
			"kind":    map[string]any{"type": "string", "enum": []string{"observation", "fact", "summary"}, "description": "observation (something seen), fact (something true to keep) or summary (of a longer exchange)."},
			"body":    str("The record, written to stand on its own."),
			"privacy": map[string]any{"type": "string", "enum": registry.Tiers, "description": "P0 private, P1 internal, P2 public. Default this session's tier. A P0 record comes back only into chats that stay on local models, so give a fact that isn't private P1."},
		}),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true},
	},
}

// Result is one search result
type Result struct {
	// Marker is registry.P0Marker on a P0 record and empty otherwise. It
	// comes first, so a runtime that cuts a long result short keeps it
	// with every record it keeps
	Marker  string   `json:"marker,omitempty"`
	ID      int64    `json:"id"`
	Source  string   `json:"source"`
	Kind    string   `json:"kind"`
	Privacy string   `json:"privacy"`
	Saved   string   `json:"saved"`
	Matched []string `json:"matched"`
	Body    string   `json:"body"`
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Server) search(ctx context.Context, tier string, raw json.RawMessage) (any, error) {
	var a struct {
		Query  string `json:"query"`
		Source string `json:"source"`
		Since  string `json:"since"`
		Until  string `json:"until"`
		Limit  *int   `json:"limit"`
	}
	if err := strictArgs(raw, &a); err != nil {
		return nil, err
	}
	a.Query = strings.TrimSpace(a.Query)
	switch {
	case a.Query == "":
		return nil, errors.New("query: say what to look for")
	case utf8.RuneCountInString(a.Query) > maxQuery:
		return nil, fmt.Errorf("query: at most %d characters", maxQuery)
	}
	for name, d := range map[string]string{"since": a.Since, "until": a.Until} {
		if _, err := time.Parse("2006-01-02", d); d != "" && (err != nil || !dateRe.MatchString(d)) {
			return nil, fmt.Errorf("%s: %q isn't a date; use YYYY-MM-DD", name, d)
		}
	}
	if a.Since != "" && a.Until != "" && a.Since > a.Until {
		return nil, errors.New("since comes after until")
	}
	limit := defLimit
	if a.Limit != nil {
		if *a.Limit < 1 || *a.Limit > maxLimit {
			return nil, fmt.Errorf("limit: give 1 to %d", maxLimit)
		}
		limit = *a.Limit
	}
	mem, ok := s.Search[tier]
	if !ok {
		return nil, fmt.Errorf("memory search isn't set up for %s sessions on this server", tier)
	}

	note := ""
	q := memory.Query{Query: a.Query, Source: a.Source, Since: a.Since, Until: a.Until, Limit: limit}
	if vecs, err := s.Embed.Embed(ctx, []string{a.Query}); err == nil {
		q.Model, q.Values = s.Model, vecs[0]
	} else {
		// the meaning half is down; words still find most things, and the
		// service applies the filters to them the same way
		note = "The embedding server is down, so this matched by words only (" + err.Error() + ")."
	}
	recs, err := mem.Retrieve(ctx, q)
	if err != nil {
		return nil, unavailable(err, "nothing was searched")
	}
	out := map[string]any{"results": results(recs)}
	if note != "" {
		out["note"] = note
	}

	return out, nil
}

func results(recs []memory.Record) []Result {
	out := []Result{}
	for _, r := range recs {
		marker := ""
		if r.Privacy == "P0" {
			marker = registry.P0Marker
		}
		out = append(out, Result{Marker: marker, ID: r.ID, Source: r.Source, Kind: r.Kind, Privacy: r.Privacy, Saved: r.CreatedAt, Matched: r.Matches, Body: r.Body})
	}

	return out
}

var sourceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func (s *Server) stage(ctx context.Context, tier string, raw json.RawMessage) (any, error) {
	var a struct {
		Source  string `json:"source"`
		Kind    string `json:"kind"`
		Body    string `json:"body"`
		Privacy string `json:"privacy"`
	}
	if err := strictArgs(raw, &a); err != nil {
		return nil, err
	}
	a.Body = strings.TrimSpace(a.Body)
	if a.Privacy == "" {
		a.Privacy = tier
	}
	switch {
	case !sourceRe.MatchString(a.Source):
		return nil, errors.New("source: name the runtime saving it, such as hermes, in lowercase letters, digits and '-'")
	case !slices.Contains([]string{"observation", "fact", "summary"}, a.Kind):
		return nil, fmt.Errorf("kind: %q isn't observation, fact or summary", a.Kind)
	case a.Body == "":
		return nil, errors.New("body: give the record to save")
	case utf8.RuneCountInString(a.Body) > maxBodyText:
		return nil, fmt.Errorf("body: at most %d characters; save a summary instead", maxBodyText)
	case !slices.Contains(registry.Tiers, a.Privacy):
		return nil, fmt.Errorf("privacy: %q isn't P0, P1 or P2", a.Privacy)
	case a.Privacy != "P0" && strings.Contains(a.Body, registry.P0Marker):
		// the marker says a P0 record came back, so only P0 may carry it
		return nil, fmt.Errorf("body: holds %s, the marker of a private record; save it as P0, or leave the marker out", registry.P0Marker)
	case rank(a.Privacy) > rank(tier):
		// a P0 session's text may hold private data, so it can't be saved
		// where a cloud session would read it
		return nil, fmt.Errorf("privacy: a %s session can't save a %s record, since that would let less private sessions read it; use %s or more private", tier, a.Privacy, tier)
	}
	// the key is the content, so a retry finds the record it already made
	sum := sha256.Sum256([]byte(a.Kind + "\x00" + a.Privacy + "\x00" + a.Body))
	id, err := s.Stage.Stage(ctx, memory.Draft{Source: a.Source, DedupeKey: hex.EncodeToString(sum[:]), Kind: a.Kind, Privacy: a.Privacy, Body: a.Body})
	if err != nil {
		return nil, unavailable(err, "nothing was saved, so save it again later")
	}

	return map[string]any{"id": id, "privacy": a.Privacy, "state": "staged for Jason's review"}, nil
}

// rank orders tiers by how many sessions may read them: P0 fewest
func rank(tier string) int { return slices.Index(registry.Tiers, tier) }

// unavailable words a memory-service failure for the model to tell the
// user, saying what didn't happen
func unavailable(err error, what string) error {
	if errors.Is(err, memory.ErrUnavailable) {
		return fmt.Errorf("Bud memory is unavailable (the memory service isn't running on the home server); %s", what)
	}
	if se, ok := errors.AsType[*memory.StatusError](err); ok {
		return fmt.Errorf("Bud memory refused the request (%d %s); %s", se.Status, se.Reason, what)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("Bud memory didn't answer in time; %s", what)
	}

	return fmt.Errorf("Bud memory failed: %v; %s", err, what)
}

// strictArgs decodes a tool's arguments, refusing names it doesn't take
func strictArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("arguments: %v", err)
	}

	return nil
}

// loopbackHost says whether a Host header names a loopback address or
// localhost
func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
