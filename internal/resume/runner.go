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
	// LaunchWait is how long the runner waits for launch to report from
	// inside tmux; 10 seconds when zero
	LaunchWait time.Duration
	// Sleep is time.Sleep, swappable for tests
	Sleep func(time.Duration)
}

// ErrCap is Run's error when the daily cap stops a start; the prompt stays
// queued. The cap counts claims, failed starts included: each one needs a
// look, and a loop of failures shouldn't run unchecked either
var ErrCap = errors.New("daily cap reached")

// Run starts the next orchestrator when a prompt is queued, and says what it did
func (r *Runner) Run() (string, error) {
	unlock, ok, err := r.Queue.Lock()
	if err != nil {
		return "", fmt.Errorf("take the lock: %w", err)
	}
	if !ok {
		return "another bud resume run holds the lock", nil
	}
	defer unlock()
	// checked under the lock, which off takes too, so off can't land
	// between this check and the start
	if r.Queue.Off() {
		return "off: the kill switch is on (bud resume on turns it off)", nil
	}

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
		return "", fmt.Errorf("%w: %d prompts claimed in the last 24 hours (--max-per-day %d); the prompt stays queued", ErrCap, n, r.MaxPerDay)
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
		if e.Error == "" {
			e.Error = err.Error()
			if rerr := r.Queue.record(path, e); rerr != nil {
				return "", fmt.Errorf("%w (and recording it failed: %v)", err, rerr)
			}
		}
		return "", fmt.Errorf("%w; the prompt is kept in %s and won't run again on its own", err, path)
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
	// entry and starts opencode, so the prompt never goes through tmux's
	// message size limit or a shell
	if err := r.Tmux.NewSession(r.session(), e.Dir, []string{r.Bud, "resume", "launch", "--opencode", r.Opencode, path}); err != nil {
		return err
	}

	return r.waitLaunch(e, path)
}

// waitLaunch waits for launch to record that it reached opencode, or why it
// didn't. tmux accepting the session only means the command was spawned
func (r *Runner) waitLaunch(e *Entry, path string) error {
	wait, sleep := r.LaunchWait, r.Sleep
	if wait == 0 {
		wait = 10 * time.Second
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	const step = 200 * time.Millisecond
	for waited := time.Duration(0); ; waited += step {
		got, err := Read(path)
		if err != nil {
			return err
		}
		if got.Error != "" {
			e.Error = got.Error
			return fmt.Errorf("launch: %s", got.Error)
		}
		if got.LaunchedAt != nil {
			return nil
		}
		if !r.Tmux.HasSession(r.session()) {
			return errors.New("the tmux session ended before bud resume launch reported; check the unit's PATH")
		}
		if waited >= wait {
			return fmt.Errorf("bud resume launch didn't report within %s", wait)
		}
		sleep(step)
	}
}

// Argv is the command the orchestrator runs. It never carries --auto,
// --yolo or --dangerously-skip-permissions: asks wait for Jason, and the
// unattended-deny plugin refuses one left unanswered (Suite D99, D111)
func Argv(opencode string, e *Entry) []string {
	// attached values, so a prompt or agent that starts with - stays the
	// option's value instead of parsing as a flag of its own
	argv := []string{opencode}
	if e.Agent != "" {
		argv = append(argv, "--agent="+e.Agent)
	}

	return append(argv, "--prompt="+e.Prompt)
}
