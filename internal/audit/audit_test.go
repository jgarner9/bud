package audit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return l, path
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	l, path := openTemp(t)
	l.now = func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.FixedZone("x", 3600)) }
	if err := l.Write(Event{Source: SourceRouter, Kind: KindSpend, Tier: "P1", Detail: map[string]any{"usd": 0.25}}); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(Event{Source: SourceMail, Kind: KindMailAction, Action: "send", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Source != SourceRouter || got[1].Action != "send" {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Time.Equal(time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC)) || got[0].Time.Location() != time.UTC {
		t.Errorf("the time should be stamped in UTC, got %v", got[0].Time)
	}
	if got[0].Detail["usd"] != 0.25 {
		t.Errorf("detail lost: %+v", got[0].Detail)
	}
}

func TestFileAndDirArePrivate(t *testing.T) {
	l, path := openTemp(t)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(Event{Source: SourceOps, Kind: KindApproval}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode: %v %v", fi, err)
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode: %v %v", fi, err)
	}
}

func TestOpenFixesAnExistingFilesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestEventsNeedASourceAndKind(t *testing.T) {
	l, path := openTemp(t)
	for _, e := range []Event{{Kind: KindSpend}, {Source: SourceRouter}} {
		if err := l.Write(e); err == nil {
			t.Errorf("%+v should be refused", e)
		}
	}
	if events, _ := Read(path); len(events) != 0 {
		t.Errorf("a refused event was written: %+v", events)
	}
}

func TestOversizedEventsAreRefused(t *testing.T) {
	l, _ := openTemp(t)
	err := l.Write(Event{Source: SourceGateway, Kind: KindToolCall, Detail: map[string]any{"x": strings.Repeat("a", maxEvent)}})
	if err == nil || !strings.Contains(err.Error(), "names and counts") {
		t.Fatalf("got %v", err)
	}
}

func TestNilLogDropsEvents(t *testing.T) {
	var l *Log
	if err := l.Write(Event{Source: SourceOps, Kind: KindApproval}); err != nil {
		t.Fatal(err)
	}
	if l.Path() != "" {
		t.Error("a nil log has no path")
	}
}

func TestOpenCreatesTheFilePrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "audit.jsonl")
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() != 0 {
		t.Fatalf("log %v, %v", fi, err)
	}
}

func TestOpenFailsWhereTheLogCantBeMade(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(dir, "file", "audit.jsonl")); err == nil {
		t.Fatal("a path under a file should be refused")
	}
}

func TestOpenNeedsAPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("an empty path should be refused")
	}
}

func TestReadSkipsBadLinesAndMissingFiles(t *testing.T) {
	if got, err := Read(filepath.Join(t.TempDir(), "none")); err != nil || got != nil {
		t.Fatalf("missing file: %v %v", got, err)
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	body := "not json\n{\"time\":\"2026-11-26T00:00:00Z\",\"source\":\"ops\",\"kind\":\"approval\"}\n{\"source\":\"ops\"}\n{\"time\":\"2026-11-26T00:00:01Z\",\"source\":\"mail\",\"kind\":\"mail_action\"}"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil || len(got) != 2 || got[1].Source != "mail" {
		t.Fatalf("got %+v %v", got, err)
	}
}

// Several leaves append at once, from separate handles, as the real
// processes do; every line must come back whole
func TestConcurrentWritersNeverInterleave(t *testing.T) {
	_, path := openTemp(t)
	const writers, each = 8, 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		l, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if err := l.Write(Event{Source: SourceGateway, Kind: KindToolCall, Detail: map[string]any{"pad": strings.Repeat("z", 300)}}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != writers*each {
		t.Fatalf("got %d events, want %d", len(got), writers*each)
	}
}

func TestOpenFixesAnExistingDirsMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(dir, "audit.jsonl")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", fi.Mode().Perm())
	}
}

// A read that fails partway must say so, not return a short log as whole
func TestReadReportsAFailedRead(t *testing.T) {
	got, err := Read(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read the audit log") {
		t.Fatalf("got %v, %v", got, err)
	}
}
