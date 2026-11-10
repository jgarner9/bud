package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Agent())
}

// Agent opens Claude Code's own TUI in a project from the shell, outside
// Neovim, on the same conversation the editor's session uses (BASE.md 4.2)
func Agent() *Command {
	return &Command{
		Name:  "agent",
		Usage: "Opens Claude Code in a project, outside the editor: agent [path] (default: the current dir)",
		Run:   agent,
	}
}

// swappable in tests
var execClaude = func(dir string, argv, env []string) error {
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return syscall.Exec(argv[0], argv, env)
}

// agentEnv is the environment Claude Code's TUI gets: this one, with
// BUD_BIN pointing at this bud, as nvim's sessions have it, so bud-ide's
// SessionStart hook knows the session is bud's and adds its notes on
// working inside bud (D77)
func agentEnv() []string {
	env := os.Environ()
	self, err := os.Executable()
	if err != nil {
		return env
	}
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "BUD_BIN=") })

	return append(env, "BUD_BIN="+self)
}

func agent(g *Globals, args []string) error {
	if len(args) > 1 {
		return usageErrorf("agent takes at most one path")
	}
	ws, err := resolveWorkspace(args)
	if err != nil {
		return err
	}
	bin := claude.Find()
	if bin == "" {
		return errors.New("Claude Code isn't installed: run bud claude setup")
	}

	s := machineSettings()
	argv, err := tuiArgv(s, bin, resumeFlags(s, ws.Root))
	if err != nil {
		return err
	}
	g.Logf("running %q in %s", argv, ws.Root)

	return execClaude(ws.Root, argv, agentEnv())
}

// resumeFlags picks the conversation the way the editor does (resume_args in
// bud.ai.claude): with resume "continue", the project's saved one, else the
// newest in the dir, but only in bud's own Claude Code config dir. In a shared
// ~/.claude the newest may be one you started by hand, an issue, gen or chat
// conversation, so it starts a new one instead (JGA-231)
func resumeFlags(s config.Settings, root string) []string {
	if s.AI.Claude.Resume != "continue" {
		return nil
	}
	if id := savedConversation(root); id != "" {
		return []string{"--resume", id}
	}
	if budClaudeDir() && anyConversation(root) {
		return []string{"--continue"}
	}

	return nil
}

// claudeProjectDir is where Claude Code keeps the conversations run in root:
// the real path with everything but letters and digits turned into -. It is ""
// when there is no home directory to look in
func claudeProjectDir(root string) string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}

	return filepath.Join(dir, "projects", nonAlnum.ReplaceAllString(root, "-"))
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// anyConversation says whether Claude Code has a conversation to continue in root
func anyConversation(root string) bool {
	dir := claudeProjectDir(root)
	if dir == "" {
		return false
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	return len(files) > 0
}

// savedConversation is the conversation the editor saved for root in
// claude-sessions.json, when Claude Code still has it, else ""
func savedConversation(root string) string {
	p, err := paths.Resolve()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(p.State, "claude-sessions.json"))
	if err != nil {
		return ""
	}
	var ids map[string]string
	if json.Unmarshal(data, &ids) != nil {
		return ""
	}
	id := ids[root]
	if id == "" {
		return ""
	}
	dir := claudeProjectDir(root)
	if dir == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, id+".jsonl")); err != nil {
		return ""
	}

	return id
}

// machineSettings is config.json's settings, the defaults when it's missing
// or broken
func machineSettings() config.Settings {
	s := config.Defaults()
	if p, err := paths.Resolve(); err == nil {
		if f, err := config.Load(p.MachineConfig()); err == nil {
			if loaded, err := f.Settings(); err == nil {
				s = loaded
			}
		}
	}
	return s
}

// tuiArgv is the command line for Claude Code's TUI, as nvim builds it: bin,
// or ai.claude.command in its place, then flags, then ai.claude.args
func tuiArgv(s config.Settings, bin string, flags []string) ([]string, error) {
	argv := []string{bin}
	if len(s.AI.Claude.Command) > 0 {
		wrapper, err := exec.LookPath(s.AI.Claude.Command[0])
		if err != nil {
			return nil, fmt.Errorf("ai.claude.command: %w", err)
		}
		argv = append([]string{wrapper}, s.AI.Claude.Command[1:]...)
	}
	argv = append(argv, flags...)

	return append(argv, s.AI.Claude.Args...), nil
}
