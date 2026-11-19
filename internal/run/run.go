// Package run runs a long command for `bud run`: the full combined output
// streams to a log file on disk, while only the failure lines and a tail are
// kept in memory. Memory use does not grow with the size of the output.
package run

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"
)

// Exit codes brain reports when the command did not exit by itself. A timeout
// is 124, as in timeout(1); a command killed by a signal is 128 plus the signal.
const ExitTimeout = 124

// Log retention: logs older than KeepAge go, then all but the newest KeepLogs.
const (
	KeepLogs = 50
	KeepAge  = 14 * 24 * time.Hour
)

// Upper limits on what a caller may ask to keep, so a typo cannot allocate
// gigabytes. Run clamps to them; `bud run` rejects larger values.
const (
	MaxTail     = 1000
	MaxContext  = 20
	MaxFailures = 100
)

// maxLine is how many bytes of one line are kept for matching and display.
// The log file always holds the whole line.
const maxLine = 512

// Options say what to run and what to keep.
type Options struct {
	Argv        []string      // the command and its arguments; run without a shell
	LogDir      string        // created if missing
	Timeout     time.Duration // 0 means no timeout
	Grace       time.Duration // between SIGTERM and SIGKILL; 0 means 5s
	Tail        int           // how many last lines to keep
	Context     int           // lines of context around each failure line
	MaxFailures int           // failure lines to keep; later ones are only counted
}

// Line is one numbered line of output (ANSI colour removed, long lines cut).
type Line struct {
	N     int    `json:"n"`
	Text  string `json:"text"`
	Match bool   `json:"match,omitempty"` // a failure line, not just context
}

// Result is what a run leaves behind.
type Result struct {
	Argv       []string `json:"argv"`
	Exit       int      `json:"exit"`
	Signal     string   `json:"signal,omitempty"` // set when a signal ended the command
	TimedOut   bool     `json:"timed_out,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Log        string   `json:"log"`
	LogError   string   `json:"log_error,omitempty"` // the log is incomplete
	Lines      int      `json:"lines"`
	Matches    int      `json:"failure_matches"`
	Failures   []Line   `json:"failures"` // matches with context, in order
	Tail       []Line   `json:"tail"`
}

// Failed reports whether the command did not succeed.
func (r Result) Failed() bool { return r.Exit != 0 }

// pattern is the language-agnostic set of failure markers, documented in
// docs/commands.md#running-commands. Case matters: it keeps "0 errors" and
// "error-free" quiet while catching "FAIL", "--- FAIL", "FAILED", "panic:",
// "error:", "TypeError", "Traceback", "fatal:" and "Exception".
var pattern = regexp.MustCompile(`FAIL|panic:|error:|Error|Traceback|fatal:|Exception`)

// Pattern returns the failure regexp's source, for documentation and tests.
func Pattern() string { return pattern.String() }

// Run starts the command in its own process group, streams its combined
// output to a new log file and returns the result. A command that runs and
// fails is a Result, not an error; an error means it could not be run.
func Run(o Options) (Result, error) {
	if len(o.Argv) == 0 {
		return Result{}, errors.New("no command to run")
	}
	if o.Grace <= 0 {
		o.Grace = 5 * time.Second
	}
	o.Tail = clamp(o.Tail, MaxTail)
	o.Context = clamp(o.Context, MaxContext)
	o.MaxFailures = clamp(o.MaxFailures, MaxFailures)
	logf, err := newLog(o.LogDir)
	if err != nil {
		return Result{}, err
	}
	sc := newScanner(logf, o)

	cmd := exec.Command(o.Argv[0], o.Argv[1:]...)
	cmd.Stdout, cmd.Stderr = sc, sc // the same writer: one pipe, ordered output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A background child that keeps the pipe open must not hold Wait forever.
	cmd.WaitDelay = 2 * time.Second

	sigs := make(chan os.Signal, 2)
	// A dropped SSH session sends SIGHUP; none of these may orphan the command.
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigs)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		logf.Close()
		os.Remove(logf.Name())
		return Result{}, fmt.Errorf("cannot start %q: %w; check it is on PATH, or use --sh for shell syntax", o.Argv[0], err)
	}
	group := cmd.Process.Pid // Setpgid makes the child its own group leader
	done := make(chan struct{})
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- cmd.Wait()
		close(done)
	}()

	var timeout <-chan time.Time
	if o.Timeout > 0 {
		t := time.NewTimer(o.Timeout)
		defer t.Stop()
		timeout = t.C
	}
	var escalate <-chan time.Time // fires when SIGTERM has had its grace period
	timedOut := false
wait:
	for {
		select {
		case <-done:
			if errors.Is(<-waitResult, exec.ErrWaitDelay) {
				// WaitDelay closes the pipes, but does not stop descendants
				// that still hold them open.
				syscall.Kill(-group, syscall.SIGKILL)
			}
			break wait
		case <-timeout:
			timeout, timedOut = nil, true
			syscall.Kill(-group, syscall.SIGTERM)
			escalate = time.After(o.Grace)
		case s := <-sigs: // forward the signal to the whole group
			syscall.Kill(-group, s.(syscall.Signal))
			if escalate == nil {
				escalate = time.After(o.Grace)
			}
		case <-escalate:
			escalate = nil
			syscall.Kill(-group, syscall.SIGKILL)
		}
	}
	if timedOut || escalate != nil {
		// The leader is gone; make sure nothing it started outlives it.
		syscall.Kill(-group, syscall.SIGKILL)
	}
	sc.finish()
	closeErr := logf.Close()

	res := Result{
		Argv:       o.Argv,
		Exit:       cmd.ProcessState.ExitCode(),
		TimedOut:   timedOut,
		DurationMS: time.Since(start).Milliseconds(),
		Log:        logf.Name(),
		Lines:      sc.lines,
		Matches:    sc.matches,
		Failures:   sc.fails,
		Tail:       sc.tail.lines(),
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		res.Signal = ws.Signal().String()
		res.Exit = 128 + int(ws.Signal())
	}
	if timedOut {
		res.Exit = ExitTimeout
	}
	switch {
	case sc.logErr != nil:
		res.LogError = sc.logErr.Error()
	case closeErr != nil:
		res.LogError = closeErr.Error()
	}
	if res.Failures == nil {
		res.Failures = []Line{}
	}
	prune(o.LogDir, filepath.Base(logf.Name()), time.Now())
	return res, nil
}

func clamp(n, max int) int {
	if n < 0 {
		return 0
	}
	return min(n, max)
}

// logName matches the files newLog makes: 20261114T153045-123456.log.
var logName = regexp.MustCompile(`^\d{8}T\d{6}-\d+\.log$`)

// newLog creates a private (0600) log file in dir, which is created 0700
// since output can hold secrets. Names sort by start time.
func newLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the log directory: %w; set XDG_STATE_HOME to a writable directory", err)
	}
	f, err := os.CreateTemp(dir, time.Now().UTC().Format("20060102T150405")+"-*.log")
	if err != nil {
		return nil, fmt.Errorf("create the log file: %w", err)
	}
	return f, nil
}

// prune removes logs in dir older than KeepAge, then the oldest beyond
// KeepLogs, ordering by modification time (names break ties, since the random
// part of a name has no fixed width). It only touches files named like run
// logs and never removes `keep`, the base name of the log just written.
// Failures are ignored: pruning is housekeeping.
func prune(dir, keep string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type logFile struct {
		name string
		mod  time.Time
	}
	var logs []logFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !logName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			logs = append(logs, logFile{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(logs, func(i, j int) bool { // oldest first
		if !logs[i].mod.Equal(logs[j].mod) {
			return logs[i].mod.Before(logs[j].mod)
		}
		return logs[i].name < logs[j].name
	})
	remaining := len(logs)
	for _, l := range logs {
		if l.name == keep {
			continue
		}
		if now.Sub(l.mod) > KeepAge || remaining > KeepLogs {
			if os.Remove(filepath.Join(dir, l.name)) == nil {
				remaining--
			}
		}
	}
}
