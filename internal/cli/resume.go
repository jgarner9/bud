package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/envfile"
	"jgarner.dev/bud/internal/optoken"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/resume"
)

func init() {
	RegisterCommand(Resume())
}

// resumeStdin, resumeNow, resumeTmux, resumeStart and resumeQueue are
// swappable for tests
var (
	resumeStdin io.Reader = os.Stdin
	resumeNow             = time.Now
	resumeTmux            = func(path string) resume.Tmux { return resume.ExecTmux{Path: path} }
	resumeStart           = startChild
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
	var sec resumeSecrets
	tmuxFlag := func(fs *flag.FlagSet) {
		fs.StringVar(&tmux, "tmux", "tmux", "the tmux binary")
	}
	secretFlags := func(fs *flag.FlagSet) {
		fs.Func("op-env-file", "an env file of op:// references op run resolves into opencode's environment; repeat for more", func(v string) error {
			sec.envFiles = append(sec.envFiles, v)
			return nil
		})
		fs.StringVar(&sec.tokenFile, "op-token-file", "", "the env file holding OP_SERVICE_ACCOUNT_TOKEN for op run (default: op's own sign-in)")
	}
	return &Command{
		Name:  "resume",
		Usage: "Starts the next OpenCode orchestrator session after one hands off",
		Subs: []*Command{
			{
				Name:  "queue",
				Usage: "queue a resume prompt from a file, or - for stdin: queue [--dir <checkout>] [--agent <name>] <file|->",
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
					secretFlags(fs)
					fs.StringVar(&opencode, "opencode", "opencode", "the opencode binary")
					fs.DurationVar(&minAge, "min-age", 2*time.Minute, "how long a prompt waits, so the session that queued it can finish")
					fs.IntVar(&maxPerDay, "max-per-day", 8, "the most orchestrators started in any 24 hours; 0 for no cap")
				},
				Run: func(g *Globals, args []string) error {
					return resumeRunRun(g, args, tmux, opencode, minAge, maxPerDay, sec)
				},
			},
			{
				Name:  "launch",
				Usage: "run inside the tmux session: start opencode with a consumed entry's prompt: launch <entry.json>",
				Flags: func(fs *flag.FlagSet) {
					secretFlags(fs)
					fs.StringVar(&opencode, "opencode", "opencode", "the opencode binary")
				},
				Run: func(g *Globals, args []string) error { return resumeLaunch(args, opencode, sec) },
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
	if len(args) != 1 {
		return usageErrorf("resume queue takes the prompt's file, or - for stdin")
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
	in := resumeStdin
	if args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			return fmt.Errorf("read the prompt: %w", err)
		}
		defer f.Close()
		in = f
	}
	data, err := io.ReadAll(io.LimitReader(in, resume.MaxPrompt+1))
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

// resumeSecrets is where an orchestrator's provider keys and tool secrets
// come from: op:// references that op run resolves when launch starts
// opencode, so no value is in a file, an argument or tmux's environment
type resumeSecrets struct {
	envFiles  []string
	tokenFile string
}

// validate refuses a token file with nothing for it to open: it would be
// ignored, which reads as working
func (s resumeSecrets) validate() error {
	if s.tokenFile != "" && len(s.envFiles) == 0 {
		return usageErrorf("--op-token-file needs at least one --op-env-file")
	}

	return nil
}

// flags are the launch flags that pass these on
func (s resumeSecrets) flags() []string {
	var out []string
	for _, f := range s.envFiles {
		out = append(out, "--op-env-file", f)
	}
	if s.tokenFile != "" {
		out = append(out, "--op-token-file", s.tokenFile)
	}
	return out
}

func resumeRunRun(g *Globals, args []string, tmux, opencode string, minAge time.Duration, maxPerDay int, sec resumeSecrets) error {
	if len(args) > 0 {
		return usageErrorf("resume run takes no arguments")
	}
	if err := sec.validate(); err != nil {
		return err
	}
	if maxPerDay < 0 {
		return usageErrorf("--max-per-day must be 0 or more")
	}
	if minAge < 0 {
		return usageErrorf("--min-age must be 0 or more")
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
		Queue: q, Tmux: resumeTmux(tmux), Bud: bud, Opencode: opencode, LaunchFlags: sec.flags(),
		MinAge: minAge, MaxPerDay: maxPerDay, Now: resumeNow,
	}
	msg, err := r.Run()
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	fmt.Fprintf(curatorOut(g), "resume: %s\n", msg)

	return nil
}

func resumeLaunch(args []string, opencode string, sec resumeSecrets) error {
	if len(args) != 1 {
		return usageErrorf("resume launch takes one consumed entry file")
	}
	if err := sec.validate(); err != nil {
		return err
	}
	e, err := resume.Read(args[0])
	if err != nil {
		return err
	}
	if err := e.Validate(); err != nil {
		return fmt.Errorf("resume launch: %w", err)
	}
	// from here every failure is recorded in the entry, which the runner
	// waits on, so a launch that dies in its tmux pane still shows in status
	fail := func(err error) error {
		if err == nil {
			return nil
		}
		if merr := resume.MarkFailed(args[0], err); merr != nil {
			return fmt.Errorf("%w (and recording it failed: %v)", err, merr)
		}
		return err
	}
	bin, err := exec.LookPath(opencode)
	if err != nil {
		return fail(fmt.Errorf("find opencode: %w; fix the unit's PATH or pass --opencode to bud resume run", err))
	}
	envv, err := sec.resolve()
	if err != nil {
		return fail(err)
	}
	if err := os.Chdir(e.Dir); err != nil {
		return fail(err)
	}
	// opencode runs as a child, not through exec, so a failed start comes
	// back here before launched_at is written: the runner can't read a
	// start as good when opencode never ran
	wait, stop, err := resumeStart(bin, resume.Argv(bin, e), envv)
	if err != nil {
		return fail(fmt.Errorf("start opencode: %w", err))
	}
	if err := resume.MarkLaunched(args[0], resumeNow()); err != nil {
		// the runner will call this start failed, so don't leave opencode
		// running behind it
		stop()
		return fail(fmt.Errorf("opencode started, but recording it failed, so it was stopped: %w", err))
	}

	return wait()
}

// resolve returns the environment opencode starts with: this process's, minus
// the service account token, plus every variable the env files name. An
// op:// reference is read with op read, a short-lived child that alone gets
// the token, so the token is never in this long-lived process's environment
// or in opencode's, and the values come back over a pipe. A value that isn't
// a reference is passed as it is
func (s resumeSecrets) resolve() ([]string, error) {
	envv := envfile.Without(os.Environ(), optoken.EnvVar)
	if len(s.envFiles) == 0 {
		return envv, nil
	}
	opEnv := os.Environ()
	if s.tokenFile != "" {
		token, err := envfile.Value(s.tokenFile, optoken.EnvVar)
		if err != nil {
			return nil, fmt.Errorf("--op-token-file: %w", err)
		}
		opEnv = envfile.With(opEnv, optoken.EnvVar, token)
	}
	op, err := exec.LookPath("op")
	if err != nil {
		return nil, fmt.Errorf("find op for --op-env-file: %w; install the 1Password CLI or fix the unit's PATH", err)
	}
	for _, f := range s.envFiles {
		pairs, err := envfile.Read(f)
		if err != nil {
			return nil, fmt.Errorf("--op-env-file: %w; bud sync writes opencode.env, and server/resume/README.md says what goes in the others", err)
		}
		for _, kv := range pairs {
			val := kv[1]
			if strings.HasPrefix(val, "op://") {
				cmd := exec.Command(op, "read", "--no-newline", val)
				cmd.Env = opEnv
				out, err := cmd.Output()
				if err != nil {
					return nil, fmt.Errorf("op read for %s in %s: %w%s", kv[0], f, err, stderrOf(err))
				}
				val = string(out)
			}
			envv = envfile.With(envv, kv[0], val)
		}
	}

	return envv, nil
}

// stderrOf is what a failed command said, for the error: op's message names
// the item, never a value
func stderrOf(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return ": " + strings.TrimSpace(string(ee.Stderr))
	}

	return ""
}

// startChild starts argv on this terminal and returns a wait that forwards
// the signals that stop a tmux pane and returns how the child exited, and a
// stop that kills and reaps the child instead
func startChild(bin string, argv, envv []string) (wait func() error, stop func(), err error) {
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// resolve already left out the service account token: the orchestrator,
	// and every command it runs, gets the secrets but never the token that
	// reads the vault
	cmd.Env = envv
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	if err := cmd.Start(); err != nil {
		signal.Stop(sigs)
		return nil, nil, err
	}
	stop = func() {
		signal.Stop(sigs)
		cmd.Process.Kill()
		cmd.Wait()
	}
	wait = func() error {
		defer signal.Stop(sigs)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		for {
			select {
			case sig := <-sigs:
				cmd.Process.Signal(sig)
			case err := <-done:
				if err != nil {
					return fmt.Errorf("opencode: %w", err)
				}
				return nil
			}
		}
	}

	return wait, stop, nil
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
	fmt.Fprintf(w, "claimed in the last 24 hours: %d (the --max-per-day cap counts these, failed starts too)\n", n)
	last, err := q.Last()
	if err != nil {
		return err
	}
	if last != nil && last.ClaimedAt != nil {
		state := "started"
		switch {
		case last.Error != "":
			state = "failed: " + last.Error
		case last.LaunchedAt == nil:
			state = "claimed, but launch never reported"
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
	// waits out a run in the middle of a start, so the session it starts is
	// the one stopped below, not one that comes up after
	unlock, err := q.LockWait()
	if err != nil {
		return fmt.Errorf("take the lock: %w", err)
	}
	defer unlock()
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
	// the same lock off takes, so the last of off and on to finish wins
	unlock, err := q.LockWait()
	if err != nil {
		return fmt.Errorf("take the lock: %w", err)
	}
	defer unlock()
	if err := q.SetOff(false); err != nil {
		return err
	}
	fmt.Fprintln(curatorOut(g), "resume: on; the next timer run starts a queued prompt")

	return nil
}
