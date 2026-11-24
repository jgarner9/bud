package resume

import (
	"errors"
	"fmt"
	"time"
)

// Session names the one tmux session an orchestrator runs in
const Session = "bud-orchestrator"

// Tmux is what the runner needs from tmux, swappable for tests
type Tmux interface {
	HasSession(name string) bool
	KillSession(name string) error
	// NewSession starts argv detached in dir, without a shell in between
	NewSession(name, dir string, argv []string) error
}

// Runner is one bud resume run: what the timer starts
type Runner struct {
	Queue     Queue
	Tmux      Tmux
	Bud       string        // this bud binary, which tmux runs as bud resume launch
	Opencode  string        // the opencode binary
	Session   string        // the tmux session; Session when empty
	MinAge    time.Duration // a prompt waits this long, so the session that queued it can finish its last reply
	MaxPerDay int           // starts allowed in any 24 hours
	Now       func() time.Time
}

// ErrCap is Run's error when the daily cap stops a start; the prompt stays queued
var ErrCap = errors.New("daily cap reached")

// Run starts the next orchestrator when a prompt is queued, and says what it did
func (r *Runner) Run() (string, error) {
	if r.Queue.Off() {
		return "off: the kill switch is on (bud resume on turns it off)", nil
	}
	unlock, ok, err := r.Queue.Lock()
	if err != nil {
		return "", fmt.Errorf("take the lock: %w", err)
	}
	if !ok {
		return "another bud resume run holds the lock", nil
	}
	defer unlock()

	e, err := r.Queue.Pending()
	if err != nil {
		return "", err
	}
	if e == nil {
		return "nothing queued", nil
	}
	now := r.Now()
	if age := now.Sub(e.QueuedAt); age < r.MinAge {
		return fmt.Sprintf("queued %s ago; waiting until it's %s old", age.Round(time.Second), r.MinAge), nil
	}
	n, err := r.Queue.ClaimedSince(now.Add(-24 * time.Hour))
	if err != nil {
		return "", err
	}
	if r.MaxPerDay > 0 && n >= r.MaxPerDay {
		return "", fmt.Errorf("%w: %d orchestrators started in the last 24 hours (--max-per-day %d); the prompt stays queued", ErrCap, n, r.MaxPerDay)
	}

	e, path, err := r.Queue.Claim(now)
	if err != nil {
		return "", fmt.Errorf("claim the prompt: %w", err)
	}
	if e == nil {
		return "nothing queued", nil
	}
	// from here the prompt is consumed: a failure is recorded, never retried
	if err := r.start(e, path); err != nil {
		e.Error = err.Error()
		if rerr := r.Queue.record(path, e); rerr != nil {
			return "", fmt.Errorf("%w (and recording it failed: %v)", err, rerr)
		}
		return "", fmt.Errorf("%w; the prompt is kept in %s and won't run again on its own", err, path)
	}
	e.Started = true
	if err := r.Queue.record(path, e); err != nil {
		return "", fmt.Errorf("started, but recording it in %s failed: %w", path, err)
	}

	return fmt.Sprintf("started the orchestrator in tmux session %s, in %s", r.session(), e.Dir), nil
}

func (r *Runner) session() string {
	if r.Session == "" {
		return Session
	}
	return r.Session
}

func (r *Runner) start(e *Entry, path string) error {
	if err := e.Validate(); err != nil {
		return err
	}
	// the session that queued this prompt is the one being replaced
	if r.Tmux.HasSession(r.session()) {
		if err := r.Tmux.KillSession(r.session()); err != nil {
			return fmt.Errorf("stop the previous orchestrator: %w", err)
		}
	}

	// tmux runs bud resume launch, which reads the prompt from the consumed
	// entry and execs opencode, so the prompt never goes through tmux's
	// message size limit or a shell
	return r.Tmux.NewSession(r.session(), e.Dir, []string{r.Bud, "resume", "launch", "--opencode", r.Opencode, path})
}

// Argv is the command the orchestrator runs. It never carries --auto,
// --yolo or --dangerously-skip-permissions: asks wait for Jason, and the
// unattended-deny plugin refuses one left unanswered (Suite D99, D111)
func Argv(opencode string, e *Entry) []string {
	argv := []string{opencode}
	if e.Agent != "" {
		argv = append(argv, "--agent", e.Agent)
	}

	return append(argv, "--prompt", e.Prompt)
}
