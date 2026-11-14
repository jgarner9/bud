package claude

import (
	"os"
	"path/filepath"
)

// ConfigDir is the Claude Code config dir in use: CLAUDE_CONFIG_DIR, else
// ~/.claude
func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := home()
	return filepath.Join(h, ".claude")
}
