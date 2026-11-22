package kb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// NewTestRepo makes a git repo in a temp dir for tests, with commits by
// Jason unless the curator makes them
func newTestRepo(t *testing.T) Repo {
	t.Helper()
	dir := t.TempDir()
	r := Repo{Dir: dir}
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"config", "user.name", "Jason"},
		{"config", "user.email", "jason@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := r.Git(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}

	return r
}

func write(t *testing.T, r Repo, rel, body string) {
	t.Helper()
	p := filepath.Join(r.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportTakesOnlyCommittedPages(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	write(t, r, "content/systems/archie.md", samplePage)
	write(t, r, "data/review.json", `{"items":[]}`)
	write(t, r, "README.md", "not a page")
	if _, err := r.Git(ctx, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git(ctx, "commit", "--quiet", "-m", "pages"); err != nil {
		t.Fatal(err)
	}
	write(t, r, "content/systems/uncommitted.md", samplePage)

	dst := t.TempDir()
	head, _ := r.Head(ctx)
	if err := r.Export(ctx, head, dst); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{"content/systems/archie.md": true, "data/review.json": true, "README.md": false, "content/systems/uncommitted.md": false} {
		_, err := os.Stat(filepath.Join(dst, rel))
		if (err == nil) != want {
			t.Errorf("%s exported = %v, want %v", rel, err == nil, want)
		}
	}
}

func TestExportRefusesSymlinks(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(r.Dir, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(r.Dir, "content", "leak.md")); err != nil {
		t.Fatal(err)
	}
	_, _ = r.Git(ctx, "add", "-A")
	if _, err := r.Git(ctx, "commit", "--quiet", "-m", "link"); err != nil {
		t.Fatal(err)
	}
	head, _ := r.Head(ctx)
	if err := r.Export(ctx, head, t.TempDir()); err == nil {
		t.Fatal("a symlink was exported")
	}
}

func TestLastHumanEditSkipsTheCurator(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if got, _ := r.LastHumanEdit(ctx, "content/systems/archie.md"); !got.IsZero() {
		t.Fatalf("no commits yet, got %v", got)
	}
	write(t, r, "content/systems/archie.md", samplePage)
	_, _ = r.Git(ctx, "add", "-A")
	if _, err := r.Git(ctx, "commit", "--quiet", "-m", "by hand", "--date", "2026-01-02T03:04:05Z"); err != nil {
		t.Fatal(err)
	}
	write(t, r, "content/systems/archie.md", samplePage+"\nMore.\n")
	if err := r.Commit(ctx, "kb: by the curator", "content/systems/archie.md"); err != nil {
		t.Fatal(err)
	}
	got, err := r.LastHumanEdit(ctx, "content/systems/archie.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.UTC().Format("2006-01-02") != "2026-01-02" {
		t.Errorf("last human edit = %v", got)
	}
}

func TestQueueRoundTrip(t *testing.T) {
	r := newTestRepo(t)
	q, err := r.ReadQueue()
	if err != nil || len(q.Items) != 0 {
		t.Fatalf("empty queue: %v %v", q, err)
	}
	q.Items = append(q.Items, Item{ID: "b", Kind: ItemRefresh, Opened: "2026-02-01"}, Item{ID: "a", Kind: ItemContradiction, Opened: "2026-01-01"})
	if err := r.WriteQueue(q); err != nil {
		t.Fatal(err)
	}
	q, _ = r.ReadQueue()
	if q.Items[0].ID != "a" || q.Find("b") == nil {
		t.Errorf("queue = %+v", q.Items)
	}
	q.Remove("a")
	if len(q.Items) != 1 || q.Find("a") != nil {
		t.Errorf("after remove = %+v", q.Items)
	}
	if ItemID("x", "y") != ItemID("x", "y") || ItemID("x", "y") == ItemID("x", "z") {
		t.Error("ItemID isn't stable and distinct")
	}
}
