package mailingest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanDropsControlAndBidiCharactersAndCuts(t *testing.T) {
	got := Clean("a\x1b[2J b‮\tc\r\nd e", 100)
	if got != "a[2J b c de" {
		t.Errorf("%q", got)
	}
	if got := Clean(strings.Repeat("é", 10), 4); got != "éééé…" {
		t.Errorf("%q", got)
	}
	if got := Clean("bad \xff byte", 100); !strings.Contains(got, "�") {
		t.Errorf("%q", got)
	}
}

func TestTheStoreIsPrivateAppendOnlyAndToleratesATornLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mail-summaries.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
	at := time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC)
	for i, d := range []time.Duration{2 * time.Hour, 0, time.Hour} {
		if err := st.Append(Summary{Folder: "INBOX", UIDValidity: 7, UID: uint32(i + 1), Received: at.Add(d), Summary: "s", From: strings.Split(strings.Repeat("x,", 9), ",")}); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	_, _ = f.WriteString(`{"folder":"INBOX","uid":9,"summ`)
	f.Close()
	all, err := st.All()
	if err != nil || len(all) != 3 {
		t.Fatalf("%d lines, %v", len(all), err)
	}
	if len(all[0].From) != maxFroms {
		t.Errorf("%d senders kept", len(all[0].From))
	}
	newest, total, _ := st.Newest(at.Add(30*time.Minute), 1)
	if total != 2 || len(newest) != 1 || newest[0].UID != 1 {
		t.Errorf("%d matched, %+v", total, newest)
	}
}

func TestAStateFileThatIsNotJSONNamesTheFix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail-ingest.json")
	_ = os.WriteFile(path, []byte("{nope"), 0o600)
	if _, err := LoadState(path); err == nil || !strings.Contains(err.Error(), "remove it") {
		t.Errorf("got %v", err)
	}
	if st, err := LoadState(filepath.Join(t.TempDir(), "missing.json")); err != nil || len(st.Folders) != 0 {
		t.Errorf("%+v, %v", st, err)
	}
}

func TestNewStoreTightensAnExistingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(filepath.Join(dir, "mail-summaries.jsonl")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("the directory is %v, want 0700", info.Mode().Perm())
	}
}

func TestAStoreThatWasNeverStartedSaysSoAndOneWithNoSummariesIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mail-summaries.jsonl")
	reader := OpenStore(path)
	if _, _, err := reader.Newest(time.Time{}, 10); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Error("opening the store to read it made its directory")
	}
	// bud mail-ingest makes the file, empty: now there is simply no mail yet
	if _, err := NewStore(path); err != nil {
		t.Fatal(err)
	}
	got, total, err := reader.Newest(time.Time{}, 10)
	if err != nil || len(got) != 0 || total != 0 {
		t.Errorf("%v, %d, %v", got, total, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
}
