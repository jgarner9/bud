package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeCore is a Claude dir whose bud-plugins marketplace is a checkout with
// core.md in it, as known_marketplaces.json records either kind of source
func fakeCore(t *testing.T, text string) (claudeDir, checkout string) {
	t.Helper()
	claudeDir, checkout = t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(claudeDir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(claudeDir, "plugins", "known_marketplaces.json"),
		[]byte(`{"bud-plugins": {"source": {"source": "directory", "path": "`+checkout+`"}, "installLocation": "`+checkout+`"}}`), 0o644)
	os.WriteFile(filepath.Join(checkout, "core.md"), []byte(text), 0o644)

	return claudeDir, checkout
}

func TestReadCore(t *testing.T) {
	state := filepath.Join(t.TempDir(), "core-pasted.json")

	// no marketplaces yet, or not this one: nothing to say
	if c, err := ReadCore("bud-plugins/core.md", t.TempDir(), state); err != nil || c.Path != "" {
		t.Fatalf("no checkout: %+v %v", c, err)
	}

	claudeDir, checkout := fakeCore(t, "You are Bud.\n")
	if c, _ := ReadCore("other/core.md", claudeDir, state); c.Path != "" {
		t.Fatalf("another marketplace: %+v", c)
	}
	c, err := ReadCore("bud-plugins/core.md", claudeDir, state)
	if err != nil || c.Path != filepath.Join(checkout, "core.md") || c.Text != "You are Bud.\n" || c.Pasted {
		t.Fatalf("never pasted: %+v %v", c, err)
	}

	if err := MarkCorePasted(state, c); err != nil {
		t.Fatal(err)
	}
	if c, _ = ReadCore("bud-plugins/core.md", claudeDir, state); !c.Pasted {
		t.Fatalf("pasted: %+v", c)
	}

	// core.md moves on: it needs pasting again
	os.WriteFile(filepath.Join(checkout, "core.md"), []byte("You are Bud. Be a bud.\n"), 0o644)
	if c, _ = ReadCore("bud-plugins/core.md", claudeDir, state); c.Pasted {
		t.Fatalf("changed: %+v", c)
	}
}
