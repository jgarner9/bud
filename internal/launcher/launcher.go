// Package launcher opens a terminal emulator window running a command (D11),
// for bud launch and the app bundles. Each terminal is one file that says how
// to ask it for that window, so more (WezTerm, Alacritty, iTerm2) are one
// file each
package launcher

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"jgarner.dev/bud/internal/installer"
)

// Window is what to open: a new terminal window titled Title, in Dir, running Argv
type Window struct {
	Dir   string
	Argv  []string
	Title string
}

// Found is where a terminal is installed: its binary on PATH and, on macOS,
// its app bundle. Either may be ""
type Found struct {
	Bin, App string
}

// command turns a window into the command line that opens it
type command func(w Window, f Found) ([]string, error)

// commands holds each terminal's command, added by its own file
var commands = map[string]command{}

// swappable in tests
var (
	goos  = runtime.GOOS
	start = startDetached
)

// Command returns the command line that opens w in the terminal called name,
// installed at f
func Command(name string, w Window, f Found) ([]string, error) {
	cmd, ok := commands[name]
	if !ok {
		return nil, fmt.Errorf("bud can't launch %s", name)
	}
	if f.Bin == "" && f.App == "" {
		return nil, fmt.Errorf("%s isn't installed", name)
	}

	return cmd(w, f)
}

// Launch opens w in the terminal called name and returns once it's started,
// without waiting for the window to close
func Launch(name string, w Window) error {
	t, ok := installer.Lookup(name)
	if !ok {
		return fmt.Errorf("bud can't launch %s", name)
	}
	bin, app := t.Locate()
	argv, err := Command(name, w, Found{Bin: bin, App: app})
	if err != nil {
		return err
	}

	return start(argv, w.Dir)
}

// openApp is the macOS way to start an app bundle with arguments. -n starts
// a new instance, since an app that's already running would ignore them
func openApp(app string, args ...string) []string {
	return append([]string{"/usr/bin/open", "-n", "-a", app, "--args"}, args...)
}

// startDetached starts argv in dir and lets it go. open returns as soon as
// the app is running, so it's waited for and its error reported; a terminal
// started directly gets its own session, so closing the shell bud launch ran
// in doesn't take the window with it
func startDetached(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	if argv[0] == "/usr/bin/open" {
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return fmt.Errorf("%s: %s", strings.Join(argv, " "), msg)
			}
			return err
		}
		return nil
	}

	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	return cmd.Process.Release()
}
