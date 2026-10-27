// Package secrets finds the secrets kit entries name (D15): an env var
// first, then the OS keychain (Keychain on macOS through security, the
// Secret Service on Linux through secret-tool), under the service "bud".
// Values never go on a command line, where other users could see them.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Service is what bud's keychain items are filed under
const Service = "bud"

// swappable in tests: runs a keychain tool with stdin, returning stdout
var (
	goos = runtime.GOOS
	run  = func(stdin string, name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.Output()
		return string(out), err
	}
)

// ErrNoKeychain is returned when there's no keychain tool to store with
var ErrNoKeychain = errors.New("no keychain: on Linux, install secret-tool (libsecret)")

// Get returns name's value: the env var, else the keychain's, else false
func Get(name string) (string, bool) {
	if v := os.Getenv(name); v != "" {
		return v, true
	}
	v, err := keychainGet(name)
	return v, err == nil && v != ""
}

// Has says whether name is set anywhere Get looks
func Has(name string) bool {
	_, ok := Get(name)
	return ok
}

// FromEnv says whether name is set in the environment, rather than only in
// the keychain, which is when a process needs it handed over
func FromEnv(name string) bool {
	return os.Getenv(name) != ""
}

func keychainGet(name string) (string, error) {
	switch goos {
	case "darwin":
		out, err := run("", "security", "find-generic-password", "-s", Service, "-a", name, "-w")
		return strings.TrimRight(out, "\n"), err
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return "", ErrNoKeychain
		}
		out, err := run("", "secret-tool", "lookup", "service", Service, "name", name)
		return strings.TrimRight(out, "\n"), err
	}

	return "", ErrNoKeychain
}

// Set stores value under name in the keychain, replacing what was there
func Set(name, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return errors.New("a secret can't hold a line break")
	}
	switch goos {
	case "darwin":
		// security -i reads the command from stdin, keeping the value off argv
		cmd := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", Service, strconv.Quote(name), strconv.Quote(value))
		_, err := run(cmd, "security", "-i")
		return err
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return ErrNoKeychain
		}
		_, err := run(value, "secret-tool", "store", "--label", "bud: "+name, "service", Service, "name", name)
		return err
	}

	return ErrNoKeychain
}

// Delete removes name from the keychain
func Delete(name string) error {
	switch goos {
	case "darwin":
		_, err := run("", "security", "delete-generic-password", "-s", Service, "-a", name)
		return err
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return ErrNoKeychain
		}
		_, err := run("", "secret-tool", "clear", "service", Service, "name", name)
		return err
	}

	return ErrNoKeychain
}
