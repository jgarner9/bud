// Package nvim launches Neovim as the editor behind bud
package nvim

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/paths"
)

// AppName is NVIM_APPNAME, which points Neovim at bud's dirs from internal/paths
const AppName = paths.AppName

// Launch replaces the bud process with nvim, running in root with each file in
// its own buffer. layout is handed to the config as BUD_LAYOUT when it's set
func Launch(root string, files []string, layout string) error {
	bin, err := exec.LookPath("nvim")
	if err != nil {
		return err
	}

	if err := os.Chdir(root); err != nil {
		return err
	}

	// "--" so a file named like a flag is still opened as a file
	argv := []string{"nvim"}
	if len(files) > 0 {
		argv = append(argv, "--")
		argv = append(argv, files...)
	}

	vars := env()
	// the config calls back into bud (the dashboard runs bud doctor), and this
	// binary may not be the one on PATH
	if self, err := os.Executable(); err == nil {
		vars = setEnv(vars, "BUD_BIN", self)
	}
	if layout != "" {
		vars = setEnv(vars, "BUD_LAYOUT", layout)
	}

	return syscall.Exec(bin, argv, vars)
}

func env() []string {
	return setEnv(os.Environ(), "NVIM_APPNAME", AppName)
}

// setEnv returns vars with key set to value, dropping any earlier value
func setEnv(vars []string, key, value string) []string {
	out := []string{key + "=" + value}
	for _, kv := range vars {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}

	return out
}

// StartupTime starts nvim headless the way bud does and returns how long it took
// to start, from --startuptime's "NVIM STARTED" line. args go before +qa, so
// "-c", "setfiletype go" times opening a Go buffer. Work deferred past startup
// (vim.schedule) isn't counted: this is the time until the first screen
func StartupTime(args ...string) (time.Duration, error) {
	log, err := os.CreateTemp("", "bud-startuptime-*.log")
	if err != nil {
		return 0, err
	}
	// a fresh, empty file each time, since --startuptime appends
	log.Close()
	defer os.Remove(log.Name())

	argv := append([]string{"--headless", "--startuptime", log.Name()}, args...)
	cmd := exec.Command("nvim", append(argv, "+qa!")...)
	cmd.Env = env()
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("nvim: %w: %s", err, strings.TrimSpace(string(out)))
	}

	data, err := os.ReadFile(log.Name())
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "NVIM STARTED") {
			ms, err := strconv.ParseFloat(strings.Fields(line)[0], 64)
			if err != nil {
				return 0, fmt.Errorf("reading --startuptime: %w", err)
			}
			return time.Duration(ms * float64(time.Millisecond)), nil
		}
	}

	return 0, errors.New("--startuptime has no NVIM STARTED line")
}
