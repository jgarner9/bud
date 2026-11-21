package mcpgateway

import (
	"bytes"
	"encoding/json"
)

// texts lists the strings of a request body the privacy rules read: every
// string value inside each JSON-RPC message's params, however deeply nested
// (a tool call's arguments, a prompt's arguments, a resource's URI). Object
// keys are skipped, since they're the tool's own field names. A body that
// isn't JSON gives nothing: the approval gate or the upstream refuses it
func texts(body []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	msgs, ok := v.([]any)
	if !ok {
		msgs = []any{v}
	}
	var out []string
	for _, m := range msgs {
		if obj, ok := m.(map[string]any); ok {
			out = appendStrings(out, obj["params"])
		}
	}

	return out
}

// appendStrings adds every non-empty string value under v to out
func appendStrings(out []string, v any) []string {
	switch v := v.(type) {
	case string:
		if v != "" {
			out = append(out, v)
		}
	case []any:
		for _, e := range v {
			out = appendStrings(out, e)
		}
	case map[string]any:
		for _, e := range v {
			out = appendStrings(out, e)
		}
	}

	return out
}
