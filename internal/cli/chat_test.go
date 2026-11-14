package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestChatRunsClaudeCodeAsTheChatAgentOutsideAnyProject(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	claudeBackend(t)
	t.Setenv("HOME", t.TempDir())
	cc := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cc)
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin))
	t.Setenv("BUD_BIN", "/some/bud")

	var dir string
	var argv, env []string
	orig := execClaude
	execClaude = func(d string, a, e []string) error { dir, argv, env = d, a, e; return nil }
	t.Cleanup(func() { execClaude = orig })

	// the embedded plugin's chat agent, for this session only (D142)
	var stderr bytes.Buffer
	if err := chat(&Globals{stderr: &stderr}, nil, false); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "bud", "chat")
	withPlugin := append([]string{bin}, wantPlugin(t)...)
	if dir != want || !slices.Equal(argv, append(slices.Clone(withPlugin), "--agent", "bud-ide:chat")) {
		t.Fatalf("ran %q in %s, want the chat agent in %s", argv, dir, want)
	}
	if stderr.Len() > 0 {
		t.Fatalf("stderr: %q", stderr.String())
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("chat dir not made: %v", err)
	}
	// not a project: bud-ide's hook mustn't add its notes on working inside bud
	if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "BUD_BIN=") }) {
		t.Fatal("BUD_BIN passed on")
	}

	// --resume picks an earlier chat
	if err := chat(&Globals{stderr: &stderr}, nil, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, append(slices.Clone(withPlugin), "--agent", "bud-ide:chat", "--resume")) {
		t.Fatalf("ran %q", argv)
	}

	if err := chat(&Globals{}, []string{"x"}, false); err == nil {
		t.Fatal("args accepted")
	}
}
