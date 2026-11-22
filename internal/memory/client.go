// Package memory is a client of Bud's working-memory service
// (server/memory/): HTTP and JSON over its Unix socket, with a bearer token
// whose tiers and actions the service holds (Suite D51, D102). It is what
// bud memory-mcp uses to search, stage and embed records; it holds no
// policy of its own, since the service decides what each token may see
package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// maxReply bounds what the client reads from one reply: 100 records at the
// service's 64 KiB request limit each fit well inside it
const maxReply = 16 << 20

// ErrUnavailable means the service couldn't be reached: its socket is
// missing or nothing answers on it
var ErrUnavailable = errors.New("the memory service isn't running")

// StatusError is a reply other than 200. The service says only "invalid
// request", "forbidden" and the like, so the status carries the meaning
type StatusError struct {
	Action string
	Status int
	Reason string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("memory %s: %d %s", e.Action, e.Status, e.Reason)
}

// Client talks to one memory service as one identity
type Client struct {
	token string
	http  *http.Client
}

// New returns a client for the service at socket, sending token
func New(socket, token string) *Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &Client{
		token: token,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", socket)
				},
				// the service closes every connection after one reply
				DisableKeepAlives: true,
			},
		},
	}
}

// Record is a search result with its provenance
type Record struct {
	ID        int64    `json:"id"`
	Source    string   `json:"source"`
	Kind      string   `json:"kind"`
	Privacy   string   `json:"privacy"`
	Body      string   `json:"body"`
	CreatedAt string   `json:"created_at"`
	Matches   []string `json:"matches,omitempty"`
}

// Query is a hybrid search. Values is the query's embedding by Model;
// Source, Since and Until (inclusive YYYY-MM-DD) are optional filters
type Query struct {
	Query  string    `json:"query"`
	Model  string    `json:"model,omitempty"`
	Values []float64 `json:"values,omitempty"`
	Source string    `json:"source,omitempty"`
	Since  string    `json:"since,omitempty"`
	Until  string    `json:"until,omitempty"`
	Limit  int       `json:"limit,omitempty"`
}

// Retrieve searches approved records by text and vector together, within
// the token's tiers. A Query with no Model and Values ranks by text alone,
// with the same filters
func (c *Client) Retrieve(ctx context.Context, q Query) ([]Record, error) {
	var reply struct {
		Records []Record `json:"records"`
	}
	err := c.post(ctx, "retrieve", q, &reply)
	return reply.Records, err
}

// Search searches approved records by text alone, for when no query
// embedding can be made; a limit of 0 takes the service's default
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Record, error) {
	req := map[string]any{"query": query}
	if limit > 0 {
		req["limit"] = limit
	}
	var reply struct {
		Records []Record `json:"records"`
	}
	if err := c.post(ctx, "search", req, &reply); err != nil {
		return nil, err
	}
	for i := range reply.Records {
		reply.Records[i].Matches = []string{"text"}
	}
	return reply.Records, nil
}

// Draft is a record to stage for review. The same Source and DedupeKey
// with the same content returns the first record's id, so a retry never
// stages twice
type Draft struct {
	Source    string `json:"source"`
	DedupeKey string `json:"dedupe_key"`
	Kind      string `json:"kind"`
	Privacy   string `json:"privacy"`
	Body      string `json:"body"`
}

// Stage stages a record; nothing is searchable until the reviewer approves it
func (c *Client) Stage(ctx context.Context, d Draft) (int64, error) {
	var reply struct {
		ID int64 `json:"id"`
	}
	err := c.post(ctx, "stage", d, &reply)
	return reply.ID, err
}

// Unembedded is an approved record with no current vector for a model.
// BodyHash goes back to PutVector, so an edit in between is refused
type Unembedded struct {
	ID       int64  `json:"id"`
	Privacy  string `json:"privacy"`
	Body     string `json:"body"`
	BodyHash string `json:"body_hash"`
}

// ErrStale means the record changed after it was read for embedding; it
// stays pending, so the next round embeds the new body
var ErrStale = errors.New("the record changed since it was read")

// Pending lists up to limit approved records with ids above after that
// model still has to embed, in id order; a limit of 0 takes the service's
// default of 20
func (c *Client) Pending(ctx context.Context, model string, after int64, limit int) ([]Unembedded, error) {
	req := map[string]any{"model": model}
	if after > 0 {
		req["after"] = after
	}
	if limit > 0 {
		req["limit"] = limit
	}
	var reply struct {
		Records []Unembedded `json:"records"`
	}
	err := c.post(ctx, "vector_pending", req, &reply)
	return reply.Records, err
}

// PutVector stores the embedding by model of the record body whose hash is
// bodyHash, or returns ErrStale when the record has changed since
func (c *Client) PutVector(ctx context.Context, id int64, model string, values []float64, bodyHash string) error {
	err := c.post(ctx, "vector_put", map[string]any{"id": id, "model": model, "values": values, "body_hash": bodyHash}, nil)
	if se, ok := errors.AsType[*StatusError](err); ok && se.Status == http.StatusConflict {
		return fmt.Errorf("memory vector_put: record %d: %w", id, ErrStale)
	}
	return err
}

// ErrApproved means a record Discard was given is already approved, so it
// stays
var ErrApproved = errors.New("the record is already approved")

// Staged lists up to limit records waiting on review with ids above after,
// in id order, within the token's tiers; a limit of 0 takes the service's
// default of 20. Only a reviewer token may list them
func (c *Client) Staged(ctx context.Context, after int64, limit int) ([]Record, error) {
	req := map[string]any{}
	if after > 0 {
		req["after"] = after
	}
	if limit > 0 {
		req["limit"] = limit
	}
	var reply struct {
		Records []Record `json:"records"`
	}
	err := c.post(ctx, "staged", req, &reply)
	return reply.Records, err
}

// Approve makes a staged record searchable
func (c *Client) Approve(ctx context.Context, id int64) error {
	return c.post(ctx, "approve", map[string]any{"id": id}, nil)
}

// Discard deletes a staged record Jason turned down, or returns
// ErrApproved when it was approved meanwhile
func (c *Client) Discard(ctx context.Context, id int64) error {
	err := c.post(ctx, "discard", map[string]any{"id": id}, nil)
	if se, ok := errors.AsType[*StatusError](err); ok && se.Status == http.StatusConflict {
		return fmt.Errorf("memory discard: record %d: %w", id, ErrApproved)
	}
	return err
}

// ExportedRecord is an approved record as the export action lists it
type ExportedRecord struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	Privacy   string `json:"privacy"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	State     string `json:"state"`
}

// Reference is one time something used an approved record
type Reference struct {
	RecordID     int64  `json:"record_id"`
	Source       string `json:"source"`
	ReferencedAt string `json:"referenced_at"`
}

// Export is every approved record within the token's tiers, with the
// references to them
type Export struct {
	Records    []ExportedRecord `json:"records"`
	References []Reference      `json:"references"`
}

// Export reads every approved record the token may see, and the references
// to them, in id order. The curator reads working memory this way
func (c *Client) Export(ctx context.Context) (*Export, error) {
	var reply Export
	if err := c.post(ctx, "export", map[string]any{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

func (c *Client) post(ctx context.Context, action string, body, reply any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("memory %s: %w", action, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://memory/"+action, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("memory %s: %w", action, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("memory %s: %w (%v)", action, ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return fmt.Errorf("memory %s: read the reply: %w", action, err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return &StatusError{Action: action, Status: resp.StatusCode, Reason: e.Error}
	}
	if reply == nil {
		return nil
	}
	if err := json.Unmarshal(data, reply); err != nil {
		return fmt.Errorf("memory %s: a reply bud can't read: %w", action, err)
	}

	return nil
}
