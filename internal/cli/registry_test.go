package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryCheckPassesTheShippedRegistry(t *testing.T) {
	out := captureStdout(t, func() {
		if err := registryCheck([]string{filepath.Join("..", "..", "content", "env", "registry.jsonc")}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "all 9 roles filled") {
		t.Fatalf("got %q", out)
	}
}

func TestRegistryCheckFailsAnOverlayThatBreaksIt(t *testing.T) {
	overlay := filepath.Join(t.TempDir(), "overlay.jsonc")
	// the classifier sees private data, so a cloud model there must fail
	os.WriteFile(overlay, []byte(`{"roles": {"classifier": {"model": "kimi-k3"}}}`), 0o644)
	err := registryCheck([]string{filepath.Join("..", "..", "content", "env", "registry.jsonc"), overlay})
	if err == nil || !strings.Contains(err.Error(), "classifier sees private data") || !strings.Contains(err.Error(), "over it") {
		t.Fatalf("got %v", err)
	}
}

func TestRegistryCheckNeedsAFile(t *testing.T) {
	var u *UsageError
	if err := registryCheck(nil); !errors.As(err, &u) {
		t.Fatalf("want a usage error, got %v", err)
	}
}
