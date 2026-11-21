package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"jgarner.dev/bud/internal/registry"
)

// maxEmbedReply bounds what the embeddings check reads: one 4,096-number
// vector as JSON is well under this
const maxEmbedReply = 1 << 20

// checkEmbeddings (--net, JGA-294) asks each model in the embedding role to
// embed one short text and compares the vector's length with the model's
// dims in the registry. A length that differs means the server runs other
// weights than the registry names, and every stored vector was made by a
// different model, so memory needs a re-embed before vector search can be
// trusted (Suite D52). Down or wrong only warns: the home server may be
// restarting, and neither is a new release's fault, so neither may roll
// back a bud update. As with model endpoints, only an {env:VAR} key is
// sent; an op:// key is never read (Suite D7)
func checkEmbeddings(ctx context.Context, reg *registry.Registry) []Result {
	r := Result{Name: "embeddings"}
	role, ok := reg.Roles["embedding"]
	if !ok {
		return nil
	}
	var problems, answered []string
	for _, id := range role.Members() {
		m, ok := reg.Models[id]
		if !ok {
			continue
		}
		dims, err := embedDims(ctx, reg.Providers[m.Provider], m.Name)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: %v", id, err))
		case dims != m.Dims:
			problems = append(problems, fmt.Sprintf("%s returned %d dims but the registry says %d; the server runs other weights, so fix the unit or the registry, then re-embed memory", id, dims, m.Dims))
		default:
			answered = append(answered, fmt.Sprintf("%s (%d dims)", id, dims))
		}
	}
	if len(problems) == 0 {
		r.Detail = strings.Join(answered, ", ") + " answered"
		return []Result{r}
	}
	r.Status = Warn
	r.Detail = strings.Join(problems, "; ")
	r.Fix = "systemctl --user status bud-embed on the home server (server/embed/README.md)"

	return []Result{r}
}

// embedDims POSTs one input to the provider's /embeddings and returns the
// length of the vector that comes back
func embedDims(ctx context.Context, prov registry.Provider, model string) (int, error) {
	base, err := url.Parse(prov.Endpoint)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(map[string]string{"model": model, "input": "bud doctor"})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.JoinPath("embeddings").String(), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if v, ok := strings.CutPrefix(prov.Key, "{env:"); ok {
		if key := os.Getenv(strings.TrimSuffix(v, "}")); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := netClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s didn't answer", base.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s answered %s to an embedding request", base.Host, resp.Status)
	}
	var reply struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxEmbedReply)).Decode(&reply); err != nil {
		return 0, fmt.Errorf("%s sent an embedding reply bud can't read: %v", base.Host, err)
	}
	if len(reply.Data) == 0 || len(reply.Data[0].Embedding) == 0 {
		return 0, fmt.Errorf("%s sent no embedding", base.Host)
	}

	return len(reply.Data[0].Embedding), nil
}
