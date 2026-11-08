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

	t.Setenv("BUD_BIN", "/an/older/bud")
	var dir string
	var argv, env []string
	orig := execClaude
	execClaude = func(d string, a, e []string) error { dir, argv, env = d, a, e; return nil }
	t.Cleanup(func() { execClaude = orig })

	project := t.TempDir()
	os.Mkdir(filepath.Join(project, ".git"), 0o755)
	file := filepath.Join(project, "main.go")
	os.WriteFile(file, nil, 0o644)

	if err := agent(&Globals{}, []string{file}); err != nil {
		t.Fatal(err)
	}
	// a fresh conversation unless ai.claude.resume says continue (D106)
	if dir != project || !slices.Equal(argv, []string{bin}) {
		t.Fatalf("ran %q in %s", argv, dir)
	}
	// bud-ide's hook knows it's bud's session by BUD_BIN, which is this bud
	self, _ := os.Executable()
	if got := slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return !strings.HasPrefix(kv, "BUD_BIN=") }); !slices.Equal(got, []string{"BUD_BIN=" + self}) {
		t.Fatalf("BUD_BIN in the env: %q", got)
	}

	// resume = "continue" carries on the newest conversation in the directory
	state := os.Getenv("XDG_STATE_HOME")
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	cfgFile := filepath.Join(state, "bud", "config.json")
	os.WriteFile(cfgFile, []byte(`{"ai": {"claude": {"resume": "continue"}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{bin, "--continue"}) {
		t.Fatalf("resume continue: argv %q", argv)
	}

	// ai.claude.command wraps it
	wrapper := filepath.Join(filepath.Dir(bin), "headroom")
	os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(cfgFile, []byte(`{"ai": {"claude": {"resume": "continue", "command": ["headroom", "wrap", "claude", "--"]}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{wrapper, "wrap", "claude", "--", "--continue"}) {
		t.Fatalf("wrapped argv %q", argv)
	}
	os.Remove(cfgFile)

	if err := agent(&Globals{}, []string{project, file}); err == nil {
		t.Fatal("two paths accepted")
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := agent(&Globals{}, nil); err == nil || !strings.Contains(err.Error(), "bud claude setup") {
		t.Fatalf("no claude: %v", err)
	}
}
