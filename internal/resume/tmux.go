package resume

import (
	"fmt"
	"os/exec"
	"strings"
)

// ExecTmux runs the tmux binary at Path
type ExecTmux struct{ Path string }

// HasSession reports whether the session exists. The = asks tmux for an
// exact name, not a prefix match
func (t ExecTmux) HasSession(name string) bool {
	return exec.Command(t.Path, "has-session", "-t", "="+name).Run() == nil
}

// KillSession stops the session and everything in it
func (t ExecTmux) KillSession(name string) error {
	return t.run("kill-session", "-t", "="+name)
}

// NewSession starts argv detached in dir. With more than one argument tmux
// execs the command directly, without a shell
func (t ExecTmux) NewSession(name, dir string, argv []string) error {
	return t.run(append([]string{"new-session", "-d", "-s", name, "-c", dir}, argv...)...)
}

func (t ExecTmux) run(args ...string) error {
	out, err := exec.Command(t.Path, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}
