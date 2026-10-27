package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAgent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin))

	var dir string
	var argv []string
	orig := execClaude
	execClaude = func(d string, a []string) error { dir, argv = d, a; return nil }
	t.Cleanup(func() { execClaude = orig })

	project := t.TempDir()
	os.Mkdir(filepath.Join(project, ".git"), 0o755)
	file := filepath.Join(project, "main.go")
	os.WriteFile(file, nil, 0o644)

	if err := agent(&Globals{}, []string{file}); err != nil {
		t.Fatal(err)
	}
	if dir != project || !slices.Equal(argv, []string{bin, "--continue"}) {
		t.Fatalf("ran %q in %s", argv, dir)
	}

	// ai.claude.command wraps it
	wrapper := filepath.Join(filepath.Dir(bin), "headroom")
	os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755)
	state := os.Getenv("XDG_STATE_HOME")
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	os.WriteFile(filepath.Join(state, "bud", "config.json"), []byte(`{"ai": {"claude": {"command": ["headroom", "wrap", "claude", "--"]}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{wrapper, "wrap", "claude", "--", "--continue"}) {
		t.Fatalf("wrapped argv %q", argv)
	}
	os.Remove(filepath.Join(state, "bud", "config.json"))

	if err := agent(&Globals{}, []string{project, file}); err == nil {
		t.Fatal("two paths accepted")
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := agent(&Globals{}, nil); err == nil || !strings.Contains(err.Error(), "bud claude setup") {
		t.Fatalf("no claude: %v", err)
	}
}
