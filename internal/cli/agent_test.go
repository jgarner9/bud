package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/paths"
)

func TestAgentOpenCodeSyncedEnv(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.MachineConfig(), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	bin, op := filepath.Join(binDir, "opencode"), filepath.Join(binDir, "op")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{bin, project}
	orig := execProcess
	t.Cleanup(func() { execProcess = orig })
	execProcess = func(dir string, argv, env []string) error {
		if dir != project || !slices.Equal(argv, want) {
			t.Fatalf("ran %q in %s; want %q in %s", argv, dir, want, project)
		}
		return nil
	}
	run := func() error { return agent(&Globals{}, []string{project}) }
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.OpenCodeEnv(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil || !strings.Contains(err.Error(), "1Password CLI") {
		t.Fatalf("missing op: %v", err)
	}
	if err := os.WriteFile(op, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	want = []string{op, "run", "--env-file=" + p.OpenCodeEnv(), "--", "/usr/bin/env", "-u", "OP_SERVICE_ACCOUNT_TOKEN", bin, "--standalone", project}
	if isTerminal(os.Stdin) && isTerminal(os.Stdout) && isTerminal(os.Stderr) {
		want = append(want[:7:7], agentPTY(want[7:])...)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentPTYArgumentsAndStatus(t *testing.T) {
	t.Setenv("SHELL", "/not a 'posix' shell")
	dir := t.TempDir()
	bin := filepath.Join(dir, "Open Code's probe")
	project := "a 'project'; $HOME\n$(false)"
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
[ -t 0 ] && [ -t 1 ] && [ -t 2 ] || exit 1
[ "$#" = 2 ] && [ "$1" = --standalone ] && [ "$2" = "$PROJECT" ] || exit 2
[ "$SHELL" = "/not a 'posix' shell" ] && [ -z "${OP_SERVICE_ACCOUNT_TOKEN+x}" ] || exit 3
exec python3 -c 'import os, signal, sys
def report(*_):
    cols, rows = os.get_terminal_size(1)
    print("SIZE", rows, cols, flush=True)
    if (rows, cols) == (50, 160):
        sys.exit(42)
signal.signal(signal.SIGWINCH, report)
report()
while True:
    signal.pause()'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT", project)
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "test")
	args := append([]string{"-u", "OP_SERVICE_ACCOUNT_TOKEN"}, agentPTY([]string{bin, "--standalone", project})...)
	// The wrapper itself is interactive: its stdin is a tty while op pipes
	// its output. Use a real outer terminal even when go test runs in CI.
	const probe = `import fcntl, os, pty, select, struct, subprocess, sys, termios, time
pid, master = pty.fork()
if pid == 0:
    p = subprocess.Popen(sys.argv[1:], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    while True:
        data = os.read(p.stdout.fileno(), 8192)
        if not data: break
        os.write(1, data)
    os._exit(p.wait())
try:
    for rows, cols in [(63,208), (24,80), (50,160)]:
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        expected = ('SIZE %d %d' % (rows, cols)).encode()
        buf = b''
        deadline = time.monotonic() + 5
        while expected not in buf:
            timeout = max(0, deadline - time.monotonic())
            if not select.select([master], [], [], timeout)[0]:
                raise RuntimeError('missing %r: %r' % (expected, buf))
            buf += os.read(master, 8192)
    sys.exit(os.waitstatus_to_exitcode(os.waitpid(pid, 0)[1]))
finally:
    os.close(master)
`
	cmd := exec.Command("python3", append([]string{"-c", probe, "/usr/bin/env"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil || cmd.ProcessState.ExitCode() != 42 {
		t.Fatalf("PTY launch: %v: %s", err, out)
	}
}

// with no synced environment, bud agent runs OpenCode's own TUI in the
// project, whatever config.json says (Suite D139)
func TestAgent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := filepath.Join(t.TempDir(), "opencode")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin))
	t.Setenv("BUD_BIN", "/an/older/bud")

	var dir string
	var argv, env []string
	orig := execProcess
	execProcess = func(d string, a, e []string) error { dir, argv, env = d, a, e; return nil }
	t.Cleanup(func() { execProcess = orig })

	project, _ := filepath.EvalSymlinks(t.TempDir())
	os.Mkdir(filepath.Join(project, ".git"), 0o755)
	file := filepath.Join(project, "main.go")
	os.WriteFile(file, nil, 0o644)

	// a config.json from before OpenCode-only, naming another backend, changes nothing
	state := os.Getenv("XDG_STATE_HOME")
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	os.WriteFile(filepath.Join(state, "bud", "config.json"), []byte(`{"ai": {"backend": "acme"}}`), 0o644)

	if err := agent(&Globals{}, []string{file}); err != nil {
		t.Fatal(err)
	}
	if dir != project || !slices.Equal(argv, []string{bin, project}) {
		t.Fatalf("ran %q in %s", argv, dir)
	}
	self, _ := os.Executable()
	if got := slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return !strings.HasPrefix(kv, "BUD_BIN=") }); !slices.Equal(got, []string{"BUD_BIN=" + self}) {
		t.Fatalf("BUD_BIN in the env: %q", got)
	}

	if err := agent(&Globals{}, []string{project, file}); err == nil {
		t.Fatal("two paths accepted")
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := agent(&Globals{}, nil); err == nil || !strings.Contains(err.Error(), "opencode isn't installed: run bud ai setup") {
		t.Fatalf("no opencode: %v", err)
	}
}
