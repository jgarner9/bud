package curator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"jgarner.dev/bud/internal/kb"
	"jgarner.dev/bud/internal/kbsite"
	"jgarner.dev/bud/internal/memory"
	"jgarner.dev/bud/internal/registry"
)

// Endpoint is one local model the curator may ask
type Endpoint struct {
	// URL is the provider's OpenAI-compatible base, ending in /v1
	URL   string
	Model string
}

// LocalModels lists the curator role's members on local providers, in the
// role's order. It never goes through the router, so a fallback can't send
// a record to a cloud model (Suite D123)
func LocalModels(reg *registry.Registry) ([]Endpoint, error) {
	role, ok := reg.Roles["curator"]
	if !ok {
		return nil, errors.New("the registry has no curator role: add one with a local model to content/env/registry.jsonc")
	}
	var out []Endpoint
	for _, id := range role.Members() {
		m, ok := reg.Models[id]
		if !ok {
			continue
		}
		p, ok := reg.Providers[m.Provider]
		if !ok || !p.Local || p.Key != "" {
			continue
		}
		out = append(out, Endpoint{URL: strings.TrimRight(p.Endpoint, "/"), Model: m.Name})
	}
	if len(out) == 0 {
		return nil, errors.New("the registry's curator role has no model on a local provider; the curator asks only local models")
	}

	return out, nil
}

const prompt = `You curate a personal knowledge base of pages about projects and systems. Read one approved memory record and list the durable facts in it.

Reply with JSON only, in this shape: {"facts": [{"key": "...", "value": "...", "statement": "...", "page": "...", "section": "..."}]}

- key is <projects|systems>/<page-slug>#<section-slug>/<name>, in lowercase letters, digits and '-', such as systems/home-server#router/port. Reuse a known page, section and key when the record is about the same thing.
- value is the fact's short value, under 100 characters, such as 8795.
- statement is one sentence that states the fact, such as "The router listens on port 8795."
- page and section are the titles to use if the page or section is new.
- Leave out anything temporary (plans for today, moods, one-off tasks), the status of work (Linear holds that), and any password, token, key or other secret value.
- A record with nothing durable in it gets {"facts": []}.`

// maxKnown bounds the known facts sent with each record
const maxKnown = 300

// ChatExtractor asks each endpoint in turn, moving on when one can't be
// reached; ErrNoModel when none can
func ChatExtractor(endpoints []Endpoint, client *http.Client) Extractor {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	// a redirect could send the record to a host that isn't local: never follow one
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &noRedirect
	return func(ctx context.Context, rec memory.ExportedRecord, known []string) ([]Fact, error) {
		if len(known) > maxKnown {
			known = known[:maxKnown]
		}
		user := fmt.Sprintf("Known pages and facts:\n%s\n\nRecord %d, from %s on %s:\n%s",
			orDefault(strings.Join(known, "\n"), "(none yet)"), rec.ID, rec.Source, day(rec.CreatedAt), rec.Body)
		var down []string
		for _, e := range endpoints {
			content, err := chat(ctx, client, e, user)
			if errors.Is(err, errUnreachable) {
				down = append(down, e.Model)
				continue
			}
			if err != nil {
				return nil, err
			}
			return parseFacts(content)
		}
		return nil, fmt.Errorf("%w (tried %s)", ErrNoModel, strings.Join(down, ", "))
	}
}

var errUnreachable = errors.New("unreachable")

func chat(ctx context.Context, client *http.Client, e Endpoint, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":           e.Model,
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%s: %w: %v", e.Model, errUnreachable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("%s: %w: %v", e.Model, errUnreachable, err)
	}
	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway {
		return "", fmt.Errorf("%s: %w: %d", e.Model, errUnreachable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d: %s", e.Model, resp.StatusCode, strings.TrimSpace(string(data[:min(len(data), 300)])))
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &reply); err != nil || len(reply.Choices) == 0 {
		return "", fmt.Errorf("%s gave no answer", e.Model)
	}

	return reply.Choices[0].Message.Content, nil
}

// parseFacts reads the model's JSON, dropping facts that don't fit the format
func parseFacts(content string) ([]Fact, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.Trim(strings.TrimSpace(content), "`")
	var reply struct {
		Facts []Fact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(content), &reply); err != nil {
		return nil, fmt.Errorf("the model's answer isn't the JSON asked for: %v", err)
	}
	var out []Fact
	for _, f := range reply.Facts {
		f.Value, f.Statement = oneLine(f.Value), oneLine(f.Statement)
		f.Page, f.Section = oneLine(f.Page), oneLine(f.Section)
		if _, err := kb.ParseKey(f.Key); err != nil || f.Value == "" || len(f.Value) > 200 || len(f.Statement) > 400 || kbsite.HasSecret(strings.Join([]string{f.Key, f.Value, f.Statement, f.Page, f.Section}, "\n")) {
			continue
		}
		out = append(out, f)
	}

	return out, nil
}
