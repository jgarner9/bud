package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Listing is one model as a provider's GET /models reports it. Facts is nil
// when the provider lists only IDs, as most OpenAI-compatible servers do;
// OpenCode Go also reports each model's inputs, capabilities, limits and
// prices (Suite D69)
type Listing struct {
	ID    string
	Facts *Facts
}

// Facts are the capability fields a provider can tell bud, so bud registry
// refresh keeps them true and hand-kept fields alone (Suite D64, D69)
type Facts struct {
	Inputs  []string
	Caps    []string
	Context int
	Output  int
	Price   *Price
}

// listedCaps are the caps a model list reports. fim and embed are declared
// by hand, so a refresh keeps them
var listedCaps = []string{"tools", "reasoning", "json"}

// catalogModel is the part of a /models entry bud reads
type catalogModel struct {
	ID         string `json:"id"`
	Modalities *struct {
		Input []string `json:"input"`
	} `json:"modalities"`
	Capabilities *struct {
		Tools *struct {
			Supported bool `json:"supported"`
		} `json:"tools"`
		Reasoning *struct {
			Supported bool `json:"supported"`
		} `json:"reasoning"`
		StructuredOutput bool `json:"structured_output"`
	} `json:"capabilities"`
	Limit *struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Cost *struct {
		Text *struct {
			Input *struct {
				Price float64 `json:"price"`
			} `json:"input"`
			Output *struct {
				Price float64 `json:"price"`
			} `json:"output"`
		} `json:"text"`
	} `json:"cost"`
}

// ParseCatalog reads a /models answer: {"data": [{"id": ...}, ...]}
func ParseCatalog(r io.Reader) ([]Listing, error) {
	var list struct {
		Data *[]catalogModel `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 8<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("read the model list: %w", err)
	}
	if list.Data == nil {
		return nil, fmt.Errorf("read the model list: it has no data array")
	}
	out := make([]Listing, 0, len(*list.Data))
	for _, m := range *list.Data {
		out = append(out, Listing{ID: m.ID, Facts: m.facts()})
	}

	return out, nil
}

// facts turns a catalog entry into Facts, or nil when it carries none
func (m catalogModel) facts() *Facts {
	if m.Modalities == nil && m.Capabilities == nil && m.Limit == nil && m.Cost == nil {
		return nil
	}
	f := &Facts{}
	if m.Modalities != nil {
		for _, in := range m.Modalities.Input {
			kind := in
			switch {
			case strings.HasPrefix(in, "image"):
				kind = "image"
			case strings.HasPrefix(in, "video"):
				kind = "video"
			case strings.HasPrefix(in, "audio"):
				kind = "audio"
			case in == "application/pdf":
				kind = "pdf"
			}
			if slices.Contains(Inputs, kind) && !slices.Contains(f.Inputs, kind) {
				f.Inputs = append(f.Inputs, kind)
			}
		}
		f.Inputs = inOrder(f.Inputs, Inputs)
	}
	if c := m.Capabilities; c != nil {
		if c.Tools != nil && c.Tools.Supported {
			f.Caps = append(f.Caps, "tools")
		}
		if c.Reasoning != nil && c.Reasoning.Supported {
			f.Caps = append(f.Caps, "reasoning")
		}
		if c.StructuredOutput {
			f.Caps = append(f.Caps, "json")
		}
	}
	if m.Limit != nil {
		f.Context, f.Output = m.Limit.Context, m.Limit.Output
	}
	if m.Cost != nil && m.Cost.Text != nil && m.Cost.Text.Input != nil && m.Cost.Text.Output != nil {
		f.Price = &Price{In: m.Cost.Text.Input.Price, Out: m.Cost.Text.Output.Price}
	}

	return f
}

// inOrder sorts xs by where each appears in order
func inOrder(xs, order []string) []string {
	slices.SortStableFunc(xs, func(a, b string) int { return slices.Index(order, a) - slices.Index(order, b) })
	return xs
}

// Fetch GETs the provider's model list. A key given as {env:VAR} is sent
// when the variable is set; an op:// key is never read (Suite D7), and Go's
// list needs none
func Fetch(ctx context.Context, client *http.Client, p Provider) ([]Listing, error) {
	base, err := url.Parse(p.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch the model list: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.JoinPath("models").String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch the model list: %w", err)
	}
	if v, ok := strings.CutPrefix(p.Key, "{env:"); ok {
		if key := os.Getenv(strings.TrimSuffix(v, "}")); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch the model list from %s: %w", base.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch the model list from %s: %s", base.Host, resp.Status)
	}

	return ParseCatalog(resp.Body)
}

// Drift is one fact the provider reports differently from the registry
type Drift struct {
	Model, Field, Was, Now string
}

// Report is what a recheck of one provider found (Suite D69)
type Report struct {
	Provider string
	Listed   int
	// Checked are the registry's models on the provider, sorted
	Checked []string
	// Gone are registry models the provider no longer lists
	Gone []string
	// New are listed models the registry doesn't have, as provider names
	New   []string
	Drift []Drift
}

// Compare rechecks the registry's models on one provider against its list
func Compare(r *Registry, provider string, list []Listing) Report {
	rep := Report{Provider: provider, Listed: len(list)}
	byName := map[string]Listing{}
	for _, l := range list {
		byName[l.ID] = l
	}
	known := map[string]bool{}
	for _, id := range sortedKeys(r.Models) {
		m := r.Models[id]
		if m.Provider != provider {
			continue
		}
		known[m.Name] = true
		rep.Checked = append(rep.Checked, id)
		l, ok := byName[m.Name]
		if !ok {
			rep.Gone = append(rep.Gone, id)
			continue
		}
		if l.Facts == nil {
			continue
		}
		next := ApplyFacts(m, *l.Facts)
		for _, field := range factFields {
			if was, now := factText(m, field), factText(next, field); was != now {
				rep.Drift = append(rep.Drift, Drift{Model: id, Field: field, Was: was, Now: now})
			}
		}
	}
	for _, l := range list {
		if !known[l.ID] {
			rep.New = append(rep.New, l.ID)
		}
	}
	slices.Sort(rep.New)

	return rep
}

// ApplyFacts returns m with the provider's facts in place. Caps the list
// can't report (fim, embed) are kept; a zero fact leaves the field alone
func ApplyFacts(m Model, f Facts) Model {
	m.Inputs = slices.Clone(f.Inputs)
	caps := slices.Clone(f.Caps)
	for _, c := range m.Caps {
		if !slices.Contains(listedCaps, c) && !slices.Contains(caps, c) {
			caps = append(caps, c)
		}
	}
	m.Caps = inOrder(caps, Caps)
	if f.Context > 0 {
		m.Context = f.Context
	}
	if f.Output > 0 {
		m.Output = f.Output
	}
	if f.Price != nil {
		p := *f.Price
		m.Price = &p
	}

	return m
}

// factFields are the model fields a refresh may rewrite, in file order
var factFields = []string{"inputs", "caps", "context", "output", "price"}

// factText renders a fact field as it is written in the registry file,
// or empty when the field is unset
func factText(m Model, field string) string {
	list := func(xs []string) string {
		if len(xs) == 0 {
			return ""
		}
		return `["` + strings.Join(xs, `", "`) + `"]`
	}
	num := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	switch field {
	case "inputs":
		return list(m.Inputs)
	case "caps":
		return list(m.Caps)
	case "context":
		return num(m.Context)
	case "output":
		return num(m.Output)
	case "price":
		if m.Price == nil {
			return ""
		}
		return fmt.Sprintf(`{ "in": %s, "out": %s }`, money(m.Price.In), money(m.Price.Out))
	}

	return ""
}

func money(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
