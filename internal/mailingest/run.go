package mailingest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/audit"
	"jgarner.dev/bud/internal/mail"
)

// Fetcher is what a run reads mail with: *mail.Account
type Fetcher interface {
	Fresh(ctx context.Context, folder string, mark mail.Mark, o mail.FreshOptions) (mail.Fresh, error)
}

// Options is one run's settings
type Options struct {
	Folders []string
	// Limit is the most messages summarized in one run, over all folders
	Limit int
	// MaxText cuts each message's text before it reaches the model
	MaxText int
	// Backfill is how many of a folder's newest messages to summarize the
	// first time it is seen, 0 for none
	Backfill int
}

// Run is everything one run needs
type Run struct {
	Fetch     Fetcher
	Summarize Summarizer
	Store     *Store
	StatePath string
	Audit     *audit.Log
	Now       func() time.Time
	Opts      Options
}

// Result is what a run did, as counts only
type Result struct {
	Summarized int
	Failed     int      // handled with an error line instead of a summary
	Skipped    int      // already in the store, from a crash before the mark moved
	Started    []string // folders seen for the first time
	Reset      []string // folders whose UIDVALIDITY changed
	More       bool     // mail was left for the next run
}

// ErrBusy says another run holds the lock, so this one did nothing
var ErrBusy = errors.New("another mail-ingest run is in progress")

// lock takes an exclusive lock on a file beside the state, without waiting:
// two runs sharing one state would both read the same mark, summarize the
// same messages and append them twice. flock goes when the process does
func lock(statePath string) (func(), error) {
	path := statePath + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("lock the mail ingest state: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock the mail ingest state: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("lock the mail ingest state: %w", err)
	}
	return func() { f.Close() }, nil
}

// Do runs once: for each folder, the mail newer than its mark is summarized
// oldest first and appended to the store, and the mark moves past each
// message only after its line is stored, so a crash repeats at most one
// summary and loses none. A model that can't be reached stops the run with
// ErrNoModel and the mark where it got to
func (r *Run) Do(ctx context.Context) (Result, error) {
	var res Result
	if r.Now == nil {
		r.Now = time.Now
	}
	unlock, err := lock(r.StatePath)
	if err != nil {
		return res, err
	}
	defer unlock()
	state, err := LoadState(r.StatePath)
	if err != nil {
		return res, err
	}
	have, err := r.Store.Has()
	if err != nil {
		return res, err
	}
	budget := r.Opts.Limit
	defer r.record(&res)
	for _, folder := range r.Opts.Folders {
		if budget <= 0 {
			res.More = true
			break
		}
		mark := state.Folders[folder]
		f, err := r.Fetch.Fresh(ctx, folder, mark, mail.FreshOptions{Limit: budget, MaxText: r.Opts.MaxText, Backfill: min(r.Opts.Backfill, budget)})
		if err != nil {
			return res, fmt.Errorf("fetch new mail in %s: %w", folder, err)
		}
		if f.First {
			res.Started = append(res.Started, folder)
		}
		if f.Changed {
			res.Reset = append(res.Reset, folder)
		}
		done := mark.UID
		if f.First || f.Changed {
			done = 0
		}
		for _, m := range f.Messages {
			k := mail.SummaryKey(folder, f.UIDValidity, m.UID)
			if have[k] {
				res.Skipped++
			} else if err := r.handle(ctx, folder, f.UIDValidity, m, &res); err != nil {
				// the mark stays at the last message stored
				state.Folders[folder] = mail.Mark{UIDValidity: f.UIDValidity, UID: done}
				if f.First || f.Changed {
					delete(state.Folders, folder)
				}
				if serr := state.Save(r.StatePath); serr != nil {
					return res, errors.Join(err, serr)
				}
				return res, err
			}
			have[k] = true
			done = m.UID
			budget--
		}
		// every message looked at is handled: the mark moves to the last one
		// looked at, or to the newest uid on a first run or a reset, so mail
		// that arrives while a run is under way is next time's
		state.Folders[folder] = mail.Mark{UIDValidity: f.UIDValidity, UID: f.Through}
		if f.More {
			res.More = true
		}
		if err := state.Save(r.StatePath); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (r *Run) handle(ctx context.Context, folder string, validity uint32, m mail.Fetched, res *Result) error {
	s := Summary{
		Folder: folder, UIDValidity: validity, UID: m.UID,
		MessageID: m.Message.MessageID, Received: m.Received,
		From: m.Message.From, Subject: m.Message.Subject, At: r.Now(),
	}
	if m.Err != "" {
		s.Error = m.Err
		res.Failed++
		return r.Store.Append(s)
	}
	sum, model, err := r.Summarize(ctx, Input{
		From: m.Message.From, Subject: m.Message.Subject, Received: m.Received,
		Text: m.Message.Text, Attachments: m.Message.Attachments,
	})
	switch {
	case errors.Is(err, ErrNoModel), ctx.Err() != nil:
		if err == nil {
			err = ctx.Err()
		}
		return err
	case err != nil:
		s.Error, s.Model = err.Error(), model
		res.Failed++
	default:
		s.Summary, s.Model = sum, model
		res.Summarized++
	}
	return r.Store.Append(s)
}

// record writes one audit line per run: counts only, never text
func (r *Run) record(res *Result) {
	r.Audit.Record(audit.Event{
		Time: r.Now(), Source: audit.SourceMail, Kind: audit.KindMailIngest, Tier: "P0",
		Action: "ingest", Target: "mail",
		Detail: map[string]any{
			"summarized": res.Summarized, "failed": res.Failed, "skipped": res.Skipped,
			"folders_started": len(res.Started), "folders_reset": len(res.Reset), "more": res.More,
		},
	})
}
