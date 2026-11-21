package approval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
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
	Error  string `json:"error"`
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
		if r.Error != "" {
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

func readSecret(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read approval secret: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("approval secret %s must be a regular file with mode 0600: %w", path, ErrReviewUnavailable)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read approval secret: %w", err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("approval secret is empty: %w", ErrReviewUnavailable)
	}

	return s, nil
}

func summarize(p Prompt) string {
	args := p.Invocation.Arguments
	more := ""
	if len(args) > maxSummaryArgs {
		args, more = args[:maxSummaryArgs], "..."
	}

	return fmt.Sprintf("%s on %s\nargs: %s%s", p.Invocation.Action, p.Invocation.Target, args, more)
}
