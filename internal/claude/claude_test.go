package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fake swaps claude for canned answers to each command line
func fake(t *testing.T, onPath bool, answers map[string]string) {
	t.Helper()
	origLook, origHome, origOut := lookPath, home, output
	dir := t.TempDir()
	lookPath = func(string) (string, error) {
		if onPath {
			return "/bin/claude", nil
		}
		return "", exec.ErrNotFound
	}
	home = func() (string, error) { return dir, nil }
	output = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out, ok := answers[strings.Join(args, " ")]
		if !ok {
			return nil, errors.New("unexpected " + strings.Join(args, " "))
		}
		return []byte(out), nil
	}
	t.Cleanup(func() { lookPath, home, output = origLook, origHome, origOut })
}

func TestCheck(t *testing.T) {
	fake(t, true, map[string]string{
		"--version":          "2.1.311 (Claude Code)\n",
		"auth status --json": `{"loggedIn":true,"authMethod":"claude.ai","email":"a@b"}`,
	})
	s, err := Check()
	if err != nil {
		t.Fatal(err)
	}
	if s != (Status{Bin: "/bin/claude", Version: "2.1.311", LoggedIn: true, Method: "claude.ai"}) {
		t.Fatalf("got %+v", s)
	}
}

func TestCheckLoggedOut(t *testing.T) {
	fake(t, true, map[string]string{
		"--version":          "2.1.311 (Claude Code)",
		"auth status --json": `{"loggedIn":false,"authMethod":"none"}`,
	})
	if s, err := Check(); err != nil || s.LoggedIn || s.Method != "" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestCheckBedrock(t *testing.T) {
	fake(t, true, map[string]string{
		"--version":          "2.1.312 (Claude Code)",
		"auth status --json": `{"loggedIn":true,"authMethod":"third_party","apiProvider":"bedrock"}`,
	})
	if s, err := Check(); err != nil || !s.LoggedIn || s.Method != "Amazon Bedrock" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestCheckElsewhere(t *testing.T) {
	fake(t, true, map[string]string{
		"--version":          "2.1.312 (Claude Code)",
		"auth status --json": `{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}`,
	})
	h, _ := home()
	os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	write := func(env string) {
		os.WriteFile(filepath.Join(h, ".claude", "settings.json"), []byte(`{"env":`+env+`}`), 0o644)
	}
	for _, c := range []struct{ dir, env, want string }{
		{filepath.Join(h, "bud"), `{"CLAUDE_CODE_USE_BEDROCK":"1","AWS_REGION":"us-east-1"}`, "Amazon Bedrock"},
		{filepath.Join(h, "bud"), `{"CLAUDE_CODE_USE_VERTEX":true}`, "Google Vertex AI"},
		{filepath.Join(h, "bud"), `{"CLAUDE_CODE_USE_BEDROCK":"0"}`, ""},
		{filepath.Join(h, "bud"), `{}`, ""},
		// the home dir itself: it would be logged in if the provider worked
		{filepath.Join(h, ".claude") + "/", `{"CLAUDE_CODE_USE_BEDROCK":"1"}`, ""},
		{"", `{"CLAUDE_CODE_USE_BEDROCK":"1"}`, ""},
	} {
		t.Setenv("CLAUDE_CONFIG_DIR", c.dir)
		write(c.env)
		if s, err := Check(); err != nil || s.Elsewhere != c.want {
			t.Errorf("%s %s: got %+v, %v", c.dir, c.env, s, err)
		}
	}
}

func TestCheckMissing(t *testing.T) {
	fake(t, false, nil)
	if s, err := Check(); err != nil || s.Bin != "" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestFindNativeInstall(t *testing.T) {
	fake(t, false, nil)
	h, _ := home()
	bin := filepath.Join(h, ".local", "bin", "claude")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	if got := Find(); got != bin {
		t.Fatalf("Find() = %q, want %q", got, bin)
	}
}
