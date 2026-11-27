package mail

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// What ingestion reads (Suite D106, JGA-317). The fetch is plain code: it
// finds the messages a folder has gained since a mark and parses them. It
// calls no model and can act on nothing; what happens to the text is the
// ingest command's business (internal/mailingest)

// Mark is how far ingestion has read one folder. Uids mean something only
// under the UIDVALIDITY they were issued with
type Mark struct {
	UIDValidity uint32 `json:"uidvalidity"`
	UID         uint32 `json:"uid"` // the highest uid handled
}

// Fetched is one new message: its uid, when the server received it, and
// the parsed message with its text cut to what was asked for
type Fetched struct {
	UID      uint32
	Received time.Time
	Message  Message
	// Err says the message couldn't be read; it is still handled, so one
	// bad message can't block the ones behind it
	Err string
}

// Fresh is what a folder has gained since a mark
type Fresh struct {
	UIDValidity uint32
	// Newest is the folder's highest uid now, 0 when it is empty
	Newest uint32
	// First is true when the folder had no mark, Changed when its
	// UIDVALIDITY isn't the mark's. Either way the mark moves to Newest
	// and, unless Backfill asked for some, no message is returned
	First, Changed bool
	// Through is the mark's new uid once the Messages are handled: Newest
	// for a first run or a changed UIDVALIDITY, else the last uid looked at
	// (one that vanished mid-run counts as handled)
	Through  uint32
	Messages []Fetched
	// More says there were more new messages than the limit; the rest
	// wait for the next run
	More bool
}

// FreshOptions bounds one fetch
type FreshOptions struct {
	// Limit is the most messages returned, oldest first
	Limit int
	// MaxText cuts each message's text to this many bytes (0 keeps MaxText)
	MaxText int
	// Backfill is how many of the newest messages to return on a folder's
	// first run, 0 for none
	Backfill int
}

// Fresh returns the messages in folder newer than mark, oldest first. It
// opens the folder read-only and fetches with BODY.PEEK, so nothing is
// marked read. A folder with no mark (first run) or a changed UIDVALIDITY
// returns no old mail: the caller moves the mark to Newest
func (a *Account) Fresh(ctx context.Context, folder string, mark Mark, o FreshOptions) (Fresh, error) {
	if o.Limit <= 0 {
		return Fresh{}, fmt.Errorf("limit: give at least 1")
	}
	maxText := o.MaxText
	if maxText <= 0 || maxText > MaxText {
		maxText = MaxText
	}
	var f Fresh
	err := a.session(ctx, func(c *Client) error {
		mb, err := c.Examine(ctx, folder)
		if err != nil {
			return err
		}
		f.UIDValidity = mb.UIDValidity
		if mb.UIDNext > 0 {
			f.Newest = mb.UIDNext - 1
		}
		var uids []uint32
		switch {
		case mark.UIDValidity == 0:
			f.First = true
			if o.Backfill > 0 {
				all, err := c.Search(ctx, Criteria{})
				if err != nil {
					return err
				}
				uids = all[max(0, len(all)-o.Backfill):]
			}
		case mark.UIDValidity != mb.UIDValidity:
			f.Changed = true
		default:
			found, err := c.Search(ctx, Criteria{UIDAfter: mark.UID})
			if err != nil {
				return err
			}
			// N:* always answers the highest uid, even when it is at or
			// below N
			uids = slices.DeleteFunc(found, func(u uint32) bool { return u <= mark.UID })
		}
		if len(uids) > o.Limit {
			uids, f.More = uids[:o.Limit], true
		}
		f.Through = mark.UID
		if f.First || f.Changed {
			f.Through = f.Newest
		} else if len(uids) > 0 {
			f.Through = uids[len(uids)-1]
		}
		if len(uids) == 0 {
			return nil
		}
		sums, err := c.FetchSummaries(ctx, uids)
		if err != nil {
			return err
		}
		received := map[uint32]time.Time{}
		for _, s := range sums {
			received[s.UID] = s.Received
		}
		for _, uid := range uids {
			item := Fetched{UID: uid, Received: received[uid]}
			b, err := c.FetchBody(ctx, uid, maxFetch)
			switch {
			case err == ErrNoMessage:
				// gone since the search: nothing to read, nothing to keep
				continue
			case err != nil:
				return err
			}
			m, err := ParseMessage(b.Raw, b.Truncated)
			if err != nil {
				item.Err = "the message couldn't be read: " + err.Error()
			} else {
				m.Text = cutText(m.Text, maxText)
				item.Message = m
			}
			f.Messages = append(f.Messages, item)
		}
		return nil
	})
	return f, err
}

// cutText keeps at most n bytes of s, on a character boundary
func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
