// Package nvim launches Neovim as the editor behind bud
package nvim

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

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
