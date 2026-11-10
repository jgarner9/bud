package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
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

	// resume = "continue" in a shared ~/.claude passes no --continue (JGA-231):
	// the newest conversation there may be one you started by hand
	state := os.Getenv("XDG_STATE_HOME")
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	cfgFile := filepath.Join(state, "bud", "config.json")
	os.WriteFile(cfgFile, []byte(`{"ai": {"claude": {"resume": "continue"}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{bin}) {
		t.Fatalf("resume continue in a shared dir: argv %q", argv)
	}

	// ai.claude.command wraps it
	wrapper := filepath.Join(filepath.Dir(bin), "headroom")
	os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(cfgFile, []byte(`{"ai": {"claude": {"resume": "continue", "command": ["headroom", "wrap", "claude", "--"]}}}`), 0o644)
	if err := agent(&Globals{}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{wrapper, "wrap", "claude", "--"}) {
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

// resume "continue" resumes the editor's saved conversation, and --continue
// only happens in bud's own Claude Code config dir (JGA-231)
func TestResumeFlags(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	root, _ := filepath.EvalSymlinks(t.TempDir())
	cont := config.Defaults()
	cont.AI.Claude.Resume = "continue"

	write := func(dir, name string) {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}

	// the default starts fresh
	if got := resumeFlags(config.Defaults(), root); got != nil {
		t.Fatalf("new: %q", got)
	}

	// shared ~/.claude with a hand-started conversation: nothing to continue
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	write(claudeProjectDir(root), "by-hand.jsonl")
	if got := resumeFlags(cont, root); got != nil {
		t.Fatalf("shared dir passed %q", got)
	}

	// the editor's saved conversation is resumed by id, shared or not
	ids, _ := json.Marshal(map[string]string{root: "saved-id"})
	os.MkdirAll(filepath.Join(state, "bud"), 0o755)
	os.WriteFile(filepath.Join(state, "bud", "claude-sessions.json"), ids, 0o644)
	if got := resumeFlags(cont, root); got != nil {
		t.Fatalf("a saved id whose conversation is gone: %q", got)
	}
	write(claudeProjectDir(root), "saved-id.jsonl")
	if got := resumeFlags(cont, root); !slices.Equal(got, []string{"--resume", "saved-id"}) {
		t.Fatalf("saved: %q", got)
	}
	os.Remove(filepath.Join(state, "bud", "claude-sessions.json"))

	// bud's own config dir: --continue, when there is a conversation
	p, _ := paths.Resolve()
	t.Setenv("CLAUDE_CONFIG_DIR", p.ClaudeConfig())
	if got := resumeFlags(cont, root); got != nil {
		t.Fatalf("own dir with no conversation: %q", got)
	}
	write(claudeProjectDir(root), "any.jsonl")
	if got := resumeFlags(cont, root); !slices.Equal(got, []string{"--continue"}) {
		t.Fatalf("own dir: %q", got)
	}

	// no home directory and no config dir set: nowhere to look, so nothing to
	// continue (not a lookup relative to the working directory)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	if dir := claudeProjectDir(root); dir != "" {
		t.Fatalf("no home: %q", dir)
	}
	if anyConversation(root) {
		t.Fatal("no home but a conversation was found")
	}
}
