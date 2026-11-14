package cli

import (
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
)

func TestClaudeConfigDir(t *testing.T) {
	p := &paths.Paths{Data: "/data"}
	s := config.Defaults()
	// bud shares ~/.claude unless told to keep its own (D77)
	if got := claudeConfigDir(p, s); got != "" {
		t.Errorf("default %q", got)
	}
	s.AI.Claude.Config = "bud"
	if got := claudeConfigDir(p, s); got != "/data/claude" {
		t.Errorf("bud %q", got)
	}
}
