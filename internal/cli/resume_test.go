package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/resume"
)

type recTmux struct {
	running bool
	argv    []string
	killed  int
}

func (r *recTmux) HasSession(string) bool { return r.running }
func (r *recTmux) KillSession(string) error {
	r.running = false
	r.killed++
	return nil
}
func (r *recTmux) NewSession(_, _ string, argv []string) error {
	r.running, r.argv = true, argv
	return resume.MarkLaunched(argv[len(argv)-1], time.Now())
}

func TestResumeQueueRunLaunch(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	tm := &recTmux{}
	now := time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)
	origStdin, origNow, origTmux, origStart := resumeStdin, resumeNow, resumeTmux, resumeStart
	t.Cleanup(func() { resumeStdin, resumeNow, resumeTmux, resumeStart = origStdin, origNow, origTmux, origStart })
	resumeNow = func() time.Time { return now }
	resumeTmux = func(string) resume.Tmux { return tm }

	checkout := t.TempDir()
	if err := resumeQueueRun(&Globals{}, nil, checkout, ""); err == nil {
		t.Error("queue without a file was accepted")
	}
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("Resume from handoff/README.md.\nBUD_RESUME_PROMPT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := resumeQueueRun(&Globals{stdout: &out}, []string{promptFile}, checkout, "build"); err != nil {
		t.Fatal(err)
	}
	resumeStdin = strings.NewReader("again")
	if err := resumeQueueRun(&Globals{}, []string{"-"}, checkout, ""); err == nil || !strings.Contains(err.Error(), "already queued") {
		t.Errorf("a second queue: %v", err)
	}

	now = now.Add(3 * time.Minute)
	out.Reset()
	if err := resumeRunRun(&Globals{stdout: &out}, nil, "tmux", "opencode", 2*time.Minute, 8); err != nil {
		t.Fatal(err)
	}
	resumeStdin = strings.NewReader("Resume again.")
	if err := resumeQueueRun(&Globals{}, []string{"-"}, checkout, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "started the orchestrator") || len(tm.argv) != 6 {
		t.Fatalf("run: %q, argv %v", out.String(), tm.argv)
	}
	entryPath := tm.argv[5]
	if !strings.HasPrefix(entryPath, filepath.Join(state, "bud", "resume", "consumed")) {
		t.Errorf("entry at %s", entryPath)
	}

	// launch execs opencode in the checkout with the prompt as one argument
	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var gotBin string
	var gotArgv []string
	resumeStart = func(b string, argv []string) (func() error, error) {
		gotBin, gotArgv = b, argv
		return func() error { return nil }, nil
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(wd) })
	if err := resumeLaunch([]string{entryPath}, bin); err != nil {
		t.Fatal(err)
	}
	want := []string{bin, "--agent=build", "--prompt=Resume from handoff/README.md.\nBUD_RESUME_PROMPT\n"}
	if gotBin != bin || !slices.Equal(gotArgv, want) {
		t.Errorf("exec %s %q, want %q", gotBin, gotArgv, want)
	}
	if cwd, _ := os.Getwd(); !samePath(cwd, checkout) {
		t.Errorf("launched in %s, want %s", cwd, checkout)
	}

	out.Reset()
	if err := resumeStatus(&Globals{stdout: &out}, nil, "tmux"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kill switch: off", "orchestrator: running", "queued: 2026-11-24T01:03:00Z", "claimed in the last 24 hours: 1", "started"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out.String())
		}
	}
}

func TestResumeOffStopsAndOnResumes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tm := &recTmux{running: true}
	origTmux := resumeTmux
	t.Cleanup(func() { resumeTmux = origTmux })
	resumeTmux = func(string) resume.Tmux { return tm }

	var out bytes.Buffer
	if err := resumeOff(&Globals{stdout: &out}, nil, "tmux"); err != nil {
		t.Fatal(err)
	}
	if tm.killed != 1 || !strings.Contains(out.String(), "stopped the orchestrator") {
		t.Errorf("off: killed %d, %q", tm.killed, out.String())
	}
	q, _ := resumeQueue()
	if !q.Off() {
		t.Fatal("the kill switch isn't on")
	}
	if err := resumeOn(&Globals{stdout: &out}, nil); err != nil {
		t.Fatal(err)
	}
	if q.Off() {
		t.Error("the kill switch is still on")
	}
}

func TestResumeLaunchRecordsAMissingOpencode(t *testing.T) {
	q := resume.Queue{Dir: t.TempDir()}
	if err := q.Add(resume.Entry{Version: resume.Version, Dir: t.TempDir(), Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	_, path, err := q.Claim(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := resumeLaunch([]string{path}, filepath.Join(t.TempDir(), "no-opencode")); err == nil {
		t.Fatal("launch without opencode succeeded")
	}
	e, err := resume.Read(path)
	if err != nil || e.Started() || !strings.Contains(e.Error, "find opencode") {
		t.Fatalf("entry = %+v, %v", e, err)
	}
}

func TestStartChildReportsFailures(t *testing.T) {
	if _, err := startChild(filepath.Join(t.TempDir(), "missing"), []string{"missing"}); err == nil {
		t.Fatal("a missing binary started")
	}
	wait, err := startChild("/bin/sh", []string{"/bin/sh", "-c", "exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := wait(); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("wait = %v", err)
	}
}
