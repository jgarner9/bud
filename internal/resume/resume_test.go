package resume

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)

func entry(t *testing.T, at time.Time) Entry {
	t.Helper()
	return Entry{Version: Version, QueuedAt: at, Dir: t.TempDir(), Prompt: "Resume the work."}
}

type fakeTmux struct {
	sessions map[string]bool
	started  [][]string
	dirs     []string
	failNew  error
	// launch stands in for bud resume launch inside the pane; nil marks it launched
	launch func(path string)
}

func (f *fakeTmux) HasSession(name string) bool { return f.sessions[name] }
func (f *fakeTmux) KillSession(name string) error {
	delete(f.sessions, name)
	return nil
}
func (f *fakeTmux) NewSession(name, dir string, argv []string) error {
	if f.failNew != nil {
		return f.failNew
	}
	if f.sessions[name] {
		return errors.New("duplicate session")
	}
	f.sessions[name] = true
	f.started = append(f.started, argv)
	f.dirs = append(f.dirs, dir)
	if f.launch != nil {
		f.launch(argv[len(argv)-1])
	} else if err := MarkLaunched(argv[len(argv)-1], t0); err != nil {
		return err
	}
	return nil
}

func runner(t *testing.T, q Queue, tm *fakeTmux, now time.Time) *Runner {
	t.Helper()
	return &Runner{
		Queue: q, Tmux: tm, Bud: "/bin/bud", Opencode: "opencode",
		MinAge: 2 * time.Minute, MaxPerDay: 2, Now: func() time.Time { return now },
		LaunchWait: time.Second, Sleep: func(time.Duration) {},
	}
}

func TestAddRefusesASecondPrompt(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	if err := q.Add(entry(t, t0)); err != nil {
		t.Fatal(err)
	}
	if err := q.Add(entry(t, t0)); !errors.Is(err, ErrQueued) {
		t.Fatalf("second Add = %v, want ErrQueued", err)
	}
	e, err := q.Pending()
	if err != nil || e == nil || e.Prompt != "Resume the work." {
		t.Fatalf("Pending = %+v, %v", e, err)
	}
	left, _ := filepath.Glob(filepath.Join(q.Dir, ".pending-*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestAddValidates(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	for name, mod := range map[string]func(*Entry){
		"empty prompt":  func(e *Entry) { e.Prompt = " \n" },
		"long prompt":   func(e *Entry) { e.Prompt = strings.Repeat("x", MaxPrompt+1) },
		"relative dir":  func(e *Entry) { e.Dir = "bud" },
		"missing dir":   func(e *Entry) { e.Dir = filepath.Join(e.Dir, "gone") },
		"flag agent":    func(e *Entry) { e.Agent = "--auto" },
		"wrong version": func(e *Entry) { e.Version = 2 },
	} {
		e := entry(t, t0)
		mod(&e)
		if err := q.Add(e); err == nil {
			t.Errorf("%s: Add succeeded", name)
		}
	}
	if e, _ := q.Pending(); e != nil {
		t.Errorf("an invalid entry was queued: %+v", e)
	}
}

func TestRunStartsOnceAndReplacesTheOldSession(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	e := entry(t, t0)
	e.Agent = "build"
	if err := q.Add(e); err != nil {
		t.Fatal(err)
	}
	tm := &fakeTmux{sessions: map[string]bool{Session: true}}
	r := runner(t, q, tm, t0.Add(3*time.Minute))
	msg, err := r.Run()
	if err != nil || !strings.HasPrefix(msg, "started") {
		t.Fatalf("Run = %q, %v", msg, err)
	}
	if len(tm.started) != 1 || tm.dirs[0] != e.Dir {
		t.Fatalf("started %v in %v", tm.started, tm.dirs)
	}
	argv := tm.started[0]
	if argv[0] != "/bin/bud" || !slices.Equal(argv[1:5], []string{"resume", "launch", "--opencode", "opencode"}) {
		t.Errorf("argv = %v", argv)
	}
	got, err := Read(argv[5])
	if err != nil || !got.Started() || got.ClaimedAt == nil || got.Prompt != e.Prompt {
		t.Errorf("consumed entry = %+v, %v", got, err)
	}

	// the same prompt never starts twice
	if msg, err := r.Run(); err != nil || msg != "nothing queued" {
		t.Errorf("second Run = %q, %v", msg, err)
	}
	if len(tm.started) != 1 {
		t.Errorf("started %d times", len(tm.started))
	}
}

func TestRunWaitsForMinAge(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	if err := q.Add(entry(t, t0)); err != nil {
		t.Fatal(err)
	}
	tm := &fakeTmux{sessions: map[string]bool{}}
	msg, err := runner(t, q, tm, t0.Add(time.Minute)).Run()
	if err != nil || !strings.Contains(msg, "waiting") || len(tm.started) != 0 {
		t.Fatalf("Run = %q, %v, started %v", msg, err, tm.started)
	}
}

func TestRunHonoursTheKillSwitch(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	if err := q.Add(entry(t, t0)); err != nil {
		t.Fatal(err)
	}
	if err := q.SetOff(true); err != nil {
		t.Fatal(err)
	}
	tm := &fakeTmux{sessions: map[string]bool{}}
	r := runner(t, q, tm, t0.Add(time.Hour))
	if msg, err := r.Run(); err != nil || !strings.HasPrefix(msg, "off") || len(tm.started) != 0 {
		t.Fatalf("Run = %q, %v", msg, err)
	}
	if e, _ := q.Pending(); e == nil {
		t.Fatal("the kill switch consumed the prompt")
	}
	if err := q.SetOff(false); err != nil {
		t.Fatal(err)
	}
	if err := q.SetOff(false); err != nil {
		t.Fatalf("turning it off twice: %v", err)
	}
	if _, err := r.Run(); err != nil || len(tm.started) != 1 {
		t.Fatalf("after on: %v, started %v", err, tm.started)
	}
}

func TestRunStopsAtTheDailyCap(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	tm := &fakeTmux{sessions: map[string]bool{}}
	now := t0
	for i := range 2 {
		if err := q.Add(entry(t, now)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(3 * time.Minute)
		if _, err := runner(t, q, tm, now).Run(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := q.Add(entry(t, now)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Minute)
	if _, err := runner(t, q, tm, now).Run(); !errors.Is(err, ErrCap) {
		t.Fatalf("third Run = %v, want ErrCap", err)
	}
	if e, _ := q.Pending(); e == nil {
		t.Fatal("the cap consumed the prompt")
	}
	// a day after the first start, it's allowed again
	if _, err := runner(t, q, tm, t0.Add(25*time.Hour)).Run(); err != nil {
		t.Fatalf("next day: %v", err)
	}
	if len(tm.started) != 3 {
		t.Errorf("started %d, want 3", len(tm.started))
	}
}

func TestRunRecordsAFailedStartAndDoesntRetry(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	if err := q.Add(entry(t, t0)); err != nil {
		t.Fatal(err)
	}
	tm := &fakeTmux{sessions: map[string]bool{}, failNew: errors.New("no server")}
	r := runner(t, q, tm, t0.Add(time.Hour))
	if _, err := r.Run(); err == nil || !strings.Contains(err.Error(), "won't run again") {
		t.Fatalf("Run = %v", err)
	}
	last, err := q.Last()
	if err != nil || last == nil || last.Started() || last.Error != "no server" {
		t.Fatalf("Last = %+v, %v", last, err)
	}
	tm.failNew = nil
	if msg, _ := r.Run(); msg != "nothing queued" {
		t.Errorf("retry: %q", msg)
	}
}

func TestRunSkipsWhileAnotherRunHoldsTheLock(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	if err := q.Add(entry(t, t0)); err != nil {
		t.Fatal(err)
	}
	unlock, ok, err := q.Lock()
	if err != nil || !ok {
		t.Fatalf("Lock = %v, %v", ok, err)
	}
	tm := &fakeTmux{sessions: map[string]bool{}}
	r := runner(t, q, tm, t0.Add(time.Hour))
	if msg, err := r.Run(); err != nil || !strings.Contains(msg, "lock") || len(tm.started) != 0 {
		t.Fatalf("Run = %q, %v", msg, err)
	}
	unlock()
	if _, err := r.Run(); err != nil || len(tm.started) != 1 {
		t.Fatalf("after unlock: %v, started %v", err, tm.started)
	}
}

func TestClaimIsOnce(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	if err := q.Add(entry(t, t0)); err != nil {
		t.Fatal(err)
	}
	e, _, err := q.Claim(t0)
	if err != nil || e == nil {
		t.Fatalf("Claim = %+v, %v", e, err)
	}
	if e, _, err := q.Claim(t0.Add(time.Second)); err != nil || e != nil {
		t.Fatalf("second Claim = %+v, %v", e, err)
	}
	if _, err := os.Stat(q.pending()); !os.IsNotExist(err) {
		t.Errorf("pending.json still there: %v", err)
	}
}

func TestArgvNeverAutoApproves(t *testing.T) {
	e := &Entry{Agent: "build", Prompt: "--auto"}
	got := Argv("/bin/opencode", e)
	// attached values: a prompt of --auto stays the prompt, never a flag
	want := []string{"/bin/opencode", "--agent=build", "--prompt=--auto"}
	if !slices.Equal(got, want) {
		t.Fatalf("Argv = %v, want %v", got, want)
	}
	for _, a := range got {
		if a == "--auto" || a == "--yolo" || a == "--dangerously-skip-permissions" {
			t.Errorf("argv carries %s", a)
		}
	}
}

func TestRunReportsALaunchThatFails(t *testing.T) {
	for name, tc := range map[string]struct {
		launch func(*fakeTmux) func(string)
		want   string
	}{
		"launch records an error": {
			launch: func(*fakeTmux) func(string) {
				return func(p string) { MarkFailed(p, errors.New("find opencode: not found")) }
			},
			want: "find opencode",
		},
		"the pane dies before reporting": {
			launch: func(f *fakeTmux) func(string) { return func(string) { delete(f.sessions, Session) } },
			want:   "ended before",
		},
		"launch never reports": {
			launch: func(*fakeTmux) func(string) { return func(string) {} },
			want:   "didn't report",
		},
	} {
		t.Run(name, func(t *testing.T) {
			q := Queue{Dir: t.TempDir()}
			if err := q.Add(entry(t, t0)); err != nil {
				t.Fatal(err)
			}
			tm := &fakeTmux{sessions: map[string]bool{}}
			tm.launch = tc.launch(tm)
			r := runner(t, q, tm, t0.Add(time.Hour))
			if _, err := r.Run(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run = %v, want %q", err, tc.want)
			}
			last, err := q.Last()
			if err != nil || last == nil || last.Started() || !strings.Contains(last.Error, tc.want) {
				t.Fatalf("Last = %+v, %v", last, err)
			}
			if tm.sessions[Session] {
				t.Error("the failed start's tmux session is still running")
			}
			if msg, _ := r.Run(); msg != "nothing queued" {
				t.Errorf("retried: %q", msg)
			}
		})
	}
}

func TestLockWaitWaitsForARun(t *testing.T) {
	q := Queue{Dir: t.TempDir()}
	unlock, ok, err := q.Lock()
	if err != nil || !ok {
		t.Fatalf("Lock = %v, %v", ok, err)
	}
	got := make(chan struct{})
	go func() {
		u, err := q.LockWait()
		if err == nil {
			u()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("LockWait didn't wait")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("LockWait never got the lock")
	}
}
