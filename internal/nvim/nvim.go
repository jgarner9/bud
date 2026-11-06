// Package nvim launches Neovim as the editor behind bud
package nvim

import (
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
// its own buffer. layout is handed to the config as BUD_LAYOUT when it's set,
// and project as BUD_OPEN=project, which opens root in a layout instead of the
// dashboard (bud.layout)
func Launch(root string, files []string, layout string, project bool) error {
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

	return syscall.Exec(bin, argv, launchEnv(layout, project))
}

// started is when this bud process started, near enough: package init runs
// just before main
var started = time.Now()

// launchEnv is nvim's environment for Launch
func launchEnv(layout string, project bool) []string {
	vars := env()
	// the config calls back into bud (the dashboard runs bud doctor), and this
	// binary may not be the one on PATH
	if self, err := os.Executable(); err == nil {
		vars = setEnv(vars, "BUD_BIN", self)
	}
	if layout != "" {
		vars = setEnv(vars, "BUD_LAYOUT", layout)
	}
	if project {
		vars = setEnv(vars, "BUD_OPEN", "project")
	}
	// bud.startup times the first screen from here, so bud's own start counts
	return setEnv(vars, "BUD_LAUNCHED", strconv.FormatInt(started.UnixMicro(), 10))
}

func env() []string {
	return setEnv(os.Environ(), "NVIM_APPNAME", AppName)
}

// withoutBud returns vars without bud's own BUD_* variables, but for BUD_BIN
func withoutBud(vars []string) []string {
	out := make([]string, 0, len(vars))
	for _, kv := range vars {
		if !strings.HasPrefix(kv, "BUD_") || strings.HasPrefix(kv, "BUD_BIN=") {
			out = append(out, kv)
		}
	}

	return out
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
