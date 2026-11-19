package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func sh(t *testing.T, script string, mod ...func(*Options)) Result {
	t.Helper()
	o := Options{Argv: []string{"sh", "-c", script}, LogDir: t.TempDir(), Tail: 5, Context: 1, MaxFailures: 10, Grace: 200 * time.Millisecond}
	for _, m := range mod {
		m(&o)
	}
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestExitCodeIsTheCommandsExitCode(t *testing.T) {
	for _, code := range []int{0, 1, 3, 42} {
		if res := sh(t, fmt.Sprintf("echo hi; exit %d", code)); res.Exit != code || res.Failed() != (code != 0) {
			t.Errorf("exit %d: got %+v", code, res)
		}
	}
}

func TestOutputOfBothStreamsGoesToTheLogInOrder(t *testing.T) {
	res := sh(t, "echo one; echo two >&2; echo three")
	data, err := os.ReadFile(res.Log)
	if err != nil || string(data) != "one\ntwo\nthree\n" {
		t.Errorf("log = %q, %v", data, err)
	}
	if res.Lines != 3 {
		t.Errorf("lines = %d", res.Lines)
	}
	if fi, _ := os.Stat(res.Log); fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestFailureLinesComeWithContextAndLineNumbers(t *testing.T) {
	res := sh(t, `echo a; echo b; echo "--- FAIL: TestX (0.00s)"; echo c; echo d; echo e; echo "main.go:3: error: nope"; echo f`, func(o *Options) { o.Context = 1 })
	var got []string
	for _, l := range res.Failures {
		m := "-"
		if l.Match {
			m = ":"
		}
		got = append(got, fmt.Sprintf("%d%s%s", l.N, m, l.Text))
	}
	want := "2-b|3:--- FAIL: TestX (0.00s)|4-c|6-e|7:main.go:3: error: nope|8-f"
	if strings.Join(got, "|") != want {
		t.Errorf("failures = %s\nwant       %s", strings.Join(got, "|"), want)
	}
	if res.Matches != 2 {
		t.Errorf("matches = %d", res.Matches)
	}
}

func TestEachDocumentedPatternMatches(t *testing.T) {
	for _, line := range []string{"FAIL", "--- FAIL: TestX", "FAILED (errors=1)", "panic: boom", "x.c:1: error: bad", "TypeError: x", "Traceback (most recent call last):", "fatal: not a repo", "java.lang.Exception"} {
		if !pattern.MatchString(line) {
			t.Errorf("%q should match %s", line, Pattern())
		}
	}
	for _, line := range []string{"ok  \tpkg\t0.1s", "0 errors", "PASS", "all good"} {
		if pattern.MatchString(line) {
			t.Errorf("%q should not match", line)
		}
	}
}

func TestFailureCapStillCountsTheRest(t *testing.T) {
	res := sh(t, `i=0; while [ $i -lt 30 ]; do echo "FAIL $i"; i=$((i+1)); done`, func(o *Options) { o.MaxFailures = 3; o.Context = 0 })
	if res.Matches != 30 || len(res.Failures) != 3 {
		t.Errorf("matches %d, kept %d", res.Matches, len(res.Failures))
	}
}

func TestTailKeepsTheLastLines(t *testing.T) {
	res := sh(t, `i=1; while [ $i -le 20 ]; do echo "line $i"; i=$((i+1)); done`)
	if len(res.Tail) != 5 || res.Tail[0].Text != "line 16" || res.Tail[4].N != 20 {
		t.Errorf("tail = %+v", res.Tail)
	}
}

func TestColourAndCarriageReturnsAreCleaned(t *testing.T) {
	res := sh(t, `printf '\033[31merror: red\033[0m\n10%%\r50%%\r100%%\n'`)
	if res.Tail[0].Text != "error: red" || res.Tail[1].Text != "100%" {
		t.Errorf("tail = %+v", res.Tail)
	}
	data, _ := os.ReadFile(res.Log)
	if !strings.Contains(string(data), "\x1b[31m") {
		t.Error("the log should keep the raw output")
	}
}

func TestProgressLineRedrawnWithoutNewlineKeepsItsFinalStateAndMatches(t *testing.T) {
	// 800 bytes of earlier redraws, then a final state with a marker; no newline at all.
	res := sh(t, `head -c 800 /dev/zero | tr '\0' x; printf '\r'; printf 'FAIL final state'`)
	if res.Lines != 1 || res.Tail[0].Text != "FAIL final state" || res.Matches != 1 {
		t.Errorf("got %+v", res)
	}
}

func TestMarkersPastHeavyColourStillMatch(t *testing.T) {
	// 300 colour codes (1,500 bytes) before the text: they must not eat the 512-byte budget.
	res := sh(t, `i=0; while [ $i -lt 300 ]; do printf '\033[1;31m'; i=$((i+1)); done; printf 'error: real\n'`)
	if res.Matches != 1 || res.Tail[0].Text != "error: real" {
		t.Errorf("got %+v", res)
	}
}

func TestLastLineWithoutNewlineCounts(t *testing.T) {
	res := sh(t, `printf 'a\nlast'`)
	if res.Lines != 2 || res.Tail[1].Text != "last" {
		t.Errorf("got %+v", res)
	}
}

func TestLargeOutputStreamsToDisk(t *testing.T) {
	const n = 500_000
	res := sh(t, fmt.Sprintf(`yes 'a fairly ordinary line of output' | head -n %d; echo "FAIL at the end"; exit 1`, n), func(o *Options) { o.Tail = 3 })
	if res.Lines != n+1 || res.Exit != 1 || res.Matches != 1 {
		t.Fatalf("lines %d exit %d matches %d", res.Lines, res.Exit, res.Matches)
	}
	if len(res.Tail) != 3 || res.Tail[2].Text != "FAIL at the end" {
		t.Errorf("tail = %+v", res.Tail)
	}
	if fi, err := os.Stat(res.Log); err != nil || fi.Size() != int64(n)*int64(len("a fairly ordinary line of output\n"))+int64(len("FAIL at the end\n")) {
		t.Errorf("log size %v, %v", fi, err)
	}
}

func TestHugeLineIsCutInMemoryButWholeInTheLog(t *testing.T) {
	res := sh(t, `head -c 3000000 /dev/zero | tr '\0' x; echo; echo end`)
	if res.Lines != 2 || len(res.Tail[0].Text) > maxLine+len("…") || !strings.HasSuffix(res.Tail[0].Text, "…") {
		t.Errorf("tail[0] has %d bytes", len(res.Tail[0].Text))
	}
	if fi, _ := os.Stat(res.Log); fi.Size() != 3_000_000+1+4 {
		t.Errorf("log size %d", fi.Size())
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	start := time.Now()
	res := sh(t, `sleep 60 & echo $! > `+pidFile+`; echo started; wait`, func(o *Options) { o.Timeout = 300 * time.Millisecond })
	if !res.TimedOut || res.Exit != ExitTimeout {
		t.Errorf("got %+v", res)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
	data, _ := os.ReadFile(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("pid file %q: %v", data, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Error("the background child outlived the timeout")
	}
	if len(res.Tail) == 0 || res.Tail[0].Text != "started" {
		t.Errorf("output before the kill should be kept: %+v", res.Tail)
	}
}

func TestTimeoutEscalatesToKillWhenTermIsIgnored(t *testing.T) {
	start := time.Now()
	res := sh(t, `trap '' TERM; while :; do sleep 0.05; done`, func(o *Options) { o.Timeout = 200 * time.Millisecond; o.Grace = 300 * time.Millisecond })
	if !res.TimedOut || res.Exit != ExitTimeout || res.Signal != "killed" {
		t.Errorf("got %+v", res)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestSignalDeathIsReported(t *testing.T) {
	res := sh(t, `kill -9 $$`)
	if res.Exit != 128+9 || res.Signal != "killed" {
		t.Errorf("got %+v", res)
	}
}

func TestBackgroundChildHoldingThePipeDoesNotHangTheRun(t *testing.T) {
	start := time.Now()
	res := sh(t, `sleep 5 & echo done; exit 0`)
	if res.Exit != 0 || time.Since(start) > 4*time.Second {
		t.Errorf("exit %d after %v", res.Exit, time.Since(start))
	}
}

func TestCommandThatCannotStartIsAnErrorAndLeavesNoLog(t *testing.T) {
	dir := t.TempDir()
	_, err := Run(Options{Argv: []string{"/no/such/tool"}, LogDir: dir})
	if err == nil || !strings.Contains(err.Error(), "--sh") {
		t.Errorf("err = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left %d files", len(entries))
	}
	if _, err := Run(Options{LogDir: dir}); err == nil {
		t.Error("an empty command should be an error")
	}
}

func TestPruneDropsOldAndExcessLogsOnly(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-age)
		os.Chtimes(p, mt, mt)
		return p
	}
	old := mk("20200101T000000-1.log", KeepAge+time.Hour)
	stranger := mk("notes.txt", 100*KeepAge)
	var recent []string
	for i := 0; i < KeepLogs+5; i++ {
		// Younger as i grows; names are all the same second with random-width suffixes.
		recent = append(recent, mk(fmt.Sprintf("20261114T000000-%d.log", 1000-i*7), time.Duration(KeepLogs+5-i)*time.Minute))
	}
	keep := filepath.Base(recent[0]) // the oldest by time, but the one just written
	prune(dir, keep, now)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(old) {
		t.Error("an old log should be pruned")
	}
	if !exists(stranger) {
		t.Error("a file that is not a run log must be left alone")
	}
	if !exists(recent[0]) {
		t.Error("the log just written must survive")
	}
	left := 0
	for _, p := range recent {
		if exists(p) {
			left++
		}
	}
	if left != KeepLogs {
		t.Errorf("%d recent logs left, want %d", left, KeepLogs)
	}
	// The oldest by modification time go first, whatever their names sort as.
	for i := 1; i <= 5; i++ {
		if exists(recent[i]) {
			t.Errorf("recent[%d] is among the 5 oldest after the kept one and should be pruned", i)
		}
	}
	for i := 6; i < len(recent); i++ {
		if !exists(recent[i]) {
			t.Errorf("recent[%d] is newer and should be kept", i)
		}
	}
}

func TestRunKeepsItsOwnLogWhenTheLogDirIsNotClean(t *testing.T) {
	dir := t.TempDir()
	res, err := Run(Options{Argv: []string{"true"}, LogDir: dir + string(filepath.Separator) + "." + string(filepath.Separator) + "runs" + string(filepath.Separator) + string(filepath.Separator)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.Log); err != nil {
		t.Errorf("the new log is gone: %v", err)
	}
}

func TestPruneNeverRemovesTheKeptLogEvenWhenItIsOld(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "20200101T000000-5.log")
	os.WriteFile(p, []byte("x"), 0o600)
	old := time.Now().Add(-2 * KeepAge)
	os.Chtimes(p, old, old)
	prune(dir, "20200101T000000-5.log", time.Now())
	if _, err := os.Stat(p); err != nil {
		t.Error("the kept log was pruned")
	}
}

func TestTailContextAndFailuresAreClamped(t *testing.T) {
	res := sh(t, `echo FAIL`, func(o *Options) { o.Tail, o.Context, o.MaxFailures = 2_000_000_000, 2_000_000_000, 2_000_000_000 })
	if res.Lines != 1 || res.Matches != 1 {
		t.Errorf("got %+v", res)
	}
	if res = sh(t, `echo a`, func(o *Options) { o.Tail = -5 }); len(res.Tail) != 0 {
		t.Errorf("negative tail: %+v", res.Tail)
	}
}

func TestHangupAndQuitAreForwardedToTheCommand(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGQUIT} {
		ready := filepath.Join(t.TempDir(), "ready")
		go func() {
			for i := 0; i < 500; i++ {
				if _, err := os.Stat(ready); err == nil {
					syscall.Kill(os.Getpid(), sig)
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
		name := strings.TrimPrefix(sig.String(), "SIG")
		res := sh(t, `trap 'echo caught; exit 5' `+map[syscall.Signal]string{syscall.SIGHUP: "HUP", syscall.SIGQUIT: "QUIT"}[sig]+`; touch `+ready+`; while :; do sleep 0.05; done`)
		if res.Exit != 5 || len(res.Tail) == 0 || res.Tail[len(res.Tail)-1].Text != "caught" {
			t.Errorf("%s (%s): got exit %d tail %+v", name, sig, res.Exit, res.Tail)
		}
	}
}

func TestRunPrunesAfterItself(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "20200101T000000-1.log")
	os.WriteFile(old, []byte("x"), 0o600)
	long := time.Now().Add(-2 * KeepAge)
	os.Chtimes(old, long, long)
	if _, err := Run(Options{Argv: []string{"true"}, LogDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the old log should be gone")
	}
}

func TestUnwritableLogDirIsAnError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o600)
	_, err := Run(Options{Argv: []string{"true"}, LogDir: filepath.Join(blocker, "runs")})
	if err == nil || !strings.Contains(err.Error(), "BRAIN_STATE_DIR") {
		t.Errorf("err = %v", err)
	}
}
