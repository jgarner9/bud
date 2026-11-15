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
