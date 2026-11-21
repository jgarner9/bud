package locate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Output is what a finished tool left behind.
type Output struct {
	Stdout    []byte
	Stderr    string // the first few KB
	Exit      int
	Truncated bool // stdout passed MaxOutput and the tool was stopped
}

// Runner runs a tool without a shell. It returns an error only when the tool
// could not be run at all; a non-zero Exit is not an error.
type Runner func(name string, args ...string) (Output, error)

// Limits on one tool run, so a huge tree cannot exhaust memory or hang.
const (
	MaxOutput  = 32 << 20
	RunTimeout = 2 * time.Minute
	maxStderr  = 4096
)

// ExecRunner is the real Runner.
func ExecRunner(name string, args ...string) (Output, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := &capBuf{max: MaxOutput + 1, full: cancel}
	stderr := &capBuf{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A tool's descendant can inherit its pipes. Kill the group on timeout
	// or output overflow, and bound the wait even if a pipe is kept open.
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	waitErr := cmd.Run()

	res := Output{Stdout: stdout.buf.Bytes(), Stderr: stderr.buf.String()}
	if len(res.Stdout) > MaxOutput {
		res.Stdout, res.Truncated = res.Stdout[:MaxOutput], true
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return res, errors.New(name + " timed out after " + RunTimeout.String())
	}
	var ee *exec.ExitError
	switch {
	case res.Truncated:
	case errors.As(waitErr, &ee):
		res.Exit = ee.ExitCode()
	case waitErr != nil:
		return res, waitErr
	}
	return res, nil
}

// capBuf keeps the first max bytes written and drops the rest.
type capBuf struct {
	buf  bytes.Buffer
	max  int
	full func()
}

func (c *capBuf) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
		if c.buf.Len() == c.max && c.full != nil {
			c.full()
		}
	}
	return len(p), nil
}

// FindAstGrep returns the path of ast-grep, or "". The binary is called
// ast-grep, or sg, but sg is also a system command (shadow-utils' group
// switcher), so a found name only counts if --version says ast-grep.
func FindAstGrep(look func(string) (string, error), run Runner) string {
	if run == nil {
		run = ExecRunner
	}
	for _, name := range []string{"ast-grep", "sg"} {
		p, err := look(name)
		if err != nil {
			continue
		}
		if out, err := run(p, "--version"); err == nil && out.Exit == 0 &&
			strings.Contains(strings.ToLower(string(out.Stdout)), "ast-grep") {
			return p
		}
	}
	return ""
}
