package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// PluginFile is rel inside the installed plugin called name, from any
// marketplace, read from Claude Code's plugins/installed_plugins.json; ""
// when the plugin isn't installed or its copy has no such file (an older one)
func PluginFile(name, rel string) string {
	data, err := os.ReadFile(filepath.Join(ConfigDir(), "plugins", "installed_plugins.json"))
	if err != nil {
		return ""
	}
	var installed struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &installed) != nil {
		return ""
	}
	for id, installs := range installed.Plugins {
		if plugin, _, ok := strings.Cut(id, "@"); !ok || plugin != name {
			continue
		}
		for _, in := range installs {
			if in.InstallPath == "" {
				continue
			}
			path := filepath.Join(in.InstallPath, rel)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	return ""
}
