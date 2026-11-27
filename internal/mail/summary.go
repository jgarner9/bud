package mail

import (
	"fmt"
	"time"
)

// StoredSummary is one line of bud mail-ingest's store, which list_summaries
// reads (Suite D106, JGA-317). Everything but the provenance (Folder,
// UIDValidity, UID, MessageID, Received, At, Model) is the sender's or a
// local model's text and is untrusted
type StoredSummary struct {
	Folder      string    `json:"folder"`
	UIDValidity uint32    `json:"uidvalidity"`
	UID         uint32    `json:"uid"`
	MessageID   string    `json:"message_id,omitempty"`
	Received    time.Time `json:"received"`
	From        []string  `json:"from,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	Summary     string    `json:"summary"`
	// Error says why Summary is empty; the message still counts as handled
	Error string    `json:"error,omitempty"`
	Model string    `json:"model,omitempty"`
	At    time.Time `json:"at"`
}

// Key names the message a line is about: one line per key, ever
func (s StoredSummary) Key() string { return SummaryKey(s.Folder, s.UIDValidity, s.UID) }

// SummaryKey is a message's identity in the store: uids mean something only
// under the UIDVALIDITY they were issued with
func SummaryKey(folder string, validity, uid uint32) string {
	return fmt.Sprintf("%s\x00%d\x00%d", folder, validity, uid)
}

// SummarySource is what list_summaries reads: the ingest's store
type SummarySource interface {
	// Newest returns up to limit summaries received at or after since,
	// newest first, and how many matched
	Newest(since time.Time, limit int) ([]StoredSummary, int, error)
}
