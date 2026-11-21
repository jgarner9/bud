package approval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

// maxReviewLine bounds the reply line so a misbehaving peer can't grow memory
const maxReviewLine = 64 << 10

// maxSummaryArgs bounds how much of the arguments the prompt shows
const maxSummaryArgs = 1024

type reviewRequest struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	ExpiresAt string `json:"expires_at"`
	Secret    string `json:"secret"`
}

type reviewReply struct {
	ID     string `json:"id"`
	Choice string `json:"choice"`
	// Error is raw so that any error key, even "" or null, refuses
	Error json.RawMessage `json:"error,omitempty"`
}

// SocketReview returns a Review that asks the reviewer listening on the unix
// socket: one JSON line {id, summary, expires_at, secret} out, one line
// {id, choice} back, the connection held open meanwhile. Closing it (ctx end)
// withdraws the prompt. scope is the one the trusted adapter assigned; it is
// copied into Reply.Scope and never read from the reply. The secret file is read
// on every call, so a rotated secret needs no restart; it must be mode 0600 or
// stricter. Every failure returns an error, which Dispatch treats as a denial.
func SocketReview(socket, secretFile string, scope Scope) Review {
	return func(ctx context.Context, p Prompt) (Reply, error) {
		if socket == "" || secretFile == "" {
			return Reply{}, ErrReviewUnavailable
		}
		secret, err := readSecret(secretFile)
		if err != nil {
			return Reply{}, err
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", socket)
		if err != nil {
			return Reply{}, fmt.Errorf("dial approval reviewer: %w", ErrReviewUnavailable)
		}
		defer conn.Close()
		// Closing on ctx end unblocks the read and tells the reviewer to withdraw.
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		defer stop()

		line, err := json.Marshal(reviewRequest{
			ID:        p.ID,
			Summary:   summarize(p),
			ExpiresAt: p.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
			Secret:    secret,
		})
		if err != nil {
			return Reply{}, fmt.Errorf("encode approval request: %w", err)
		}
		if _, err := conn.Write(append(line, '\n')); err != nil {
			return Reply{}, ctxOr(ctx, fmt.Errorf("send approval request: %w", err))
		}
		raw, err := readLine(bufio.NewReader(conn))
		if err != nil {
			return Reply{}, ctxOr(ctx, fmt.Errorf("read approval reply: %w", err))
		}
		var r reviewReply
		if err := json.Unmarshal(raw, &r); err != nil {
			return Reply{}, fmt.Errorf("decode approval reply: %w", ErrReviewMismatch)
		}
		if r.Error != nil {
			return Reply{}, fmt.Errorf("approval reviewer refused: %w", ErrReviewUnavailable)
		}
		if r.ID != p.ID {
			return Reply{}, ErrReviewMismatch
		}
		switch c := Choice(r.Choice); c {
		case AllowOnce, DenyOnce:
			return Reply{ID: r.ID, Scope: scope, Choice: c}, nil
		}
		return Reply{}, ErrReviewMismatch
	}
}

func ctxOr(ctx context.Context, err error) error {
	if e := ctx.Err(); e != nil {
		return e
	}

	return err
}

// readLine reads one newline-terminated line of at most maxReviewLine bytes
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > maxReviewLine {
			return nil, errors.New("reply line too long")
		}
		if err == nil {
			return buf, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}

		return nil, err
	}
}

// maxSecret bounds the secret file read
const maxSecret = 4096

// CheckSecret reports whether the secret file at path passes the checks a
// review makes before sending it, without returning the secret
func CheckSecret(path string) error {
	_, err := readSecret(path)
	return err
}

func readSecret(path string) (string, error) {
	// no-follow so a symlink is refused, non-blocking so a FIFO can't hang
	// the review, and the checks run on the opened file, not the path
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("read approval secret %s (it must be a regular file, not a symlink): %w", path, ErrReviewUnavailable)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read approval secret: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("approval secret %s must be a regular file with mode 0600: %w", path, ErrReviewUnavailable)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSecret+1))
	if err != nil {
		return "", fmt.Errorf("read approval secret: %w", err)
	}
	if len(b) > maxSecret {
		return "", fmt.Errorf("approval secret %s is over %d bytes: %w", path, maxSecret, ErrReviewUnavailable)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("approval secret is empty: %w", ErrReviewUnavailable)
	}

	return s, nil
}

// titles say what a known action asks, in words for a phone, and which of
// its arguments are only there to bind the approval (hashes, paths) and so
// aren't worth showing
var titles = map[string]struct {
	title func(target string) string
	hide  []string
	// whole are arguments shown in full, never cut, in this order before
	// the rest: a mail send shows the whole message (Suite D107). A
	// multi-line value is shown as lines each opening with a bar, so no
	// line of it can pass for a field or for bud's own words
	whole []string
}{
	"mcp:mail/send_message": {
		title: func(string) string { return "Send this email from Jason's iCloud?" },
		whole: []string{"to", "cc", "subject", "body"},
	},
	"mcp:mail/reply_message": {
		title: func(string) string { return "Send this reply from Jason's iCloud?" },
		whole: []string{"to", "cc", "subject", "body"},
	},
	"router:p0-fallback": {
		title: func(t string) string {
			return "The local model is down. Send this private (P0) chat to " + t + " instead?"
		},
		hide: []string{"endpoint"},
	},
}

// maxSummaryValue bounds one argument's shown value
const maxSummaryValue = 160

// summarize is the prompt's text: what is asked, the arguments worth reading
// as "key: value" lines (sorted, hashes left out, long values cut) and when
// it expires, never raw JSON. Arguments that aren't a JSON object are shown
// as they are, cut to maxSummaryArgs
func summarize(p Prompt) string {
	inv := p.Invocation
	var b strings.Builder
	t, known := titles[inv.Action]
	if known {
		b.WriteString(t.title(inv.Target))
	} else {
		fmt.Fprintf(&b, "%s on %s", inv.Action, inv.Target)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(inv.Arguments, &fields) == nil {
		for _, k := range t.whole {
			if v, ok := fields[k]; ok {
				b.WriteString(wholeLine(k, v))
			}
		}
		keys := make([]string, 0, len(fields))
		for k := range fields {
			if strings.Contains(k, "sha256") || strings.Contains(k, "hash") || slices.Contains(t.hide, k) || slices.Contains(t.whole, k) {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		shown := 0
		for _, k := range keys {
			v := plain(fields[k])
			if len(v) > maxSummaryValue {
				v = v[:maxSummaryValue] + "..."
			}
			line := "\n" + k + ": " + v
			if shown+len(line) > maxSummaryArgs {
				b.WriteString("\n...")
				break
			}
			b.WriteString(line)
			shown += len(line)
		}
	} else if len(inv.Arguments) > 0 {
		args, more := inv.Arguments, ""
		if len(args) > maxSummaryArgs {
			args, more = args[:maxSummaryArgs], "..."
		}
		fmt.Fprintf(&b, "\nargs: %s%s", args, more)
	}
	if !p.ExpiresAt.IsZero() {
		fmt.Fprintf(&b, "\nExpires at %s.", p.ExpiresAt.Local().Format("15:04"))
	}

	return b.String()
}

// wholeLine shows one argument in full: a list of strings as a comma-
// separated line, a one-line string as itself, and a string with line
// breaks as lines under the key that each open with a bar
func wholeLine(k string, v json.RawMessage) string {
	var list []string
	if json.Unmarshal(v, &list) == nil {
		for i, s := range list {
			list[i] = plain(mustJSON(s))
		}
		return "\n" + k + ": " + strings.Join(list, ", ")
	}
	var s string
	if json.Unmarshal(v, &s) != nil || !strings.ContainsAny(s, "\r\n\u2028\u2029") {
		return "\n" + k + ": " + plain(v)
	}
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n").Replace(s)
	var b strings.Builder
	b.WriteString("\n" + k + ":")
	for _, line := range strings.Split(s, "\n") {
		b.WriteString("\n\u2502 " + plain(mustJSON(line)))
	}
	return b.String()
}

func mustJSON(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// plain is a JSON value as a phone should show it: a string without its
// quotes and escapes and on one line, anything else compact
func plain(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		// one value is one line: a newline in an argument mustn't pass
		// for another field, or for words of bud's own, in the prompt
		return strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
				return ' '
			}
			return r
		}, s)
	}
	var buf bytes.Buffer
	if json.Compact(&buf, v) == nil {
		return buf.String()
	}
	return string(v)
}
