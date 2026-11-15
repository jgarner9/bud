package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentPackWritesTheBundleAndManifest(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "skills", "learn"), 0o755)
	os.WriteFile(filepath.Join(src, "skills", "learn", "SKILL.md"), []byte("x"), 0o644)
	out := filepath.Join(t.TempDir(), "dist")

	stdout := captureStdout(t, func() {
		if err := contentPack([]string{src}, "2026.11.15.1", out); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, "packed 1 files") {
		t.Fatalf("got %q", stdout)
	}
	for _, name := range []string{"content-2026.11.15.1.tar.gz", "content.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Error(err)
		}
	}
}

func TestContentPackNeedsAVersionAndADir(t *testing.T) {
	var u *UsageError
	if err := contentPack(nil, "2026.11.15.1", t.TempDir()); !errors.As(err, &u) {
		t.Fatalf("no dir: %v", err)
	}
	if err := contentPack([]string{t.TempDir()}, "", t.TempDir()); !errors.As(err, &u) {
		t.Fatalf("no version: %v", err)
	}
}

func TestContentChannelStartsANewChannelFile(t *testing.T) {
	out := t.TempDir()
	stdout := captureStdout(t, func() {
		if err := contentChannel(nil, "", true, "edge=2026.11.15.1", out); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, "edge: nothing -> 2026.11.15.1") {
		t.Fatalf("got %q", stdout)
	}
	b, err := os.ReadFile(filepath.Join(out, "channel.json"))
	if err != nil || !strings.Contains(string(b), `"content": "2026.11.15.1"`) {
		t.Fatalf("channel.json: %s, %v", b, err)
	}
}

func TestContentChannelRefusesAnUnverifiedChannelFile(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "channel.json")
	os.WriteFile(from, []byte(`{"channels":{"stable":{"content":"2026.11.14.1"}}}`), 0o644)
	if err := contentChannel(nil, from, false, "edge=2026.11.15.1", dir); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("no .sig: %v", err)
	}
	os.WriteFile(from+".sig", []byte("not a signature"), 0o644)
	if err := contentChannel(nil, from, false, "edge=2026.11.15.1", dir); err == nil {
		t.Fatal("a bad signature verified")
	}
}

func TestContentChannelUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		args  []string
		from  string
		fresh bool
		set   string
	}{
		"no --set":           {nil, "", true, ""},
		"--set without =":    {nil, "", true, "edge"},
		"neither source":     {nil, "", false, "edge=2026.11.15.1"},
		"both sources":       {nil, "channel.json", true, "edge=2026.11.15.1"},
		"a stray positional": {[]string{"x"}, "", true, "edge=2026.11.15.1"},
	} {
		t.Run(name, func(t *testing.T) {
			var u *UsageError
			if err := contentChannel(tc.args, tc.from, tc.fresh, tc.set, t.TempDir()); !errors.As(err, &u) {
				t.Fatalf("want a usage error, got %v", err)
			}
		})
	}
}
