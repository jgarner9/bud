package memorymcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jgarner.dev/bud/internal/memory"
)

// Backfill embeds the approved records the model has no current vector for,
// so they become searchable by meaning (Suite D52). It runs once at start
// and then every interval. A new model starts with every record pending, so
// this is also how a model change re-embeds memory
type Backfill struct {
	Vectors  Vectors
	Embed    Embedder
	Model    string
	Batch    int           // records per embedding request; 16 when 0
	Interval time.Duration // between rounds; a minute when 0
	// Logf reports each round that did something or failed
	Logf func(format string, args ...any)
}

// Run embeds until ctx ends. A failure waits for the next round, longer
// each time up to ten intervals, so a down embedding server isn't hammered
func (b *Backfill) Run(ctx context.Context) {
	interval := b.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	wait := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		n, err := b.Round(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			b.logf("memory-mcp: embedding paused after %d records: %v", n, err)
			wait = min(max(2*wait, interval), 10*interval)
		default:
			if n > 0 {
				b.logf("memory-mcp: embedded %d records with %s", n, b.Model)
			}
			wait = interval
		}
	}
}

// Round embeds batches until none are pending, and returns how many
// records it stored
func (b *Backfill) Round(ctx context.Context) (int, error) {
	size := b.Batch
	if size <= 0 {
		size = 16
	}
	stored := 0
	// the round walks forward by id, so a record that changes under it is
	// left for the next round instead of being asked for again
	var after int64
	for {
		todo, err := b.Vectors.Pending(ctx, b.Model, after, size)
		if err != nil {
			return stored, err
		}
		if len(todo) == 0 {
			return stored, nil
		}
		after = todo[len(todo)-1].ID
		texts := make([]string, len(todo))
		for i, p := range todo {
			texts[i] = p.Body
		}
		vecs, err := b.Embed.Embed(ctx, texts)
		if err != nil {
			return stored, err
		}
		for i, p := range todo {
			err := b.Vectors.PutVector(ctx, p.ID, b.Model, vecs[i], p.BodyHash)
			switch {
			case errors.Is(err, memory.ErrStale):
				// changed since it was read; the next round has its new body
			case err != nil:
				return stored, fmt.Errorf("store record %d's vector: %w", p.ID, err)
			default:
				stored++
			}
		}
	}
}

func (b *Backfill) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}
