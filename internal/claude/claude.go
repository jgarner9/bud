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
	Method   string // how it's logged in: "claude.ai", "api key", "Amazon Bedrock", ...
	// Elsewhere is the cloud provider ~/.claude/settings.json sets up when
	// the config dir in use (CLAUDE_CONFIG_DIR) is another one and isn't
	// logged in: claude.ai's login is the wrong fix then
	Elsewhere string
}

// providerVars are the settings that put Claude Code on a cloud provider,
// with the name each shows as
var providerVars = []struct{ env, api, name string }{
	{"CLAUDE_CODE_USE_BEDROCK", "bedrock", "Amazon Bedrock"},
	{"CLAUDE_CODE_USE_VERTEX", "vertex", "Google Vertex AI"},
	{"CLAUDE_CODE_USE_FOUNDRY", "foundry", "Microsoft Foundry"},
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
		LoggedIn    bool   `json:"loggedIn"`
		AuthMethod  string `json:"authMethod"`
		APIProvider string `json:"apiProvider"`
	}
	if err := json.Unmarshal(out, &auth); err != nil {
		return s, fmt.Errorf("%s auth status: %w", s.Bin, err)
	}
	s.LoggedIn = auth.LoggedIn
	if auth.LoggedIn {
		s.Method = strings.ReplaceAll(auth.AuthMethod, "_", " ")
		// a cloud provider reports third_party; name the provider instead
		if auth.APIProvider != "" && auth.APIProvider != "firstParty" {
			s.Method = auth.APIProvider
			for _, p := range providerVars {
				if p.api == auth.APIProvider {
					s.Method = p.name
				}
			}
		}
	} else {
		s.Elsewhere = homeProvider()
	}

	return s, nil
}

// homeProvider is the cloud provider ~/.claude/settings.json's env turns on,
// when CLAUDE_CONFIG_DIR points somewhere else; "" otherwise
func homeProvider() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	h, err := home()
	if dir == "" || err != nil || filepath.Clean(dir) == filepath.Join(h, ".claude") {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(h, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var settings struct {
		Env map[string]any `json:"env"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	for _, p := range providerVars {
		switch v := settings.Env[p.env].(type) {
		case bool:
			if v {
				return p.name
			}
		case string:
			// the values Claude Code takes as on
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "true", "yes", "on":
				return p.name
			}
		}
	}

	return ""
}

// InstallCommand is the command line that installs Claude Code
func InstallCommand() []string {
	return []string{"/bin/sh", "-c", InstallScript}
}

// LoginCommand is Claude Code's own login, run on the terminal
func LoginCommand(bin string) []string {
	return []string{bin, "auth", "login"}
}
