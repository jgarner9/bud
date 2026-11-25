package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
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
	if err := os.WriteFile(p.MachineConfig(), []byte(`{"ai":{"backend":"opencode"}}`), 0o600); err != nil {
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
	orig := execClaude
	t.Cleanup(func() { execClaude = orig })
	execClaude = func(dir string, argv, env []string) error {
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

func TestAgent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Claude Code's own TUI: the claude backend (D131)
	claudeBackend(t)
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin))

	t.Setenv("BUD_BIN", "/an/older/bud")
	var dir string
	var argv, env []string
	orig := execClaude
	execClaude = func(d string, a, e []string) error { dir, argv, env = d, a, e; return nil }
	t.Cleanup(func() { execClaude = orig })

	project := t.TempDir()
	os.Mkdir(filepath.Join(project, ".git"), 0o755)
	file := filepath.Join(project, "main.go")
	os.WriteFile(file, nil, 0o644)

	if err := agent(&Globals{}, []string{file}); err != nil {
		t.Fatal(err)
	}
	// a fresh conversation unless ai.claude.resume says continue (D106)
	if dir != project || !slices.Equal(argv, append([]string{bin}, wantPlugin(t)...)) {
		t.Fatalf("ran %q in %s", argv, dir)
	}
	// bud-ide's hook knows it's bud's session by BUD_BIN, which is this bud
	self, _ := os.Executable()
	if got := slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return !strings.HasPrefix(kv, "BUD_BIN=") }); !slices.Equal(got, []string{"BUD_BIN=" + self}) {
		t.Fatalf("BUD_BIN in the env: %q", got)
	}

	// resume = "continue" in a shared ~/.claude passes no --continue (JGA-231):
	// the newest conversation there may be one you started by hand
	state := os.Getenv("XDG_STATE_HOME")
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	cfgFile := filepath.Join(state, "bud", "config.json")
	os.WriteFile(cfgFile, []byte(`{"ai": {"backend": "claude", "claude": {"resume": "continue"}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, append([]string{bin}, wantPlugin(t)...)) {
		t.Fatalf("resume continue in a shared dir: argv %q", argv)
	}

	// ai.claude.command wraps it
	wrapper := filepath.Join(filepath.Dir(bin), "headroom")
	os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(cfgFile, []byte(`{"ai": {"backend": "claude", "claude": {"resume": "continue", "command": ["headroom", "wrap", "claude", "--"]}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, append([]string{wrapper, "wrap", "claude", "--"}, wantPlugin(t)...)) {
		t.Fatalf("wrapped argv %q", argv)
	}
	os.WriteFile(cfgFile, []byte(`{"ai": {"backend": "claude"}}`), 0o644)

	if err := agent(&Globals{}, []string{project, file}); err == nil {
		t.Fatal("two paths accepted")
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := agent(&Globals{}, nil); err == nil || !strings.Contains(err.Error(), "bud claude setup") {
		t.Fatalf("no claude: %v", err)
	}
}

// resume "continue" resumes the editor's saved conversation, and --continue
// only happens in bud's own Claude Code config dir (JGA-231)
func TestResumeFlags(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	root, _ := filepath.EvalSymlinks(t.TempDir())
	cont := config.Defaults()
	cont.AI.Claude.Resume = "continue"

	write := func(dir, name string) {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}

	// the default starts fresh
	if got := resumeFlags(config.Defaults(), root); got != nil {
		t.Fatalf("new: %q", got)
	}

	// shared ~/.claude with a hand-started conversation: nothing to continue
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	write(claudeProjectDir(root), "by-hand.jsonl")
	if got := resumeFlags(cont, root); got != nil {
		t.Fatalf("shared dir passed %q", got)
	}

	// the editor's saved conversation is resumed by id, shared or not
	ids, _ := json.Marshal(map[string]string{root: "saved-id"})
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	os.WriteFile(filepath.Join(state, "bud", "claude-sessions.json"), ids, 0o644)
	if got := resumeFlags(cont, root); got != nil {
		t.Fatalf("a saved id whose conversation is gone: %q", got)
	}
	write(claudeProjectDir(root), "saved-id.jsonl")
	if got := resumeFlags(cont, root); !slices.Equal(got, []string{"--resume", "saved-id"}) {
		t.Fatalf("saved: %q", got)
	}
	os.Remove(filepath.Join(state, "bud", "claude-sessions.json"))

	// bud's own config dir: --continue, when there is a conversation
	p, _ := paths.Resolve()
	t.Setenv("CLAUDE_CONFIG_DIR", p.ClaudeConfig())
	if got := resumeFlags(cont, root); got != nil {
		t.Fatalf("own dir with no conversation: %q", got)
	}
	write(claudeProjectDir(root), "any.jsonl")
	if got := resumeFlags(cont, root); !slices.Equal(got, []string{"--continue"}) {
		t.Fatalf("own dir: %q", got)
	}

	// no home directory and no config dir set: nowhere to look, so nothing to
	// continue (not a lookup relative to the working directory)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	if dir := claudeProjectDir(root); dir != "" {
		t.Fatalf("no home: %q", dir)
	}
	if anyConversation(root) {
		t.Fatal("no home but a conversation was found")
	}
}

// claudeBackend sets ai.backend = "claude" in the test's config.json
func claudeBackend(t *testing.T) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "bud")
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"ai": {"backend": "claude"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// on another backend, bud agent runs its own TUI: OpenClaw's on the
// project's Gateway session, the token in the environment (D139)
func TestAgentOpenClaw(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	bin := filepath.Join(t.TempDir(), "openclaw")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin))
	token := filepath.Join(t.TempDir(), "gateway-token")
	os.WriteFile(token, []byte("tok\n"), 0o600)
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	cfg, _ := json.Marshal(map[string]any{"ai": map[string]any{"backend": "openclaw", "openclaw": map[string]any{"url": "wss://home:18789", "token_file": token}}})
	os.WriteFile(filepath.Join(state, "bud", "config.json"), cfg, 0o644)

	var dir string
	var argv, env []string
	orig := execClaude
	execClaude = func(d string, a, e []string) error { dir, argv, env = d, a, e; return nil }
	t.Cleanup(func() { execClaude = orig })

	project, _ := filepath.EvalSymlinks(t.TempDir())
	os.Mkdir(filepath.Join(project, ".git"), 0o755)
	// no session yet: OpenClaw's main one
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{bin, "tui", "--url", "wss://home:18789"}) {
		t.Fatalf("no saved session: ran %q", argv)
	}
	// the session the editor saved (D139)
	ids, _ := json.Marshal(map[string]any{project: map[string]string{"backend": "openclaw", "id": "abc"}})
	os.WriteFile(filepath.Join(state, "bud", "agent-sessions.json"), ids, 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	want := []string{bin, "tui", "--url", "wss://home:18789", "--session", "agent:main:acp-bridge:abc"}
	if dir != project || !slices.Equal(argv, want) {
		t.Fatalf("ran %q in %s, want %q", argv, dir, want)
	}
	if !slices.Contains(env, "OPENCLAW_GATEWAY_TOKEN=tok") {
		t.Errorf("no token in the env")
	}

	// bud chat: the Gateway's one chat session, outside any project
	if err := chat(&Globals{}, nil, false); err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(state, "bud", "chat") || argv[len(argv)-1] != "agent:main:bud:chat" {
		t.Errorf("chat ran %q in %s", argv, dir)
	}
	if err := chat(&Globals{}, nil, true); err == nil || !strings.Contains(err.Error(), "--resume is Claude Code's") {
		t.Errorf("chat --resume: %v", err)
	}

	// not installed
	t.Setenv("PATH", t.TempDir())
	if err := agent(&Globals{}, []string{project}); err == nil || !strings.Contains(err.Error(), "openclaw isn't installed: run bud ai setup") {
		t.Errorf("missing openclaw: %v", err)
	}
}
