package router

import "encoding/json"

// texts lists the strings of a request the privacy rules read: each chat
// message's content (a string, or the text of each {"type":"text"} part),
// a completion's prompt and an embedding's input (a string or an array of
// strings; token arrays are skipped). Anything it can't read is skipped,
// since the upstream rejects a malformed body anyway
func texts(fields map[string]json.RawMessage) []string {
	var out []string
	var messages []json.RawMessage
	if json.Unmarshal(fields["messages"], &messages) == nil {
		for _, raw := range messages {
			var m struct {
				Content json.RawMessage `json:"content"`
			}
			if json.Unmarshal(raw, &m) == nil {
				out = append(out, contentTexts(m.Content)...)
			}
		}
	}
	out = append(out, stringsOf(fields["prompt"])...)
	out = append(out, stringsOf(fields["input"])...)

	return out
}

// contentTexts reads a message's content: a string or an array of parts
func contentTexts(raw json.RawMessage) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		// null decodes to "" too, and an empty text has nothing to match
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		}
	}

	return out
}

// stringsOf reads a string, or an array of strings, skipping any other kind
// of element such as a token array
func stringsOf(raw json.RawMessage) []string {
	// a null decodes into a *string as nil, not as "", so it is skipped
	var s *string
	if json.Unmarshal(raw, &s) == nil {
		if s == nil {
			return nil
		}
		return []string{*s}
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var out []string
	for _, item := range list {
		var e *string
		if json.Unmarshal(item, &e) == nil && e != nil {
			out = append(out, *e)
		}
	}

	return out
}
