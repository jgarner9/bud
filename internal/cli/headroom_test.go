package cli

import (
	"errors"
	"slices"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
)

func TestOfferHeadroom(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	fakeClaude(t, claudeReady)
	installed := func(string) (string, error) { return "/bin/headroom", nil }
	command := func() []string {
		f, _ := config.Load(p.MachineConfig())
		s, err := f.Settings()
		if err != nil {
			t.Fatal(err)
		}
		return s.AI.Claude.Command
	}
	offer := func(pr *prompter) {
		t.Helper()
		f, _ := config.Load(p.MachineConfig())
		s, _ := f.Settings()
		captureStdout(t, func() {
			if err := offerHeadroom(pr, f, s); err != nil {
				t.Fatal(err)
			}
		})
	}

	// not installed: not asked
	lookPath = func(string) (string, error) { return "", errors.New("no") }
	offer(yesPrompter("y\n"))
	if command() != nil {
		t.Fatal("set without headroom")
	}

	// installed, but no, or nobody to ask: left off
	lookPath = installed
	offer(yesPrompter("n\n"))
	offer(nil)
	if command() != nil {
		t.Fatal("set without a yes")
	}

	// Bedrock goes past Headroom's proxy: not offered (JGA-149)
	bedrock := claudeReady
	bedrock.Method = "Amazon Bedrock"
	fakeClaude(t, bedrock)
	offer(yesPrompter("y\n"))
	if command() != nil {
		t.Fatal("offered on Bedrock")
	}
	fakeClaude(t, claudeReady)

	// yes: sessions run through it
	offer(yesPrompter("y\n"))
	if !slices.Equal(command(), headroomCommand) {
		t.Fatalf("command is %v", command())
	}

	// a command you set yourself is never replaced, or asked about
	f, _ := config.Load(p.MachineConfig())
	f.Set("ai.claude.command", []string{"mywrap", "--"})
	f.Save()
	offer(yesPrompter(""))
	if !slices.Equal(command(), []string{"mywrap", "--"}) {
		t.Fatalf("replaced: %v", command())
	}
}
