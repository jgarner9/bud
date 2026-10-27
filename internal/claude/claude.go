// Package claude finds and checks the Claude Code CLI, the only AI harness
// bud drives (D26): whether it's installed, which version, and whether it's
// logged in. bud never stores a key; logging in is Claude Code's own
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Status is what Check found
type Status struct {
	Bin      string // "" when Claude Code isn't installed
	Version  string
	LoggedIn bool
	Method   string // how it's logged in: "claude.ai", "api key", ...
}

// InstallScript is Anthropic's native installer, which puts claude in
// ~/.local/bin and keeps it updated itself
const InstallScript = "curl -fsSL https://claude.ai/install.sh | bash"

// timeout bounds each claude call, so a hung one never holds up doctor
const timeout = 10 * time.Second

// swappable in tests
var (
	lookPath = exec.LookPath
	home     = os.UserHomeDir
	output   = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, bin, args...).Output()
	}
)

// Find returns the claude binary: on PATH, else where the native installer
// puts it, which a fresh install may not have on PATH yet. "" when neither
func Find() string {
	if bin, err := lookPath("claude"); err == nil {
		return bin
	}
	if h, err := home(); err == nil {
		bin := filepath.Join(h, ".local", "bin", "claude")
		if info, err := os.Stat(bin); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return bin
		}
	}

	return ""
}

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Check finds claude and asks it its version and whether it's logged in
func Check() (Status, error) {
	s := Status{Bin: Find()}
	if s.Bin == "" {
		return s, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := output(ctx, s.Bin, "--version")
	if err != nil {
		return s, fmt.Errorf("%s --version: %w", s.Bin, err)
	}
	s.Version = versionPattern.FindString(string(out))

	out, err = output(ctx, s.Bin, "auth", "status", "--json")
	if err != nil {
		var exit *exec.ExitError
		// logged out exits non-zero on some versions, still printing the JSON
		if !errors.As(err, &exit) || len(out) == 0 {
			return s, fmt.Errorf("%s auth status: %w", s.Bin, err)
		}
	}
	var auth struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.Unmarshal(out, &auth); err != nil {
		return s, fmt.Errorf("%s auth status: %w", s.Bin, err)
	}
	s.LoggedIn = auth.LoggedIn
	if auth.LoggedIn {
		s.Method = strings.ReplaceAll(auth.AuthMethod, "_", " ")
	}

	return s, nil
}

// InstallCommand is the command line that installs Claude Code
func InstallCommand() []string {
	return []string{"/bin/sh", "-c", InstallScript}
}

// LoginCommand is Claude Code's own login, run on the terminal
func LoginCommand(bin string) []string {
	return []string{bin, "auth", "login"}
}
