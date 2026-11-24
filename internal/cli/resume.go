package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/resume"
)

func init() {
	RegisterCommand(Resume())
}

// resumeStdin, resumeNow, resumeTmux, resumeExec and resumeQueue are
// swappable for tests
var (
	resumeStdin io.Reader = os.Stdin
	resumeNow             = time.Now
	resumeTmux            = func(path string) resume.Tmux { return resume.ExecTmux{Path: path} }
	resumeExec            = syscall.Exec
	resumeQueue           = func() (resume.Queue, error) {
		p, err := paths.Resolve()
		if err != nil {
			return resume.Queue{}, err
		}
		return resume.Queue{Dir: p.Resume()}, nil
	}
)

// Resume starts the next OpenCode orchestrator after a handoff (JGA-394);
// see docs/internals/resume.md
func Resume() *Command {
	var dir, agent, opencode, tmux string
	var minAge time.Duration
	var maxPerDay int
	tmuxFlag := func(fs *flag.FlagSet) {
		fs.StringVar(&tmux, "tmux", "tmux", "the tmux binary")
	}
	return &Command{
		Name:  "resume",
		Usage: "Starts the next OpenCode orchestrator session after one hands off",
		Subs: []*Command{
			{
				Name:  "queue",
				Usage: "queue a resume prompt read from stdin: queue [--dir <checkout>] [--agent <name>] -",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&dir, "dir", "", "where the next session starts (default: the current dir)")
					fs.StringVar(&agent, "agent", "", "the OpenCode agent to start it with (default: OpenCode's)")
				},
				Run: func(g *Globals, args []string) error { return resumeQueueRun(g, args, dir, agent) },
			},
			{
				Name:  "run",
				Usage: "what the timer runs: start the queued prompt's orchestrator in tmux, once",
				Flags: func(fs *flag.FlagSet) {
					tmuxFlag(fs)
					fs.StringVar(&opencode, "opencode", "opencode", "the opencode binary")
					fs.DurationVar(&minAge, "min-age", 2*time.Minute, "how long a prompt waits, so the session that queued it can finish")
					fs.IntVar(&maxPerDay, "max-per-day", 8, "the most orchestrators started in any 24 hours; 0 for no cap")
				},
				Run: func(g *Globals, args []string) error {
					return resumeRunRun(g, args, tmux, opencode, minAge, maxPerDay)
				},
			},
			{
				Name:  "launch",
				Usage: "run inside the tmux session: exec opencode with a consumed entry's prompt: launch <entry.json>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&opencode, "opencode", "opencode", "the opencode binary")
				},
				Run: func(g *Globals, args []string) error { return resumeLaunch(args, opencode) },
			},
			{
				Name:  "status",
				Usage: "show the kill switch, the queued prompt, the running orchestrator and recent starts",
				Flags: tmuxFlag,
				Run:   func(g *Globals, args []string) error { return resumeStatus(g, args, tmux) },
			},
			{
				Name:  "off",
				Usage: "the kill switch: stop the running orchestrator and start no more until on",
				Flags: tmuxFlag,
				Run:   func(g *Globals, args []string) error { return resumeOff(g, args, tmux) },
			},
			{
				Name:  "on",
				Usage: "turn the kill switch off, so queued prompts start again",
				Run:   resumeOn,
			},
		},
	}
}

func resumeQueueRun(g *Globals, args []string, dir, agent string) error {
	if len(args) != 1 || args[0] != "-" {
		return usageErrorf("resume queue reads the prompt from stdin: pass - and pipe it in")
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(resumeStdin, resume.MaxPrompt+1))
	if err != nil {
		return fmt.Errorf("read the prompt: %w", err)
	}
	q, err := resumeQueue()
	if err != nil {
		return err
	}
	e := resume.Entry{Version: resume.Version, QueuedAt: resumeNow().UTC(), Dir: abs, Agent: agent, Prompt: string(data)}
	if err := q.Add(e); err != nil {
		if errors.Is(err, resume.ErrQueued) {
			return fmt.Errorf("%w: bud resume status shows it; nothing was queued", err)
		}
		return fmt.Errorf("queue the prompt: %w", err)
	}
	fmt.Fprintf(curatorOut(g), "resume: queued; the next orchestrator starts in %s within a few minutes\n", abs)

	return nil
}

func resumeRunRun(g *Globals, args []string, tmux, opencode string, minAge time.Duration, maxPerDay int) error {
	if len(args) > 0 {
		return usageErrorf("resume run takes no arguments")
	}
	if maxPerDay < 0 {
		return usageErrorf("--max-per-day must be 0 or more")
	}
	q, err := resumeQueue()
	if err != nil {
		return err
	}
	bud, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this bud binary: %w", err)
	}
	r := &resume.Runner{
		Queue: q, Tmux: resumeTmux(tmux), Bud: bud, Opencode: opencode,
		MinAge: minAge, MaxPerDay: maxPerDay, Now: resumeNow,
	}
	msg, err := r.Run()
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	fmt.Fprintf(curatorOut(g), "resume: %s\n", msg)

	return nil
}

func resumeLaunch(args []string, opencode string) error {
	if len(args) != 1 {
		return usageErrorf("resume launch takes one consumed entry file")
	}
	e, err := resume.Read(args[0])
	if err != nil {
		return err
	}
	if err := e.Validate(); err != nil {
		return fmt.Errorf("resume launch: %w", err)
	}
	bin, err := exec.LookPath(opencode)
	if err != nil {
		return fmt.Errorf("find opencode: %w; pass --opencode to bud resume run", err)
	}
	if err := os.Chdir(e.Dir); err != nil {
		return err
	}

	return resumeExec(bin, resume.Argv(bin, e), os.Environ())
}

func resumeStatus(g *Globals, args []string, tmux string) error {
	if len(args) > 0 {
		return usageErrorf("resume status takes no arguments")
	}
	q, err := resumeQueue()
	if err != nil {
		return err
	}
	w := curatorOut(g)
	if q.Off() {
		fmt.Fprintln(w, "kill switch: on (bud resume on turns it off)")
	} else {
		fmt.Fprintln(w, "kill switch: off")
	}
	if resumeTmux(tmux).HasSession(resume.Session) {
		fmt.Fprintf(w, "orchestrator: running (tmux attach -t %s)\n", resume.Session)
	} else {
		fmt.Fprintln(w, "orchestrator: not running")
	}
	e, err := q.Pending()
	if err != nil {
		return err
	}
	if e == nil {
		fmt.Fprintln(w, "queued: nothing")
	} else {
		fmt.Fprintf(w, "queued: %s, for %s\n", e.QueuedAt.Format(time.RFC3339), e.Dir)
	}
	n, err := q.ClaimedSince(resumeNow().Add(-24 * time.Hour))
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "started in the last 24 hours: %d\n", n)
	last, err := q.Last()
	if err != nil {
		return err
	}
	if last != nil && last.ClaimedAt != nil {
		state := "started"
		if !last.Started {
			state = "failed: " + last.Error
		}
		fmt.Fprintf(w, "last: %s, %s\n", last.ClaimedAt.Format(time.RFC3339), state)
	}

	return nil
}

func resumeOff(g *Globals, args []string, tmux string) error {
	if len(args) > 0 {
		return usageErrorf("resume off takes no arguments")
	}
	q, err := resumeQueue()
	if err != nil {
		return err
	}
	if err := q.SetOff(true); err != nil {
		return fmt.Errorf("turn the kill switch on: %w", err)
	}
	t := resumeTmux(tmux)
	stopped := "no orchestrator was running"
	if t.HasSession(resume.Session) {
		if err := t.KillSession(resume.Session); err != nil {
			return fmt.Errorf("the kill switch is on, but stopping the orchestrator failed: %w", err)
		}
		stopped = "stopped the orchestrator"
	}
	fmt.Fprintf(curatorOut(g), "resume: off; %s; a queued prompt stays queued until bud resume on\n", stopped)

	return nil
}

func resumeOn(g *Globals, args []string) error {
	if len(args) > 0 {
		return usageErrorf("resume on takes no arguments")
	}
	q, err := resumeQueue()
	if err != nil {
		return err
	}
	if err := q.SetOff(false); err != nil {
		return err
	}
	fmt.Fprintln(curatorOut(g), "resume: on; the next timer run starts a queued prompt")

	return nil
}
