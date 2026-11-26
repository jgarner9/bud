package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// bud chat runs OpenCode's TUI in a dir of its own, outside any project
func TestChatRunsOpenCodeOutsideAnyProject(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	bin := filepath.Join(t.TempDir(), "opencode")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin))

	var dir string
	var argv []string
	orig := execClaude
	execClaude = func(d string, a, e []string) error { dir, argv = d, a; return nil }
	t.Cleanup(func() { execClaude = orig })

	if err := chat(&Globals{}, nil, false); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "bud", "chat")
	if dir != want || !slices.Equal(argv, []string{bin, want}) {
		t.Fatalf("ran %q in %s, want OpenCode in %s", argv, dir, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("chat dir not made: %v", err)
	}

	if err := chat(&Globals{}, nil, true); err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Errorf("chat --resume: %v", err)
	}
	if err := chat(&Globals{}, []string{"x"}, false); err == nil {
		t.Fatal("args accepted")
	}

	t.Setenv("PATH", t.TempDir())
	if err := chat(&Globals{}, nil, false); err == nil || !strings.Contains(err.Error(), "opencode isn't installed: run bud ai setup") {
		t.Errorf("missing opencode: %v", err)
	}
}
