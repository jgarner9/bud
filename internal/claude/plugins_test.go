package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginFileLooksInTheInstalledCopy(t *testing.T) {
	cc := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cc)
	if got := PluginFile("bud-ide", "agents/chat.md"); got != "" {
		t.Fatalf("no plugins: got %q", got)
	}

	plugin := t.TempDir()
	os.MkdirAll(filepath.Join(plugin, "agents"), 0o755)
	os.WriteFile(filepath.Join(plugin, "agents", "chat.md"), nil, 0o644)
	os.MkdirAll(filepath.Join(cc, "plugins"), 0o755)
	os.WriteFile(filepath.Join(cc, "plugins", "installed_plugins.json"), []byte(`{"version": 2, "plugins": {
		"bud@bud-plugins": [{"installPath": "`+plugin+`"}],
		"bud-ide@bud-plugins": [{"installPath": "/gone"}, {"installPath": "`+plugin+`"}]
	}}`), 0o644)

	if got, want := PluginFile("bud-ide", "agents/chat.md"), filepath.Join(plugin, "agents", "chat.md"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := PluginFile("bud-ide", "agents/nope.md"); got != "" {
		t.Fatalf("an older copy without the file: got %q", got)
	}
	if got := PluginFile("bud-id", "agents/chat.md"); got != "" {
		t.Fatalf("matched a prefix of the name: %q", got)
	}
}
