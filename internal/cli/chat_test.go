package cli

import (
	"os"
	"testing"
)

func TestChat(t *testing.T) {
	t.Setenv("BUD_OPEN", "")
	t.Setenv("BUD_CHAT_RESUME", "")
	var root string
	var project bool
	orig := launchNvim
	launchNvim = func(r string, _ []string, _ string, p bool) error { root, project = r, p; return nil }
	t.Cleanup(func() { launchNvim = orig })

	if err := chat(&Globals{}, nil, "roadmap"); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if root != home || project || os.Getenv("BUD_OPEN") != "chat" || os.Getenv("BUD_CHAT_RESUME") != "roadmap" {
		t.Fatalf("launched in %s (project %v), BUD_OPEN %q, resume %q", root, project, os.Getenv("BUD_OPEN"), os.Getenv("BUD_CHAT_RESUME"))
	}
	if err := chat(&Globals{}, []string{"x"}, ""); err == nil {
		t.Fatal("args accepted")
	}
}
