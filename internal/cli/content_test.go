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

func TestContentChannelWritesBothChannels(t *testing.T) {
	out := t.TempDir()
	stdout := captureStdout(t, func() {
		if err := contentChannel(nil, "2026.11.15.2", "2026.11.15.1", out); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, "edge 2026.11.15.2, stable 2026.11.15.1") {
		t.Fatalf("got %q", stdout)
	}
	b, err := os.ReadFile(filepath.Join(out, "channel.json"))
	if err != nil || !strings.Contains(string(b), `"content": "2026.11.15.2"`) || !strings.Contains(string(b), `"stable"`) {
		t.Fatalf("channel.json: %s, %v", b, err)
	}
	if err := contentChannel(nil, "2026.11.15.1", "2026.11.15.2", out); err == nil {
		t.Fatal("stable ahead of edge was written")
	}
}

func TestContentChannelUsage(t *testing.T) {
	var u *UsageError
	if err := contentChannel(nil, "", "", t.TempDir()); !errors.As(err, &u) {
		t.Fatalf("no --edge: %v", err)
	}
	if err := contentChannel([]string{"x"}, "2026.11.15.1", "", t.TempDir()); !errors.As(err, &u) {
		t.Fatalf("a stray positional: %v", err)
	}
}

func TestContentOfferedRefusesAnUnverifiedChannelFile(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "channel.json")
	os.WriteFile(from, []byte(`{"channels":{"edge":{"content":"2026.11.15.1"}}}`), 0o644)
	if err := contentOffered([]string{from}, "edge"); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("no .sig: %v", err)
	}
	os.WriteFile(from+".sig", []byte("not a signature"), 0o644)
	if err := contentOffered([]string{from}, "edge"); err == nil {
		t.Fatal("a bad signature verified")
	}
	var u *UsageError
	if err := contentOffered(nil, "edge"); !errors.As(err, &u) {
		t.Fatalf("no file: %v", err)
	}
}
