package syncer

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCheckFindsNothingRightAfterASync(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	before := snapshot(t, o)

	rep, err := Check(o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed() || len(rep.Skipped) > 0 {
		t.Fatalf("got changes %+v, skipped %+v", rep.Changes, rep.Skipped)
	}
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	if drift, err := led.Drift(); err != nil || len(drift) > 0 {
		t.Fatalf("drift = %v, %v", drift, err)
	}
	if after := snapshot(t, o); !maps.Equal(before, after) {
		t.Fatal("Check changed files on disk")
	}
}

func TestDriftNamesMissingAndChangedFiles(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	cfg := filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc")
	agent := filepath.Join(o.Paths.OpenCodeConfig(), "agents", "review.md")
	if err := os.WriteFile(cfg, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(agent); err != nil {
		t.Fatal(err)
	}

	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	drift, err := led.Drift()
	if err != nil {
		t.Fatal(err)
	}
	want := []Drift{{Path: agent, Missing: true}, {Path: cfg}}
	if !slices.Equal(drift, want) {
		t.Fatalf("drift = %+v, want %+v", drift, want)
	}
}

func TestCheckSeesARenderThatNoLongerMatches(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	// the content on disk changes under the config, as an overlay pull or a
	// new adapter would change what's rendered
	base := filepath.Join(o.Paths.Content(), "2026.11.15.1", "env", "base.jsonc")
	if err := os.Chmod(base, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte(`{"permissions": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Check(o)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc")
	if len(rep.Changes) != 1 || rep.Changes[0].Action != Write || rep.Changes[0].Path != cfg {
		t.Fatalf("changes = %+v, want a write of opencode.jsonc", rep.Changes)
	}
}

func TestCheckNeedsASyncAndTheContentOnDisk(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	if _, err := Check(o); err == nil || !strings.Contains(err.Error(), "run bud sync") {
		t.Fatalf("before any sync: err = %v", err)
	}

	run(t, o)
	if err := os.RemoveAll(filepath.Join(o.Paths.Content(), "2026.11.15.1")); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(o); err == nil || !strings.Contains(err.Error(), "isn't on disk") {
		t.Fatalf("content gone: err = %v", err)
	}
}

func TestFetchChannelVerifiesWhatItReads(t *testing.T) {
	s := newServer(t)
	s.offer("edge", "2026.11.15.2")
	ch, err := FetchChannel(context.Background(), s.url)
	if err != nil {
		t.Fatal(err)
	}
	if got := ch.Channels["edge"].Content; got != "2026.11.15.2" {
		t.Fatalf("edge content = %q", got)
	}

	s.put("/"+ChannelTag+"/channel.json.sig", []byte("not a signature"))
	if _, err := FetchChannel(context.Background(), s.url); err == nil {
		t.Fatal("a bad signature was accepted")
	}
}

func TestOverlayRepoPrefersTheEnvironment(t *testing.T) {
	t.Setenv("BUD_OVERLAY", "")
	if got := OverlayRepo("git@example.com:a.git"); got != "git@example.com:a.git" {
		t.Errorf("got %q", got)
	}
	t.Setenv("BUD_OVERLAY", "git@example.com:b.git")
	if got := OverlayRepo("git@example.com:a.git"); got != "git@example.com:b.git" {
		t.Errorf("got %q", got)
	}
}
