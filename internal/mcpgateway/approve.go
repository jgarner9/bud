package mcpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"jgarner.dev/bud/internal/approval"
)

// canceledStatus is nginx's "client closed request", used only in the audit
// log: the client left, so nothing was answered
const canceledStatus = 499

// holdForApproval handles a request that holds an approval-tier tools/call
// (r6b approvals design, section 4). It returns false when the request has
// none, and the caller forwards it unchanged. Otherwise it answers or forwards
// the request itself and returns true. Every failure denies: nothing goes
// upstream unless Dispatch consumed an approval for these exact bytes
func (g *Gateway) holdForApproval(w http.ResponseWriter, r *http.Request, up *upstream, body []byte, msgs []message, batch bool, base Record, start time.Time) bool {
	if reason := unreadable(body); reason != "" {
		// the typed parse drops what it can't read, so a call it dropped would
		// go upstream unreviewed; refuse instead
		rec := base
		rec.Method, rec.Decision, rec.Status, rec.Millis = "tools/call", "refused", http.StatusOK, g.now().Sub(start).Milliseconds()
		g.log(rec)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(toolResult(firstID(body), reason))
		return true
	}
	var asked []message
	for _, m := range msgs {
		if m.Method != "tools/call" {
			continue
		}
		// the parse kept each argument's raw bytes, and a duplicate key's
		// last value, as the upstream reads it
		args, _ := json.Marshal(m.Params.Arguments)
		if up.approval.Asks(m.Params.Name, args) {
			asked = append(asked, m)
		}
	}
	if len(asked) == 0 {
		return false
	}
	if batch {
		// one approval can't cover a batch, and splitting it would change what
		// the client sent
		var replies []json.RawMessage
		for _, m := range msgs {
			g.log(m.record(base, "refused", http.StatusOK, g.now().Sub(start)))
			if m.ID != nil {
				replies = append(replies, toolResult(m.ID, "approval-tier calls can't be sent in a JSON-RPC batch; send each call on its own"))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if len(replies) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return true
		}
		b, _ := json.Marshal(replies)
		_, _ = w.Write(b)
		return true
	}
	m := asked[0]
	var raw struct {
		Params struct {
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return false // parse accepted it, so this can't happen; the upstream judges it
	}

	session := r.Header.Get("Mcp-Session-Id")
	if session == "" {
		session = base.Client + "/" + base.Tier
	}
	scope := approval.Scope{Runtime: "mcp-gateway", Profile: base.Tier, Surface: up.name, Actor: base.Client, Session: session}
	call := approval.Invocation{
		Action:    "mcp:" + up.name + "/" + m.Params.Name,
		Target:    up.name + " " + up.publicURL(),
		Arguments: raw.Params.Arguments,
	}
	var rv approval.Review
	if g.appr.Review != nil {
		rv = g.appr.Review(scope)
	}
	var deadline time.Time
	if g.appr.Timeout > 0 {
		deadline = g.now().Add(g.appr.Timeout)
	}

	forwarded := false
	err := g.appr.Store.Dispatch(r.Context(), scope, call, deadline, rv, func(ctx context.Context, _ approval.Invocation) error {
		forwarded = true
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		sw := &statusWriter{ResponseWriter: w}
		// deferred like the plain path, so a call the upstream ran is logged
		// even when the proxy unwinds because the client left
		defer func() { g.log(m.record(base, "approved", sw.code(), g.now().Sub(start))) }()
		up.proxy.ServeHTTP(sw, r.WithContext(ctx))
		return nil
	})
	if forwarded {
		return true
	}

	decision, reason := "failed", "the approval store failed, so the call was not made"
	switch {
	case errors.Is(err, approval.ErrDenied):
		decision, reason = "denied", "the call was denied by the reviewer and was not made"
	case errors.Is(err, context.Canceled):
		g.log(m.record(base, "canceled", canceledStatus, g.now().Sub(start)))
		return true // the client left; nobody to answer
	case errors.Is(err, context.DeadlineExceeded):
		decision, reason = "expired", "no one answered the approval request in time, so the call was not made"
	case errors.Is(err, approval.ErrReviewUnavailable):
		decision, reason = "unavailable", "approval reviews are unavailable, so the call was not made"
	case errors.Is(err, approval.ErrReviewMismatch):
		reason = "the approval reply didn't match the request, so the call was not made"
	}
	g.log(m.record(base, decision, http.StatusOK, g.now().Sub(start)))
	w.Header().Set("Content-Type", "application/json")
	if m.ID == nil {
		w.WriteHeader(http.StatusAccepted) // a notification gets no reply
		return true
	}
	_, _ = w.Write(toolResult(m.ID, reason))
	return true
}

// toolResult is a normal tool result marked isError, so the model can tell
// the user why the call wasn't made
func toolResult(id json.RawMessage, reason string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "mcp-gateway: " + reason}},
		},
	})
	return b
}

// publicURL is scheme://host/path only: userinfo, query and fragment can carry
// credentials, and this reaches the prompt and the store
func (u *upstream) publicURL() string {
	c := url.URL{Scheme: u.url.Scheme, Host: u.url.Host, Path: u.url.Path}
	return c.String()
}

// unreadable returns why a body must not be forwarded, or "" when every
// message in it can be classified. It reads raw JSON: a tools/call needs
// readable params and name, and arguments, when present, must be an object
func unreadable(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	items := []json.RawMessage{body}
	if body[0] == '[' {
		items = nil
		if json.Unmarshal(body, &items) != nil {
			return "the request couldn't be read, so the call was not made"
		}
	}
	for _, it := range items {
		var m struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(it, &m) != nil {
			return "the request couldn't be read, so the call was not made"
		}
		if m.Method != "tools/call" {
			continue
		}
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(m.Params, &p) != nil || p.Name == "" {
			return "the tool call's params or name couldn't be read, so the call was not made"
		}
		if len(p.Arguments) > 0 && !bytes.HasPrefix(bytes.TrimSpace(p.Arguments), []byte("{")) {
			return "the tool call's arguments aren't a JSON object, so the call was not made"
		}
	}
	return ""
}

// firstID is the id of the first message that has one, or null
func firstID(body []byte) json.RawMessage {
	body = bytes.TrimSpace(body)
	var items []json.RawMessage
	if len(body) > 0 && body[0] == '[' {
		_ = json.Unmarshal(body, &items)
	} else {
		items = []json.RawMessage{body}
	}
	for _, it := range items {
		var m struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(it, &m) == nil && len(m.ID) > 0 {
			return m.ID
		}
	}
	return json.RawMessage("null")
}
