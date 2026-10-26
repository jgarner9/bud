// Package nvim launches Neovim as the editor behind bud
package nvim

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const AppName = "bud"

func Launch(root, file string) error {
	bin, err := exec.LookPath("nvim")
	if err != nil {
		return err
	}

	if err := os.Chdir(root); err != nil {
		return err
	}

	argv := []string{"nvim"}
	if file != "" {
		argv = append(argv, file)
	}

	return syscall.Exec(bin, argv, env())
}

func env() []string {
	vars := []string{"NVIM_APPNAME=" + AppName}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "NVIM_APPNAME=") {
			vars = append(vars, kv)
		}
	}

	return vars
}
