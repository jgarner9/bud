package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/improve"
	"jgarner.dev/bud/internal/paths"
)

func TestGithubRepo(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:you/bud-overlay.git":       "you/bud-overlay",
		"https://github.com/you/bud-overlay":       "you/bud-overlay",
		"https://github.com/you/bud-overlay.git/":  "you/bud-overlay",
		"ssh://git@github.com/you/bud-overlay.git": "you/bud-overlay",
		"git@gitlab.com:you/bud-overlay.git":       "",
		"":                                         "",
	} {
		if got := githubRepo(in); got != want {
			t.Errorf("githubRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHermesPinReadsThePlugin(t *testing.T) {
	pin, err := hermesPin()
	if err != nil || pin == "" {
		t.Fatalf("pin %q, %v", pin, err)
	}
}

func TestImproveAddQueuesOnce(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("BUD_LEAK_PATTERNS", "")
	file := filepath.Join(t.TempDir(), "p.json")
	body := `{"title": "Teach the mail skill to skip newsletters", "evidence": "Asked by hand three times", "target": "public",
	  "files": [{"path": "content/skills/mail/SKILL.md", "before": "old", "after": "new"}],
	  "criteria": [{"metric": "doctor.fail"}]}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := improveAdd([]string{file}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	st, err := improve.Load(p.ImproveState())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Proposals) != 1 || st.Proposals[0].Source != "hand" || st.Proposals[0].Status != improve.New {
		t.Fatalf("queue %+v", st.Proposals)
	}

	if err := os.WriteFile(file, []byte(strings.Replace(body, "content/skills", "internal", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := improveAdd([]string{file}); err == nil || !strings.Contains(err.Error(), "only content/") {
		t.Fatalf("a public change outside content/: %v", err)
	}
}
