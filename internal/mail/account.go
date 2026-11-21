package mail

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
)

// Account is one mailbox reached over IMAP. Each call opens its own
// connection, logs in, does its work and logs out, so a dropped connection
// leaves nothing behind. Calls run one at a time
type Account struct {
	addr string
	tls  *tls.Config // nil for a plain connection to loopback (tests)
	user string
	pass string
	mu   sync.Mutex // one IMAP session at a time
	// writes holds a move, restore or send together with its log entry,
	// so a restore or the digest never sees a move that isn't logged yet
	writes sync.Mutex

	// Trash is the folder cleanup moves to when no folder has the \Trash
	// special-use flag
	Trash string
	// Log records moves, restores and sends; moves and sends need it
	Log *Log
	// Sender sends; without it the send tools say sending isn't set up
	Sender *Sender
}

// NewAccount checks the IMAP address and credentials. A loopback host is
// dialed in the clear, for tests; anything else needs TLS
func NewAccount(addr, user, pass string) (*Account, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("--imap %q isn't host:port, such as imap.mail.me.com:993", addr)
	}
	if user == "" || pass == "" {
		return nil, errors.New("no mail credentials: set BUD_MAIL_USER and BUD_MAIL_PASSWORD (op run fills them from the Bud vault)")
	}
	a := &Account{addr: addr, user: user, pass: pass, Trash: "Deleted Messages"}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		a.tls = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	return a, nil
}

func (a *Account) session(ctx context.Context, fn func(*Client) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := Dial(ctx, a.addr, a.tls)
	if err != nil {
		return err
	}
	if err := c.Login(ctx, a.user, a.pass); err != nil {
		_ = c.Close()
		return err
	}
	if err := fn(c); err != nil {
		_ = c.Close()
		return err
	}
	// the work is done once fn returns: a LOGOUT that fails (a dropped
	// connection, say) mustn't turn a move that happened into a failure,
	// which would skip its log entry
	_ = c.Logout(ctx)
	return nil
}

// FolderInfo is a folder with its counts
type FolderInfo struct {
	Name    string `json:"name"`
	Special string `json:"special,omitempty"` // trash, sent, junk, drafts, archive
	Total   uint32 `json:"messages"`
	Unseen  uint32 `json:"unseen"`
}

var specialUse = []string{`\Trash`, `\Sent`, `\Junk`, `\Drafts`, `\Archive`}

// Folders lists the folders that can hold mail, with their counts
func (a *Account) Folders(ctx context.Context) ([]FolderInfo, error) {
	var out []FolderInfo
	err := a.session(ctx, func(c *Client) error {
		list, err := c.List(ctx)
		if err != nil {
			return err
		}
		for _, f := range list {
			if !f.Selectable() {
				continue
			}
			st, err := c.Status(ctx, f.Name)
			if err != nil {
				return err
			}
			fi := FolderInfo{Name: f.Name, Total: st.Messages, Unseen: st.Unseen}
			if i := slices.IndexFunc(specialUse, f.HasFlag); i >= 0 {
				fi.Special = strings.ToLower(specialUse[i][1:])
			}
			out = append(out, fi)
		}
		return nil
	})
	return out, err
}

// Search returns the summaries of the newest limit matches in folder,
// newest first, and how many matched in all
func (a *Account) Search(ctx context.Context, folder string, cr Criteria, limit int) ([]Summary, int, error) {
	var out []Summary
	total := 0
	err := a.session(ctx, func(c *Client) error {
		if _, err := c.Examine(ctx, folder); err != nil {
			return err
		}
		uids, err := c.Search(ctx, cr)
		if err != nil {
			return err
		}
		total = len(uids)
		if len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}
		sums, err := c.FetchSummaries(ctx, uids)
		if err != nil {
			return err
		}
		// UIDs grow as mail arrives, so the highest is the newest
		slices.SortFunc(sums, func(x, y Summary) int { return cmp.Compare(y.UID, x.UID) })
		out = sums
		return nil
	})
	return out, total, err
}

// Read fetches up to max bytes of one message without marking it read
func (a *Account) Read(ctx context.Context, folder string, uid uint32, max int) (Body, error) {
	var b Body
	err := a.session(ctx, func(c *Client) error {
		if _, err := c.Examine(ctx, folder); err != nil {
			return err
		}
		var err error
		b, err = c.FetchBody(ctx, uid, max)
		return err
	})
	return b, err
}
