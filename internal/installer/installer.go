// Package installer makes sure the system tooling bud depends on is present.
// Mason handles language tooling inside Neovim; this covers everything else
package installer

import (
	"fmt"
	"os/exec"
)

type Dep struct {
	Name    string
	Bin     string
	Install func() error
}

// Deps lists every system dependency in the order it should be installed.
// nvim and git come first since the config sync and plugin restore need them
var Deps = []Dep{
	{Name: "neovim", Bin: "nvim", Install: installNeovim},
	{Name: "git", Bin: "git", Install: installGit},
	{Name: "ripgrep", Bin: "rg", Install: installRipgrep},
	{Name: "fd", Bin: "fd", Install: installFd},
	{Name: "node", Bin: "node", Install: installNode},
}

func (d Dep) Installed() bool {
	_, err := exec.LookPath(d.Bin)
	return err == nil
}

// Missing returns the deps whose binary isn't on PATH
func Missing() []Dep {
	var missing []Dep
	for _, dep := range Deps {
		if !dep.Installed() {
			missing = append(missing, dep)
		}
	}

	return missing
}

// EnsureAll installs every missing dep, stopping at the first failure
func EnsureAll() error {
	for _, dep := range Missing() {
		if err := dep.Install(); err != nil {
			return fmt.Errorf("installing %s: %w", dep.Name, err)
		}
	}

	return nil
}

func installNeovim() error {
	fmt.Println("installing neovim")
	return nil
}

func installGit() error {
	fmt.Println("installing git")
	return nil
}

func installRipgrep() error {
	fmt.Println("installing ripgrep")
	return nil
}

func installFd() error {
	fmt.Println("installing fd")
	return nil
}

func installNode() error {
	fmt.Println("installing node")
	return nil
}
