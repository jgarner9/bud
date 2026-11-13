package cli

import (
	"bytes"
	"encoding/json"
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

	// without bud-ide's chat agent: plain Claude Code, saying how to get it
	var stderr bytes.Buffer
	if err := chat(&Globals{stderr: &stderr}, nil, false); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "bud", "chat")
	if dir != want || !slices.Equal(argv, []string{bin}) {
		t.Fatalf("ran %q in %s, want plain claude in %s", argv, dir, want)
	}
	if !strings.Contains(stderr.String(), "bud kit sync") {
		t.Fatalf("no hint to sync the kit: %q", stderr.String())
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("chat dir not made: %v", err)
	}
	// not a project: bud-ide's hook mustn't add its notes on working inside bud
	if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "BUD_BIN=") }) {
		t.Fatal("BUD_BIN passed on")
	}

	// with it installed, from any marketplace: the agent, and --resume picks
	plugin := filepath.Join(t.TempDir(), "bud-ide")
	os.MkdirAll(filepath.Join(plugin, "agents"), 0o755)
	os.WriteFile(filepath.Join(plugin, "agents", "chat.md"), []byte("---\nname: chat\n---\n"), 0o644)
	installed, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"bud-ide@clod": []map[string]string{{"scope": "user", "installPath": plugin}},
	}})
	os.MkdirAll(filepath.Join(cc, "plugins"), 0o755)
	os.WriteFile(filepath.Join(cc, "plugins", "installed_plugins.json"), installed, 0o644)
	stderr.Reset()
	if err := chat(&Globals{stderr: &stderr}, nil, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{bin, "--agent", "bud-ide:chat", "--resume"}) || stderr.Len() > 0 {
		t.Fatalf("ran %q (stderr %q)", argv, stderr.String())
	}

	if err := chat(&Globals{}, []string{"x"}, false); err == nil {
		t.Fatal("args accepted")
	}
}
