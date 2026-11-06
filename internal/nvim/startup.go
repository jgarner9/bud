package nvim

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// startupTimeout is how long StartupTime waits for a first screen
var startupTimeout = 10 * time.Second

// StartupTime starts bud's nvim with a screen attached and returns how long it
// took to draw the first one (D35, D92), from --startuptime's "first screen
// update" line. A headless run would stop short of it: UIEnter and the first
// redraw (the statusline, the dashboard) only happen with a screen. The screen
// is a bare UI attached over --embed's msgpack-rpc, so no terminal is needed,
// and the moment nvim waits for it to attach isn't counted.
//
// With no filetype it's bare bud, which shows the dashboard; with one, a new
// scratch buffer of that filetype. BUD_STARTUP_PROBE=1 tells the config to
// skip the work it does after the first screen (a Claude Code session, the
// layout) and the session save on quit. It runs in a temp dir and its own
// process group, so whatever it started is stopped with it, and it quits
// without writing ShaDa, so the scratch file never shows up in recent files
func StartupTime(filetype string) (time.Duration, error) {
	dir, err := os.MkdirTemp("", "bud-startuptime-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	log := filepath.Join(dir, "startuptime.log")

	argv := []string{"--embed", "--startuptime", log}
	if filetype != "" {
		argv = append(argv, "-c", "setfiletype "+filetype, "--", filepath.Join(dir, "scratch"))
	}
	cmd := exec.Command("nvim", argv...)
	// in the temp dir, so quitting can't save a session over a real project's
	cmd.Dir = dir
	// what a bud session hands down (bud doctor run from bud's own terminal)
	// would change what's timed (BUD_OPEN=project skips the dashboard,
	// BUD_PACK_RESTORE every plugin's config), so only BUD_BIN, which a real
	// launch sets too, goes through
	cmd.Env = setEnv(withoutBud(env()), "BUD_STARTUP_PROBE", "1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// files, not pipes: the redraws nvim sends the screen are dropped, and
	// with no copying to wait for, Wait returns when nvim exits even if
	// something it started still holds them
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	defer devnull.Close()
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		return 0, err
	}
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = devnull, stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("nvim: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stop := func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-exited
	}

	attach := rpcRequest(1, "nvim_ui_attach", 80, 24, map[string]any{"ext_linegrid": true})
	if _, err := stdin.Write(attach); err != nil {
		stop()
		return 0, fmt.Errorf("nvim: attaching a screen: %w", err)
	}

	deadline := time.After(startupTimeout)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			// what it started goes too
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			why := "nvim exited before its first screen"
			if err != nil {
				why += ": " + err.Error()
			}
			out, _ := os.ReadFile(stderr.Name())
			if msg := strings.TrimSpace(string(out)); msg != "" {
				why += ": " + msg
			}
			return 0, errors.New(why)
		case <-deadline:
			stop()
			return 0, fmt.Errorf("nvim drew no first screen within %s", startupTimeout)
		case <-tick.C:
			data, err := os.ReadFile(log)
			if err != nil {
				continue
			}
			d, ok, err := firstScreen(data)
			if err != nil {
				stop()
				return 0, err
			}
			if !ok {
				continue
			}
			// a plain quit, then whatever's left of the group goes
			_, _ = stdin.Write(rpcRequest(2, "nvim_command", "set shada= | qa!"))
			select {
			case <-exited:
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			case <-time.After(2 * time.Second):
				stop()
			}
			return d, nil
		}
	}
}

// firstScreen reads a --startuptime log for the time to the first screen,
// less the time nvim spent waiting for a screen to attach. ok is false until
// the log has the line, which nvim writes once the first screen is drawn
func firstScreen(log []byte) (d time.Duration, ok bool, err error) {
	var waited, drawn float64
	for _, line := range strings.Split(string(log), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) < 2:
		case strings.HasSuffix(line, ": done waiting for UI"):
			waited, err = strconv.ParseFloat(strings.TrimSuffix(fields[1], ":"), 64)
		case strings.HasSuffix(line, ": first screen update"):
			drawn, err = strconv.ParseFloat(fields[0], 64)
			ok = true
		}
		if err != nil {
			return 0, false, fmt.Errorf("reading --startuptime: %w", err)
		}
	}
	if !ok {
		return 0, false, nil
	}
	if drawn < waited {
		return 0, false, errors.New("reading --startuptime: the first screen came before nvim stopped waiting for one")
	}
	// the log is in milliseconds to three places: whole microseconds
	return time.Duration(math.Round((drawn-waited)*1000)) * time.Microsecond, true, nil
}

// rpcRequest encodes a msgpack-rpc request, [0, id, method, params], with just
// enough msgpack for what StartupTime sends: ints, strings, bools and maps
// with string keys
func rpcRequest(id int, method string, params ...any) []byte {
	b := []byte{0x94, 0x00}
	b = msgpack(b, id)
	b = msgpack(b, method)
	return msgpack(b, params)
}

func msgpack(b []byte, v any) []byte {
	switch v := v.(type) {
	case bool:
		if v {
			return append(b, 0xc3)
		}
		return append(b, 0xc2)
	case int:
		if v >= 0 && v < 128 {
			return append(b, byte(v))
		}
		return binary.BigEndian.AppendUint64(append(b, 0xd3), uint64(v))
	case string:
		b = header(b, len(v), 0xa0, 32, 0xda)
		return append(b, v...)
	case []any:
		b = header(b, len(v), 0x90, 16, 0xdc)
		for _, e := range v {
			b = msgpack(b, e)
		}
		return b
	case map[string]any:
		b = header(b, len(v), 0x80, 16, 0xde)
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			b = msgpack(msgpack(b, k), v[k])
		}
		return b
	}
	panic(fmt.Sprintf("msgpack: can't encode %T", v))
}

// header writes a length: in the fix byte below limit, else the 16-bit form
func header(b []byte, n int, fix byte, limit int, wide byte) []byte {
	if n < limit {
		return append(b, fix|byte(n))
	}
	return binary.BigEndian.AppendUint16(append(b, wide), uint16(n))
}
