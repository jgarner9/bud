package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/syncer"
)

// the command reads device, overlay.repo and content.mirror from config,
// lets BUD_OVERLAY win, and --rollback picks the rollback
func TestSyncPassesConfigToTheSyncAndRollbackPicksIt(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("BUD_OVERLAY", "git@example.com:you/overlay.git")
	cfg := `{"device": "dev-machine", "overlay": {"repo": "ignored"}, "content": {"mirror": "http://mirror.invalid"}}`
	if err := os.MkdirAll(filepath.Join(state, "bud"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "bud", "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []string
	record := func(name string) func(context.Context, syncer.Options) (*syncer.Report, error) {
		return func(_ context.Context, o syncer.Options) (*syncer.Report, error) {
			got = append(got, name+" "+o.Device+" "+o.Overlay+" "+o.Mirror)
			return &syncer.Report{Content: "2026.11.15.1"}, nil
		}
	}
	origRun, origRollback := syncRun, syncRollback
	syncRun, syncRollback = record("run"), record("rollback")
	t.Cleanup(func() { syncRun, syncRollback = origRun, origRollback })

	g := &Globals{}
	if err := runSync(g, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if err := runSync(g, nil, false, true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"run dev-machine git@example.com:you/overlay.git http://mirror.invalid",
		"rollback dev-machine git@example.com:you/overlay.git http://mirror.invalid",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if err := runSync(g, []string{"extra"}, false, false); err == nil {
		t.Fatal("sync took an argument")
	}
}

func TestSyncOutputListsChangesThenSkips(t *testing.T) {
	rep := &syncer.Report{
		Content: "2026.11.15.2",
		Notes:   []string{"a note"},
		Changes: []syncer.Change{{Action: syncer.Switch, Path: "/c/current", Why: "content none to 2026.11.15.2"}, {Action: syncer.Write, Path: "/o/opencode.jsonc"}},
		Skipped: []syncer.Change{{Action: syncer.Skip, Path: "/o/agents/x.md", Why: "changed by hand"}},
	}
	var b bytes.Buffer
	printSync(&b, rep, true)
	want := "note: a note\n" +
		"  would switch /c/current (content none to 2026.11.15.2)\n" +
		"  would write  /o/opencode.jsonc\n" +
		"  skipped      /o/agents/x.md: changed by hand\n" +
		"dry run: 2 changes, none made\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}

	b.Reset()
	printSync(&b, &syncer.Report{Content: "2026.11.15.2"}, false)
	if b.String() != "nothing changed: content 2026.11.15.2 is in use\n" {
		t.Fatalf("got %q", b.String())
	}
}
