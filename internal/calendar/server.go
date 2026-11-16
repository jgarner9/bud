package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Backend is what the tools call; *Client is the real one
type Backend interface {
	Calendars(ctx context.Context) ([]Calendar, error)
	Events(ctx context.Context, calendar string, start, end time.Time) ([]Event, error)
	Create(ctx context.Context, calendar string, e NewEvent) (Event, error)
	Move(ctx context.Context, id string, start, end time.Time, allDay bool) (Event, error)
	Delete(ctx context.Context, id string) (Event, error)
}

// Server answers MCP over Streamable HTTP, one JSON answer per POST and no
// sessions, since every tool call stands alone
type Server struct {
	cal Backend
	loc *time.Location
	now func() time.Time
}

// NewServer serves the backend's calendars, reading times without a zone
// in loc
func NewServer(cal Backend, loc *time.Location) *Server {
	return &Server{cal: cal, loc: loc, now: time.Now}
}

// the MCP versions this server speaks, newest first
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

const maxBody = 1 << 20

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// MCP clients send no Origin, and name the server by its address; a
	// page that rebinds its domain to loopback does neither
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		http.Error(w, "the body is unreadable or over 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}

	var out any
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
			writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "the body isn't a JSON-RPC message"))
			return
		}
		var answers []any
		for _, m := range batch {
			if a := s.handle(r.Context(), m); a != nil {
				answers = append(answers, a)
			}
		}
		if len(answers) > 0 {
			out = answers
		}
	} else if a := s.handle(r.Context(), body); a != nil {
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
func (s *Server) handle(ctx context.Context, raw json.RawMessage) any {
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
			"serverInfo":      map[string]any{"name": "bud-calendar", "version": "1"},
			"instructions":    "Jason's calendars. Reading runs alone; creating, moving or deleting an event needs his approval. Reminders are events with alarm_minutes, which ring on his phone.",
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
		if !slices.ContainsFunc(toolList, func(t tool) bool { return t.Name == p.Name }) {
			return rpcError(req.ID, -32602, fmt.Sprintf("no tool is named %q", p.Name))
		}
		v, err := s.call(ctx, p.Name, p.Arguments)
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

const whenHelp = "A date and time such as 2026-11-18T14:30 (in the server's zone) or 2026-11-18T14:30:00-06:00, or a date such as 2026-11-18."

// Reads are marked read-only, so Hermes runs them alone; the rest are not,
// so a server Hermes doesn't trust asks Jason first (Suite D82, D10)
var toolList = []tool{
	{
		Name:        "list_calendars",
		Title:       "List calendars",
		Description: "Lists the calendars, with the id and name each tool takes.",
		InputSchema: object(nil, map[string]any{}),
		Annotations: map[string]any{"readOnlyHint": true},
	},
	{
		Name:        "list_events",
		Title:       "List events",
		Description: "Lists the events between start and end, sorted by start, with repeating events as their occurrences. With no start it's from the start of today, and with no end it's the 7 days after start, so no arguments gives this week. At most 366 days.",
		InputSchema: object(nil, map[string]any{
			"start":    str("Where the list starts. " + whenHelp),
			"end":      str("Where it ends, not included. " + whenHelp),
			"calendar": str("One calendar's name or id. Leave it out for every calendar."),
		}),
		Annotations: map[string]any{"readOnlyHint": true},
	},
	{
		Name:        "create_event",
		Title:       "Create an event",
		Description: "Creates an event. Give alarm_minutes to make it a reminder that rings on Jason's phone that many minutes before the start (0 rings at the start). A start that's only a date makes an all-day event.",
		InputSchema: object([]string{"title", "start"}, map[string]any{
			"title":            str("What the event is called."),
			"start":            str("When it starts. " + whenHelp),
			"end":              str("When it ends; for an all-day event, its last day. Leave it out for duration_minutes, or a one-day all-day event."),
			"duration_minutes": map[string]any{"type": "integer", "minimum": 1, "description": "How long it runs when end is left out. Default 60."},
			"calendar":         str("The calendar's name or id. Needed when there's more than one."),
			"location":         str("Where it is."),
			"notes":            str("Notes on the event."),
			"alarm_minutes":    map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 0, "maximum": 40320}, "description": "Minutes before the start at which to alert, such as [10] or [1440, 60]."},
		}),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false},
	},
	{
		Name:        "move_event",
		Title:       "Move an event",
		Description: "Moves an event that doesn't repeat to a new start, keeping its length unless end is given. Everything else about it stays.",
		InputSchema: object([]string{"id", "start"}, map[string]any{
			"id":    str("The event's id from list_events."),
			"start": str("The new start. " + whenHelp),
			"end":   str("The new end; for an all-day event, its last day."),
		}),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true},
	},
	{
		Name:        "delete_event",
		Title:       "Delete an event",
		Description: "Deletes an event that doesn't repeat.",
		InputSchema: object([]string{"id"}, map[string]any{
			"id": str("The event's id from list_events."),
		}),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true},
	},
}

// maxRange caps a listing, so one call can't pull years of calendar
const maxRange = 366 * 24 * time.Hour

func (s *Server) call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var a struct {
		Start    string `json:"start"`
		End      string `json:"end"`
		Calendar string `json:"calendar"`
		Title    string `json:"title"`
		Duration *int   `json:"duration_minutes"`
		Location string `json:"location"`
		Notes    string `json:"notes"`
		Alarms   []int  `json:"alarm_minutes"`
		ID       string `json:"id"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&a); err != nil {
			return nil, fmt.Errorf("the arguments don't fit %s: %w", name, err)
		}
	}

	switch name {
	case "list_calendars":
		cals, err := s.cal.Calendars(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"calendars": nonNil(cals)}, nil

	case "list_events":
		y, m, d := s.now().In(s.loc).Date()
		start := time.Date(y, m, d, 0, 0, 0, 0, s.loc)
		if a.Start != "" {
			t, _, err := parseWhen(a.Start, s.loc)
			if err != nil {
				return nil, fmt.Errorf("start: %w", err)
			}
			start = t
		}
		end := start.AddDate(0, 0, 7)
		if a.End != "" {
			t, _, err := parseWhen(a.End, s.loc)
			if err != nil {
				return nil, fmt.Errorf("end: %w", err)
			}
			end = t
		}
		if !end.After(start) {
			return nil, errors.New("end must come after start")
		}
		if end.Sub(start) > maxRange {
			return nil, errors.New("list at most 366 days at a time")
		}
		evs, err := s.cal.Events(ctx, a.Calendar, start, end)
		if err != nil {
			return nil, err
		}
		return map[string]any{"start": start, "end": end, "events": nonNil(evs)}, nil

	case "create_event":
		e, err := s.newEvent(a.Title, a.Start, a.End, a.Duration, a.Alarms)
		if err != nil {
			return nil, err
		}
		e.Location, e.Notes = a.Location, a.Notes
		ev, err := s.cal.Create(ctx, a.Calendar, e)
		if err != nil {
			return nil, err
		}
		return map[string]any{"created": ev}, nil

	case "move_event":
		if a.ID == "" {
			return nil, errors.New("id: give the event's id from list_events")
		}
		start, allDay, err := parseWhen(a.Start, s.loc)
		if err != nil {
			return nil, fmt.Errorf("start: %w", err)
		}
		var end time.Time
		if a.End != "" {
			if end, _, err = parseWhen(a.End, s.loc); err != nil {
				return nil, fmt.Errorf("end: %w", err)
			}
			if allDay {
				end = end.AddDate(0, 0, 1)
			}
			if !end.After(start) {
				return nil, errors.New("end must come after start")
			}
		}
		ev, err := s.cal.Move(ctx, a.ID, start, end, allDay)
		if err != nil {
			return nil, err
		}
		return map[string]any{"moved": ev}, nil

	case "delete_event":
		if a.ID == "" {
			return nil, errors.New("id: give the event's id from list_events")
		}
		ev, err := s.cal.Delete(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": ev}, nil
	}

	return nil, fmt.Errorf("no tool is named %q", name)
}

// newEvent checks create's arguments
func (s *Server) newEvent(title, startS, endS string, duration *int, alarms []int) (NewEvent, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return NewEvent{}, errors.New("title: give the event a title")
	}
	if utf8.RuneCountInString(title) > 500 {
		return NewEvent{}, errors.New("title: keep it under 500 characters")
	}
	start, allDay, err := parseWhen(startS, s.loc)
	if err != nil {
		return NewEvent{}, fmt.Errorf("start: %w", err)
	}
	e := NewEvent{Title: title, Start: start, AllDay: allDay}
	switch {
	case endS != "" && duration != nil:
		return NewEvent{}, errors.New("give end or duration_minutes, not both")
	case endS != "":
		end, _, err := parseWhen(endS, s.loc)
		if err != nil {
			return NewEvent{}, fmt.Errorf("end: %w", err)
		}
		if allDay {
			// the last day is included; DTEND is the day after it
			y, m, d := end.Date()
			end = time.Date(y, m, d, 0, 0, 0, 0, s.loc).AddDate(0, 0, 1)
		}
		e.End = end
	case allDay:
		e.End = start.AddDate(0, 0, 1)
	default:
		mins := 60
		if duration != nil {
			mins = *duration
		}
		if mins < 1 {
			return NewEvent{}, errors.New("duration_minutes: give at least 1")
		}
		e.End = start.Add(time.Duration(mins) * time.Minute)
	}
	if !e.End.After(e.Start) {
		return NewEvent{}, errors.New("end must come after start")
	}
	for _, m := range alarms {
		if m < 0 || m > 40320 {
			return NewEvent{}, fmt.Errorf("alarm_minutes: %d is outside 0 to 40320 (four weeks)", m)
		}
	}
	slices.Sort(alarms)
	e.Alarms = slices.Compact(alarms)

	return e, nil
}

// parseWhen reads a tool's time: RFC 3339, a local date and time, or a
// date, which it reports as all day
func parseWhen(v string, loc *time.Location) (time.Time, bool, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, false, nil
	}
	for _, f := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(f, v, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return t, true, nil
	}

	return time.Time{}, false, fmt.Errorf("%q isn't a time. %s", v, whenHelp)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
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

// CheckListen accepts only a loopback host:port, since the MCP gateway is
// what devices reach and the calendar server sits behind it
func CheckListen(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("--listen %q isn't host:port, such as 127.0.0.1:8791", addr)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--listen %q must be a loopback address such as 127.0.0.1:8791: devices reach the calendar through the MCP gateway", addr)
	}

	return nil
}

// Serve listens on addr until ctx ends
func Serve(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shut)
	}
}
