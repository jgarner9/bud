package memorymcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Embedder turns texts into vectors, one per text and in order
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// HTTPEmbedder asks an OpenAI-compatible /embeddings endpoint, the home
// server's embedding server (server/embed/, Suite D52)
type HTTPEmbedder struct {
	endpoint string
	model    string
	dims     int
	client   *http.Client
}

// NewHTTPEmbedder embeds with model at endpoint (the registry provider's
// base URL, such as http://100.64.0.1:8082/v1). A vector of other than
// dims numbers is refused, since it would come from other weights than the
// registry names
func NewHTTPEmbedder(endpoint, model string, dims int) (*HTTPEmbedder, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("the embedding endpoint %q isn't an http or https URL", endpoint)
	}
	return &HTTPEmbedder{endpoint: u.JoinPath("embeddings").String(), model: model, dims: dims, client: &http.Client{Timeout: 60 * time.Second}}, nil
}

// maxEmbedReply bounds one reply: 16 vectors of 4,096 numbers as JSON fit
const maxEmbedReply = 8 << 20

func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the embedding server didn't answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("the embedding server answered %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var reply struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxEmbedReply)).Decode(&reply); err != nil {
		return nil, fmt.Errorf("the embedding server sent a reply bud can't read: %w", err)
	}
	if len(reply.Data) != len(texts) {
		return nil, fmt.Errorf("the embedding server sent %d vectors for %d texts", len(reply.Data), len(texts))
	}
	out := make([][]float64, len(texts))
	for _, d := range reply.Data {
		if d.Index < 0 || d.Index >= len(texts) || out[d.Index] != nil {
			return nil, errors.New("the embedding server sent vectors out of order")
		}
		if len(d.Embedding) != e.dims {
			return nil, fmt.Errorf("the embedding server sent %d dims where the registry says %d; it runs other weights", len(d.Embedding), e.dims)
		}
		out[d.Index] = d.Embedding
	}

	return out, nil
}
