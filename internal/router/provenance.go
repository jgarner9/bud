package router

import (
	"bytes"
	"encoding/json"
	"strings"

	"jgarner.dev/bud/internal/registry"
)

// ProvenanceRule names the raise in X-Bud-Privacy-Raised when a P0-only
// tool's call is in the conversation (Suite D119)
const ProvenanceRule = "provenance"

// MemoryRule names the raise in X-Bud-Privacy-Raised when a Bud memory
// search brought a P0 record into the conversation (JGA-389)
const MemoryRule = "memory"

// recalled says whether a message holds registry.P0Marker, which Bud
// memory puts on each P0 record it returns. Memory isn't a P0-only server,
// since most of what it holds may go to the cloud, so tainted can't tell
// by the tool's name; the marker says a private record actually came back.
// Any message counts, whatever its role or shape, since a runtime may
// quote a tool result as text or as content parts
func recalled(fields map[string]json.RawMessage) bool {
	return bytes.Contains(fields["messages"], []byte(registry.P0Marker))
}

// tainted says whether the request's history holds a call to a tool of a
// server only P0 sessions may use (the calendar, mail), and names the first
// such tool. Its result is somewhere in the conversation, so the whole
// conversation is as private as that server, however the client tiered it.
// This is the taint D95 couldn't build inside Hermes, built at a Bud-owned
// leaf (Suite D97, D119). It reads the tool names of the assistant's
// tool_calls (and the older function_call), and the name a tool message may
// carry, so a history trimmed of the call still counts
func tainted(fields map[string]json.RawMessage, servers []string) (string, bool) {
	if len(servers) == 0 {
		return "", false
	}
	var messages []json.RawMessage
	if json.Unmarshal(fields["messages"], &messages) != nil {
		return "", false
	}
	for _, raw := range messages {
		var m struct {
			Name      string `json:"name"`
			Role      string `json:"role"`
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
			FunctionCall struct {
				Name string `json:"name"`
			} `json:"function_call"`
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		names := []string{m.FunctionCall.Name}
		if m.Role == "tool" || m.Role == "function" {
			names = append(names, m.Name)
		}
		for _, c := range m.ToolCalls {
			names = append(names, c.Function.Name)
		}
		for _, n := range names {
			if n != "" && fromServer(n, servers) {
				return n, true
			}
		}
	}

	return "", false
}

// fromServer says whether a tool name belongs to one of the servers, with
// or without a runtime's prefix: OpenCode's calendar_list_events, Hermes's
// mcp__calendar__list_events or mcp_calendar_list_events. A server name's
// '-' may be '_' in the tool name. Case doesn't count
func fromServer(name string, servers []string) bool {
	full := strings.ToLower(name)
	// the name as written, and without a runtime's mcp prefix, so a server
	// that is itself named mcp still matches its unprefixed tools
	names := []string{full}
	if rest, ok := strings.CutPrefix(full, "mcp"); ok && strings.HasPrefix(rest, "_") {
		names = append(names, strings.TrimLeft(rest, "_"))
	}
	for _, s := range servers {
		s = strings.ToLower(s)
		for _, form := range []string{s, strings.ReplaceAll(s, "-", "_")} {
			for _, n := range names {
				if strings.HasPrefix(n, form+"_") || n == form {
					return true
				}
			}
		}
	}

	return false
}
