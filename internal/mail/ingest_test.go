package mail

import (
	"context"
	"strings"
	"testing"
)

func ingestAccount(t *testing.T, m *memServer) *Account {
	t.Helper()
	a, err := NewAccount(m.addr, testUser, testPass)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFreshOnAFirstRunReturnsNoOldMailAndMovesTheMarkToTheNewest(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	a := ingestAccount(t, m)
	f, err := a.Fresh(context.Background(), "INBOX", Mark{}, FreshOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !f.First || f.Changed || len(f.Messages) != 0 || f.Newest != 3 || f.Through != 3 || f.UIDValidity == 0 {
		t.Fatalf("%+v", f)
	}
	// the folder is opened read-only and fetched with PEEK: nothing becomes read
	if st, _ := dialMem(t, m).Status(context.Background(), "INBOX"); st.Unseen != 1 {
		t.Errorf("unseen %d, want 1", st.Unseen)
	}
}

func TestFreshBackfillsTheNewestFewOnAFirstRun(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	f, err := ingestAccount(t, m).Fresh(context.Background(), "INBOX", Mark{}, FreshOptions{Limit: 10, Backfill: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Messages) != 2 || f.Messages[0].UID != 2 || f.Messages[1].UID != 3 || f.Through != 3 {
		t.Fatalf("%+v", f)
	}
}

func TestFreshReturnsOnlyNewerMailOldestFirstAndBoundedByTheLimit(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	a := ingestAccount(t, m)
	ctx := context.Background()
	first, _ := a.Fresh(ctx, "INBOX", Mark{}, FreshOptions{Limit: 10})
	mark := Mark{UIDValidity: first.UIDValidity, UID: first.Through}

	// nothing new: the server answers the highest uid for N:*, which isn't new
	none, err := a.Fresh(ctx, "INBOX", mark, FreshOptions{Limit: 10})
	if err != nil || len(none.Messages) != 0 || none.More || none.Through != 3 {
		t.Fatalf("%+v, %v", none, err)
	}

	for _, id := range []string{"a", "b", "c"} {
		m.add(t, "INBOX", msg("Body "+id+"\r\n", "From: Sam <sam@example.com>", "Subject: New "+id, "Message-ID: <"+id+"@example.com>"))
	}
	two, err := a.Fresh(ctx, "INBOX", mark, FreshOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Messages) != 2 || !two.More || two.Through != 5 || two.Messages[0].UID != 4 || two.Messages[1].UID != 5 {
		t.Fatalf("%+v", two)
	}
	if got := two.Messages[0].Message.Text; got != "Body a" {
		t.Errorf("text %q", got)
	}
	if two.Messages[0].Received.IsZero() {
		t.Error("no received time")
	}
	rest, err := a.Fresh(ctx, "INBOX", Mark{UIDValidity: two.UIDValidity, UID: two.Through}, FreshOptions{Limit: 2})
	if err != nil || len(rest.Messages) != 1 || rest.More || rest.Through != 6 || rest.Messages[0].Message.Subject != "New c" {
		t.Fatalf("%+v, %v", rest, err)
	}
}

func TestFreshWithAChangedUIDValidityReturnsNothingAndResetsTheMark(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	f, err := ingestAccount(t, m).Fresh(context.Background(), "INBOX", Mark{UIDValidity: 12345, UID: 1}, FreshOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Changed || f.First || len(f.Messages) != 0 || f.Through != 3 {
		t.Fatalf("%+v", f)
	}
}

func TestFreshCutsLongTextAndKeepsAnUnreadableMessageHandled(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	a := ingestAccount(t, m)
	ctx := context.Background()
	first, _ := a.Fresh(ctx, "INBOX", Mark{}, FreshOptions{Limit: 10})
	m.add(t, "INBOX", msg(strings.Repeat("é", 100)+"\r\n", "From: Sam <sam@example.com>", "Subject: Long"))
	f, err := a.Fresh(ctx, "INBOX", Mark{UIDValidity: first.UIDValidity, UID: first.Through}, FreshOptions{Limit: 10, MaxText: 51})
	if err != nil {
		t.Fatal(err)
	}
	// 51 bytes is mid-character; the cut backs up to a boundary
	if got := f.Messages[0].Message.Text; got != strings.Repeat("é", 25) {
		t.Errorf("text %q", got)
	}
}

func TestFreshRefusesANonPositiveLimit(t *testing.T) {
	if _, err := (&Account{}).Fresh(context.Background(), "INBOX", Mark{}, FreshOptions{}); err == nil {
		t.Error("a zero limit was accepted")
	}
}

func TestFreshNeverBackfillsMoreThanItsLimit(t *testing.T) {
	m := newMemServer(t)
	fixtureInbox(t, m)
	f, err := ingestAccount(t, m).Fresh(context.Background(), "INBOX", Mark{}, FreshOptions{Limit: 2, Backfill: 20})
	if err != nil {
		t.Fatal(err)
	}
	// the newest two, so none below the new mark is left unhandled
	if len(f.Messages) != 2 || f.Messages[0].UID != 2 || f.More || f.Through != 3 {
		t.Fatalf("%+v", f)
	}
}
