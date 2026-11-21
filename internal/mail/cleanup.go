package mail

import (
	"context"
	"errors"
	"fmt"
	netmail "net/mail"
	"slices"
	"strings"
	"time"
)

// Cleanup and sends (Suite D31, D36, D107). Cleanup only moves mail to the
// trash and back, logging each move by Message-ID so a restore can find it
// and the daily digest can list it; nothing here deletes mail. Sends are
// logged by recipients and subject, never body

// MaxMove caps one move_to_trash call
const MaxMove = 50

// Moved is one message moved to the trash
type Moved struct {
	UID       uint32   `json:"uid"`
	MessageID string   `json:"message_id,omitempty"`
	From      []string `json:"from,omitempty"`
	Subject   string   `json:"subject,omitempty"`
}

// ErrNoLog says the account has no action log, which moves and sends need
var ErrNoLog = errors.New("the mail action log isn't set up, so bud won't move or send mail")

// trash finds the trash folder: the one with the \Trash special-use flag,
// else the one named a.Trash
func (a *Account) trash(ctx context.Context, c *Client) (string, error) {
	list, err := c.List(ctx)
	if err != nil {
		return "", err
	}
	for _, f := range list {
		if f.HasFlag(`\Trash`) && f.Selectable() {
			return f.Name, nil
		}
	}
	for _, f := range list {
		if f.Name == a.Trash && f.Selectable() {
			return f.Name, nil
		}
	}
	return "", fmt.Errorf("there's no trash folder: none has the \\Trash flag and none is named %q (set --trash)", a.Trash)
}

// MoveToTrash moves uids in folder to the trash and logs each one. UIDs no
// longer in the folder are left out of the answer
func (a *Account) MoveToTrash(ctx context.Context, folder string, uids []uint32) ([]Moved, string, error) {
	if a.Log == nil {
		return nil, "", ErrNoLog
	}
	if len(uids) == 0 || len(uids) > MaxMove {
		return nil, "", fmt.Errorf("uids: give 1 to %d", MaxMove)
	}
	a.writes.Lock()
	defer a.writes.Unlock()
	var moved []Moved
	var trash string
	err := a.session(ctx, func(c *Client) error {
		var err error
		if trash, err = a.trash(ctx, c); err != nil {
			return err
		}
		if folder == trash {
			return fmt.Errorf("those messages are already in %s", trash)
		}
		if _, err := c.Select(ctx, folder); err != nil {
			return err
		}
		sums, err := c.FetchSummaries(ctx, uids)
		if err != nil {
			return err
		}
		if len(sums) == 0 {
			return ErrNoMessage
		}
		var found []uint32
		for _, s := range sums {
			h, _ := ParseHeader(s.Header)
			moved = append(moved, Moved{UID: s.UID, MessageID: h.MessageID, From: h.From, Subject: h.Subject})
			found = append(found, s.UID)
		}
		return c.Move(ctx, found, trash)
	})
	if err != nil {
		return nil, "", err
	}
	acts := make([]Action, len(moved))
	for i, m := range moved {
		acts[i] = Action{Action: "trash", Folder: folder, ToFolder: trash, MessageID: m.MessageID, From: m.From, Subject: m.Subject}
	}
	if err := a.Log.Append(acts...); err != nil {
		return nil, "", fmt.Errorf("moved %d messages to %s but couldn't log them, so restore_message can't find them (move them back in Mail): %w", len(moved), trash, err)
	}
	return moved, trash, nil
}

// Restore moves a message bud put in the trash back to the folder it came
// from, found by its Message-ID in the action log
func (a *Account) Restore(ctx context.Context, messageID string) (Action, error) {
	if a.Log == nil {
		return Action{}, ErrNoLog
	}
	a.writes.Lock()
	defer a.writes.Unlock()
	acts, err := a.Log.Read()
	if err != nil {
		return Action{}, err
	}
	var last *Action
	for i := range acts {
		if acts[i].MessageID == messageID && (acts[i].Action == "trash" || acts[i].Action == "restore") {
			last = &acts[i]
		}
	}
	if last == nil || last.Action != "trash" {
		return Action{}, fmt.Errorf("the action log has no move of %s to the trash since its last restore: restore_message only undoes bud's own moves", messageID)
	}
	var trash string
	err = a.session(ctx, func(c *Client) error {
		if trash, err = a.trash(ctx, c); err != nil {
			return err
		}
		if _, err := c.Select(ctx, trash); err != nil {
			return err
		}
		uids, err := c.Search(ctx, Criteria{MessageID: messageID})
		if err != nil {
			return err
		}
		if len(uids) == 0 {
			return fmt.Errorf("%s isn't in %s any more: it may have been emptied (after about 30 days) or moved by hand", messageID, trash)
		}
		return c.Move(ctx, uids, last.Folder)
	})
	if err != nil {
		return Action{}, err
	}
	r := Action{Action: "restore", Folder: trash, ToFolder: last.Folder, MessageID: messageID, From: last.From, Subject: last.Subject}
	if err := a.Log.Append(r); err != nil {
		return Action{}, fmt.Errorf("restored %s to %s but couldn't log it: %w", messageID, last.Folder, err)
	}
	return r, nil
}

// Digest returns the moves and restores since a time, oldest first
func (a *Account) Digest(since time.Time) ([]Action, error) {
	if a.Log == nil {
		return nil, ErrNoLog
	}
	a.writes.Lock()
	defer a.writes.Unlock()
	acts, err := a.Log.Read()
	if err != nil {
		return nil, err
	}
	out := []Action{}
	for _, x := range acts {
		if (x.Action == "trash" || x.Action == "restore") && !x.Time.Before(since) {
			out = append(out, x)
		}
	}
	return out, nil
}

// Send sends a new message and logs it
func (a *Account) Send(ctx context.Context, m Outgoing) (string, error) {
	a.writes.Lock()
	defer a.writes.Unlock()
	return a.send(ctx, m)
}

func (a *Account) send(ctx context.Context, m Outgoing) (string, error) {
	if a.Sender == nil {
		return "", errors.New("sending isn't set up: set BUD_MAIL_FROM and restart bud mail-mcp")
	}
	if a.Log == nil {
		return "", ErrNoLog
	}
	if err := m.Check(); err != nil {
		return "", err
	}
	id, err := a.Sender.Send(ctx, m)
	if err != nil {
		return "", err
	}
	var rcpt []string
	for _, r := range append(append([]*netmail.Address{}, m.To...), m.Cc...) {
		rcpt = append(rcpt, r.Address)
	}
	if err := a.Log.Append(Action{Action: "send", MessageID: id, Recipients: rcpt, Subject: m.Subject}); err != nil {
		return id, fmt.Errorf("sent %s but couldn't log it: %w", id, err)
	}
	return id, nil
}

// Reply sends a reply to the message at folder and uid. Every recipient
// must be on the original (its Reply-To or From, To and Cc, less this
// account's own addresses), and the one it came from (Reply-To, else From)
// must be in to, so the approval shows who it really goes to (D107). The
// subject must be the one the reply gets, "Re: " and the original's, for
// the same reason
func (a *Account) Reply(ctx context.Context, folder string, uid uint32, to, cc []*netmail.Address, subject, body string) (string, error) {
	a.writes.Lock()
	defer a.writes.Unlock()
	var orig Header
	err := a.session(ctx, func(c *Client) error {
		if _, err := c.Examine(ctx, folder); err != nil {
			return err
		}
		sums, err := c.FetchSummaries(ctx, []uint32{uid})
		if err != nil {
			return err
		}
		if len(sums) == 0 {
			return ErrNoMessage
		}
		orig, err = ParseHeader(sums[0].Header)
		return err
	})
	if err != nil {
		return "", err
	}
	self := []string{strings.ToLower(a.user)}
	if a.Sender != nil {
		self = append(self, strings.ToLower(a.Sender.From()))
	}
	primary := orig.ReplyTo
	if len(primary) == 0 {
		primary = orig.From
	}
	var allowed []string
	for _, s := range slices.Concat(primary, orig.To, orig.Cc) {
		if ad := addrOf(s); ad != "" && !slices.Contains(self, ad) {
			allowed = append(allowed, ad)
		}
	}
	for _, r := range slices.Concat(to, cc) {
		if !slices.Contains(allowed, strings.ToLower(r.Address)) {
			return "", fmt.Errorf("%s isn't on the message being answered, so reply_message won't send to them: the people on it are %s (use send_message for anyone else)", r.Address, strings.Join(allowed, ", "))
		}
	}
	hasPrimary := false
	for _, p := range primary {
		hasPrimary = hasPrimary || slices.ContainsFunc(to, func(r *netmail.Address) bool { return strings.EqualFold(r.Address, addrOf(p)) })
	}
	if !hasPrimary {
		return "", fmt.Errorf("to: a reply goes to %s, who sent the message", strings.Join(primary, ", "))
	}
	want := ReplySubject(orig.Subject)
	if subject != want {
		return "", fmt.Errorf("subject: a reply to this message has the subject %q; give exactly that", want)
	}
	refs := strings.TrimSpace(orig.References + " " + orig.MessageID)
	return a.send(ctx, Outgoing{To: to, Cc: cc, Subject: want, Body: body, InReplyTo: orig.MessageID, References: refs})
}

// ReplySubject is the subject a reply gets: the original's on one line,
// with "Re: " in front unless it has one
func ReplySubject(orig string) string {
	s := strings.Join(strings.Fields(orig), " ")
	if !strings.HasPrefix(strings.ToLower(s), "re:") {
		s = "Re: " + s
	}
	return s
}

// addrOf is the lower-cased address in "Name <address>" or a bare address
func addrOf(s string) string {
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			s = s[i+1 : i+j]
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}
