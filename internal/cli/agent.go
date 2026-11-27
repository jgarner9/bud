package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"syscall"

	backend "jgarner.dev/bud/internal/agent"
	"jgarner.dev/bud/internal/envfile"
	"jgarner.dev/bud/internal/optoken"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Agent())
}

// Agent opens OpenCode's own TUI in a project from the shell, outside Neovim
// (Suite D139)
func Agent() *Command {
	return &Command{
		Name:  "agent",
		Usage: "Opens OpenCode's own TUI in a project, outside the editor: agent [path] (default: the current dir)",
		Run:   agent,
	}
}

// execProcess replaces bud with argv in dir; swappable in tests
var execProcess = func(dir string, argv, env []string) error {
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return syscall.Exec(argv[0], argv, env)
}

// execOpenCodeTUI replaces bud with OpenCode's own TUI in dir
func execOpenCodeTUI(g *Globals, dir string) error {
	return execOpenCode(g, backend.TUI(dir), dir)
}

// execOpenCode replaces bud with argv, an opencode command line, in dir
func execOpenCode(g *Globals, argv []string, dir string) error {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("%s isn't installed: run bud ai setup", argv[0])
	}
	argv[0] = bin
	g.Logf("running %q in %s", argv, dir)
	return execProcess(dir, argv, agentEnv())
}

// agentEnv is this environment with BUD_BIN pointing at this bud, as nvim's
// sessions have it
func agentEnv() []string {
	env := os.Environ()
	self, err := os.Executable()
	if err != nil {
		return env
	}
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "BUD_BIN=") })

	return append(env, "BUD_BIN="+self)
}

func agent(g *Globals, args []string) error {
	if len(args) > 1 {
		return usageErrorf("agent takes at most one path")
	}
	ws, err := resolveWorkspace(args)
	if err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	file := p.OpenCodeEnv()
	if _, err := os.Stat(file); err == nil {
		op, err := exec.LookPath("op")
		if err != nil {
			return errors.New("op isn't installed: install the 1Password CLI to launch OpenCode with synced secrets")
		}
		bin, err := exec.LookPath("opencode")
		if err != nil {
			return errors.New("opencode isn't installed: run bud ai setup")
		}
		// A running shared server would keep its original environment.
		// op needs its service token, but OpenCode only needs the resolved values.
		child := []string{bin, "--standalone", ws.Root}
		if isTerminal(os.Stdin) && isTerminal(os.Stdout) && isTerminal(os.Stderr) {
			child = agentPTY(child)
		}
		argv := append([]string{op, "run", "--env-file=" + file, "--", "/usr/bin/env", "-u", "OP_SERVICE_ACCOUNT_TOKEN"}, child...)
		env, err := agentOpEnv(g, p, agentEnv())
		if err != nil {
			return err
		}
		g.Logf("running %q in %s", argv, ws.Root)
		return execProcess(ws.Root, argv, env)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking the synced OpenCode environment: %w", err)
	}
	return execOpenCodeTUI(g, ws.Root)
}

// agentTokenFinder says where bud looks for the service account token;
// swappable so tests never reach a real Keychain
var agentTokenFinder = func(p *paths.Paths) optoken.Finder { return optoken.Finder{File: p.OpTokenFile()} }

// agentOpEnv is the environment op run starts with: this one plus the service
// account token optoken finds (the environment's own, the Keychain's or the
// token file's), so the launch needs no sign-in (Suite D142, D144). The token is for op run alone: the OpenCode
// child runs under env -u, and nothing is written to disk. With no token
// anywhere, op authenticates however it can, as before; a token source that
// exists but fails is an error
func agentOpEnv(g *Globals, p *paths.Paths, env []string) ([]string, error) {
	tok, err := agentTokenFinder(p).Lookup()
	switch {
	case errors.Is(err, optoken.ErrNone):
		return env, nil
	case err != nil:
		return nil, fmt.Errorf("find the 1Password service account token for op run: %w", err)
	}
	g.Logf("op run uses the %s", tok)

	return envfile.With(env, optoken.EnvVar, tok.Value()), nil
}

// agentPTY runs inside op run, whose output pipes still mask resolved secrets.
func agentPTY(child []string) []string {
	if runtime.GOOS == "darwin" {
		// Apple's script copies the initial window size but doesn't relay SIGWINCH.
		// Give Expect its program on fd 3, leaving stdin attached to the terminal.
		// sh -c consumes "bud-agent" as $0; "$@" starts with the OpenCode binary.
		return append([]string{"/bin/sh", "-c", `exec /usr/bin/expect -N -n -f /dev/fd/3 "$@" 3<<'EXPECT'
spawn -noecho {*}$argv
set slave $spawn_out(slave,name)
proc resize {} {
    global slave
    lassign [exec /bin/stty size <@stdin] rows cols
    exec /bin/stty rows $rows cols $cols < $slave
}
trap resize WINCH
resize
interact
lassign [wait] pid id failed status reason sig
if {$failed} {exit 1}
if {$reason eq "CHILDKILLED"} {exit [expr {128 + [exec /bin/bash -c {kill -l "$1"} sh $sig]}]}
exit $status
EXPECT`, "bud-agent"}, child...)
	}
	// Older util-linux script takes a shell command, not an argument vector.
	// Use sh for that command regardless of the user's shell; restore SHELL
	// before exec so OpenCode's own terminal still uses the user's choice.
	restore := "-u SHELL"
	if shell, ok := os.LookupEnv("SHELL"); ok {
		restore = shellQuote("SHELL=" + shell)
	}
	cmd := "exec /usr/bin/env " + restore
	for _, arg := range child {
		cmd += " " + shellQuote(arg)
	}
	return []string{"/usr/bin/env", "SHELL=/bin/sh", "script", "-qec", cmd, "/dev/null"}
}
