package curator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/kb"
	"jgarner.dev/bud/internal/memory"
)

// fakeMemory serves a fixed export
type fakeMemory struct{ exp memory.Export }

func (f *fakeMemory) Export(context.Context) (*memory.Export, error) { return &f.exp, nil }

func (f *fakeMemory) add(id int64, source, date, privacy, body string) {
	f.exp.Records = append(f.exp.Records, memory.ExportedRecord{ID: id, Source: source, Kind: "fact", Privacy: privacy, Body: body, CreatedAt: date + " 10:00:00", State: "approved"})
}

// fakeModel reads "key=value" bodies as one fact, and counts its calls
type fakeModel struct {
	calls int
	err   error
}

func (m *fakeModel) extract(_ context.Context, rec memory.ExportedRecord, _ []string) ([]Fact, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	key, value, ok := strings.Cut(rec.Body, "=")
	if !ok {
		return nil, nil
	}
	return []Fact{{Key: key, Value: value, Statement: "It is " + value + ".", Page: "Home server", Section: "Router"}}, nil
}

type fixture struct {
	c   *Curator
	mem *fakeMemory
	mdl *fakeModel
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	r := kb.Repo{Dir: dir}
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"config", "user.name", "Jason"},
		{"config", "user.email", "jason@example.invalid"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "--quiet", "--allow-empty", "-m", "start"},
	} {
		if _, err := r.Git(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{mem: &fakeMemory{}, mdl: &fakeModel{}, now: time.Now().UTC()}
	f.c = &Curator{
		Repo: r, Memory: f.mem, State: filepath.Join(t.TempDir(), "curator.json"),
		Tiers: []string{"P1", "P2"}, Now: func() time.Time { return f.now },
		Logf: t.Logf,
	}
	f.c.Extract = f.mdl.extract
	return f
}

func (f *fixture) run(t *testing.T) Summary {
	t.Helper()
	sum, err := f.c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func (f *fixture) page(t *testing.T, rel string) *kb.Page {
	t.Helper()
	p, err := f.c.Repo.ReadPage(rel)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) commits(t *testing.T) int {
	t.Helper()
	out, err := f.c.Repo.Git(context.Background(), "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range out {
		n = n*10 + int(r-'0')
	}
	return n
}

func (f *fixture) git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := f.c.Repo.Git(context.Background(), args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func days(n int) string { return time.Now().UTC().AddDate(0, 0, n).Format(time.DateOnly) }

const (
	key  = "systems/home#router/port"
	page = "content/systems/home.md"
)

func TestPromotesAtTwoIndependentOccurrences(t *testing.T) {
	f := newFixture(t)
	// the same source on the same day counts once
	f.mem.add(1, "hermes", days(-2), "P1", key+"=8795")
	f.mem.add(2, "hermes", days(-2), "P1", key+"=8795")
	if sum := f.run(t); sum.Promoted != 0 || sum.Read != 2 {
		t.Fatalf("one source-day promoted: %+v", sum)
	}
	f.mem.add(3, "opencode", days(-1), "P2", key+"=8795")
	if sum := f.run(t); sum.Promoted != 1 || sum.Read != 1 {
		t.Fatalf("second run: %+v", sum)
	}
	p := f.page(t, page)
	if p.Meta.Title != "Home server" || p.Meta.Kind != "system" {
		t.Errorf("page meta %+v", p.Meta)
	}
	fact := p.Meta.Facts["router/port"]
	if fact == nil || fact.Value != "8795" || len(fact.Sources) != 3 || fact.Privacy != "P1" {
		t.Fatalf("fact %+v", fact)
	}
	if line, ok := p.FactLine("router/port"); !ok || line != "It is 8795." {
		t.Errorf("bullet %q", line)
	}
	if !strings.Contains(p.Body, "## Router") {
		t.Errorf("no section: %q", p.Body)
	}
	msg := f.git(t, "log", "-1", "--format=%an%n%B")
	for _, want := range []string{kb.CuratorName, "curator: add " + key + " = 8795", "memory record 3 (opencode"} {
		if !strings.Contains(msg, want) {
			t.Errorf("commit message lacks %q:\n%s", want, msg)
		}
	}

	// a rerun with nothing new changes nothing, and asks the model nothing
	n, calls := f.commits(t), f.mdl.calls
	f.run(t)
	if f.commits(t) != n || f.mdl.calls != calls {
		t.Errorf("rerun made %d commits and %d model calls", f.commits(t)-n, f.mdl.calls-calls)
	}
}

func TestManualPromote(t *testing.T) {
	f := newFixture(t)
	f.mem.add(4, "hermes", days(-1), "P2", key+"=8795")
	if err := f.c.Promote(4); err != nil {
		t.Fatal(err)
	}
	if sum := f.run(t); sum.Promoted != 1 {
		t.Fatalf("%+v", sum)
	}
}

func TestOnlyItsTiers(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-1), "P0", key+"=8795")
	f.mem.add(2, "opencode", days(-1), "P0", key+"=8795")
	f.run(t)
	if f.mdl.calls != 0 {
		t.Errorf("the model read %d P0 records", f.mdl.calls)
	}
}

func TestConfirmAddsSources(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	f.mem.add(3, "hermes", days(-1), "P1", key+"=8795")
	if sum := f.run(t); sum.Confirmed != 1 {
		t.Fatalf("%+v", sum)
	}
	fact := f.page(t, page).Meta.Facts["router/port"]
	if len(fact.Sources) != 3 || fact.Privacy != "P1" {
		t.Errorf("fact %+v", fact)
	}
}

func TestContradictionGoesToTheQueue(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	// one record, or records no newer than the page's evidence, don't change it
	f.mem.add(3, "hermes", days(-3), "P2", key+"=8800")
	f.mem.add(4, "opencode", days(-1), "P2", key+"=8800")
	if sum := f.run(t); sum.Queued != 1 || sum.Changed != 0 {
		t.Fatalf("%+v", sum)
	}
	q, _ := f.c.Repo.ReadQueue()
	if len(q.Items) != 1 || q.Items[0].Current.Value != "8795" || q.Items[0].Proposed.Value != "8800" || q.Items[0].Key != key {
		t.Fatalf("queue %+v", q.Items)
	}
	if v := f.page(t, page).Meta.Facts["router/port"].Value; v != "8795" {
		t.Errorf("value %s", v)
	}
	// the same question isn't asked again
	n := f.commits(t)
	f.run(t)
	if f.commits(t) != n {
		t.Error("the rerun committed")
	}

	if err := f.c.Resolve(context.Background(), q.Items[0].ID, Take); err != nil {
		t.Fatal(err)
	}
	if v := f.page(t, page).Meta.Facts["router/port"].Value; v != "8800" {
		t.Errorf("after take: %s", v)
	}
	if line, _ := f.page(t, page).FactLine("router/port"); line != "It is 8800." {
		t.Errorf("bullet %q", line)
	}
	if q, _ := f.c.Repo.ReadQueue(); len(q.Items) != 0 {
		t.Errorf("queue %+v", q.Items)
	}
	if err := f.c.Resolve(context.Background(), "nope", Keep); err == nil {
		t.Error("an unknown id resolved")
	}
}

func TestSupportedChangeReplaces(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	// newer than every source and than the page's commits
	f.mem.add(3, "hermes", days(1), "P2", key+"=8800")
	f.mem.add(4, "opencode", days(2), "P2", key+"=8800")
	if sum := f.run(t); sum.Changed != 1 {
		t.Fatalf("%+v", sum)
	}
	fact := f.page(t, page).Meta.Facts["router/port"]
	if fact.Value != "8800" || len(fact.Sources) != 2 {
		t.Errorf("fact %+v", fact)
	}
}

func TestRevertIsNotRedone(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	f.git(t, "revert", "--no-edit", "HEAD")
	n := f.commits(t)
	f.run(t)
	if f.commits(t) != n {
		t.Fatalf("the reverted fact came back from the same evidence:\n%s", f.git(t, "log", "-1", "--format=%B"))
	}
	// new evidence brings it back
	f.mem.add(3, "hermes", days(-1), "P2", key+"=8795")
	if sum := f.run(t); sum.Promoted != 1 {
		t.Fatalf("%+v", sum)
	}
}

func TestModelDownWaits(t *testing.T) {
	f := newFixture(t)
	f.mdl.err = ErrNoModel
	f.mem.add(1, "hermes", days(-1), "P2", key+"=8795")
	sum := f.run(t)
	if sum.Waiting == "" || sum.Read != 0 {
		t.Fatalf("%+v", sum)
	}
	f.mdl.err = nil
	if sum := f.run(t); sum.Read != 1 {
		t.Fatalf("the record wasn't read once the model came back: %+v", sum)
	}
}

func TestBadRecordGivesUp(t *testing.T) {
	f := newFixture(t)
	f.mdl.err = errors.New("not JSON")
	f.mem.add(1, "hermes", days(-1), "P2", key+"=8795")
	for range maxFailures + 1 {
		f.run(t)
	}
	if f.mdl.calls != maxFailures {
		t.Errorf("asked %d times, want %d", f.mdl.calls, maxFailures)
	}
}

func TestStaleness(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)

	f.now = time.Now().UTC().AddDate(0, 7, 0)
	if sum := f.run(t); sum.Flagged != 1 {
		t.Fatalf("%+v", sum)
	}
	if s := f.page(t, page).Meta.Sections["router"]; s.Status != kb.StatusReview {
		t.Fatalf("section %+v", s)
	}
	// a reference to the fact's record makes it current again
	f.mem.exp.References = []memory.Reference{{RecordID: 2, Source: "opencode", ReferencedAt: f.now.Format(time.DateOnly) + " 09:00:00"}}
	f.run(t)
	if s := f.page(t, page).Meta.Sections["router"]; s.Status != "" {
		t.Fatalf("after a reference: %+v", s)
	}

	f.mem.exp.References = nil
	f.now = time.Now().UTC().AddDate(0, 13, 0)
	if sum := f.run(t); sum.Archived != 1 {
		t.Fatalf("%+v", sum)
	}
	p := f.page(t, page)
	if _, ok := p.Section("router"); ok || len(p.Meta.Facts) != 0 {
		t.Fatalf("the section is still on the page: %+v %q", p.Meta, p.Body)
	}
	a := f.page(t, kb.ArchivePath("systems", "home"))
	if !a.Meta.Archived || a.Meta.Facts["router/port"] == nil || !strings.Contains(a.Body, "It is 8795.") {
		t.Fatalf("archive %+v %q", a.Meta, a.Body)
	}
	if msg := f.git(t, "log", "-1", "--format=%B"); !strings.Contains(msg, "bud curator restore systems/home#router") {
		t.Errorf("message %s", msg)
	}

	if err := f.c.Restore(context.Background(), "systems/home#router"); err != nil {
		t.Fatal(err)
	}
	p = f.page(t, page)
	if _, ok := p.Section("router"); !ok || p.Meta.Facts["router/port"] == nil {
		t.Fatalf("not restored: %q", p.Body)
	}
	if _, err := os.Stat(filepath.Join(f.c.Repo.Dir, "content/archive/systems/home.md")); !os.IsNotExist(err) {
		t.Errorf("the empty archive page is still there: %v", err)
	}
	if err := f.c.Restore(context.Background(), "systems/home#router"); err == nil {
		t.Error("restored twice")
	}
}

func TestEvergreenGetsARefreshCheck(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	p := f.page(t, page)
	p.Meta.Evergreen = true
	if err := f.c.Repo.WritePage(page, p); err != nil {
		t.Fatal(err)
	}
	f.git(t, "commit", "--quiet", "-am", "evergreen")

	f.now = time.Now().UTC().AddDate(0, 13, 0)
	if sum := f.run(t); sum.Archived != 0 || sum.Queued != 1 {
		t.Fatalf("%+v", sum)
	}
	q, _ := f.c.Repo.ReadQueue()
	if len(q.Items) != 1 || q.Items[0].Kind != kb.ItemRefresh {
		t.Fatalf("queue %+v", q.Items)
	}
	if err := f.c.Resolve(context.Background(), q.Items[0].ID, Take); err == nil {
		t.Error("take answered a refresh check")
	}
	if err := f.c.Resolve(context.Background(), q.Items[0].ID, Confirm); err != nil {
		t.Fatal(err)
	}
	if s := f.page(t, page).Meta.Sections["router"]; s.Status != "" || s.Referenced != f.now.Format(time.DateOnly) {
		t.Fatalf("section %+v", s)
	}
	n := f.commits(t)
	f.run(t)
	if f.commits(t) != n {
		t.Error("a confirmed section was flagged again")
	}
}

func TestJasonsEditSettlesAContradiction(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	f.mem.add(3, "hermes", days(-3), "P2", key+"=8800")
	f.mem.add(4, "opencode", days(-1), "P2", key+"=8800")
	f.run(t)
	p := f.page(t, page)
	p.Meta.Facts["router/port"].Value = "9000"
	if err := f.c.Repo.WritePage(page, p); err != nil {
		t.Fatal(err)
	}
	f.git(t, "commit", "--quiet", "-am", "Jason fixes the port")
	f.run(t)
	if q, _ := f.c.Repo.ReadQueue(); len(q.Items) != 0 {
		t.Errorf("queue %+v", q.Items)
	}
}

func TestRefusesADirtyClone(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.c.Repo.Dir, "stray.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("err = %v", err)
	}
}

func TestSupportedChangeSettlesItsQueueItem(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	f.mem.add(3, "hermes", days(-3), "P2", key+"=8800")
	f.mem.add(4, "opencode", days(-1), "P1", key+"=8800")
	f.run(t)
	q, _ := f.c.Repo.ReadQueue()
	if len(q.Items) != 1 || q.Items[0].Proposed.Privacy != "P1" {
		t.Fatalf("queue %+v", q.Items)
	}
	// newer evidence changes the fact, and the item goes in the same commit
	f.mem.add(5, "hermes", days(1), "P2", key+"=8800")
	f.mem.add(6, "opencode", days(2), "P2", key+"=8800")
	if sum := f.run(t); sum.Changed != 1 {
		t.Fatalf("%+v", sum)
	}
	if q, _ := f.c.Repo.ReadQueue(); len(q.Items) != 0 {
		t.Errorf("queue %+v", q.Items)
	}
	if files := f.git(t, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, kb.QueuePath) {
		t.Errorf("the change's commit has %q", files)
	}
}

func TestTakeKeepsTheProposalsTier(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	f.mem.add(3, "hermes", days(-3), "P1", key+"=8800")
	f.mem.add(4, "opencode", days(-1), "P2", key+"=8800")
	f.run(t)
	q, _ := f.c.Repo.ReadQueue()
	if err := f.c.Resolve(context.Background(), q.Items[0].ID, Take); err != nil {
		t.Fatal(err)
	}
	if fact := f.page(t, page).Meta.Facts["router/port"]; fact.Value != "8800" || fact.Privacy != "P1" {
		t.Errorf("fact %+v", fact)
	}
}

func TestTakeRefusesAStaleItem(t *testing.T) {
	f := newFixture(t)
	f.mem.add(1, "hermes", days(-3), "P2", key+"=8795")
	f.mem.add(2, "opencode", days(-2), "P2", key+"=8795")
	f.run(t)
	f.mem.add(3, "hermes", days(-3), "P2", key+"=8800")
	f.mem.add(4, "opencode", days(-1), "P2", key+"=8800")
	f.run(t)
	q, _ := f.c.Repo.ReadQueue()
	p := f.page(t, page)
	p.Meta.Facts["router/port"].Value = "9000"
	if err := f.c.Repo.WritePage(page, p); err != nil {
		t.Fatal(err)
	}
	f.git(t, "commit", "--quiet", "-am", "Jason fixes the port")
	if err := f.c.Resolve(context.Background(), q.Items[0].ID, Take); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("err = %v", err)
	}
	if v := f.page(t, page).Meta.Facts["router/port"].Value; v != "9000" {
		t.Errorf("Jason's edit was overwritten: %s", v)
	}
}

func TestQueueAndRestorePathsStayInTheKB(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := &kb.Queue{Items: []kb.Item{
		{ID: "aaaa0001", Kind: kb.ItemContradiction, Page: "../../AGENTS.md", Section: "router", Key: key,
			Current: &kb.Claim{Value: "1"}, Proposed: &kb.Claim{Value: "2"}, Opened: days(0)},
		{ID: "aaaa0002", Kind: kb.ItemRefresh, Page: "content/systems/../../x.md", Section: "router", Opened: days(0)},
	}}
	if err := f.c.Repo.WriteQueue(q); err != nil {
		t.Fatal(err)
	}
	f.git(t, "add", "-A")
	f.git(t, "commit", "--quiet", "-m", "a bad queue")
	for _, id := range []string{"aaaa0001", "aaaa0002"} {
		answer := Take
		if id == "aaaa0002" {
			answer = Confirm
		}
		if err := f.c.Resolve(ctx, id, answer); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Errorf("%s: err = %v", id, err)
		}
	}
	for _, target := range []string{"systems/../other#router", "systems/home#../x", "systems/Home#router"} {
		if err := f.c.Restore(ctx, target); err == nil || !strings.Contains(err.Error(), "isn't <projects|systems>") {
			t.Errorf("restore %q: err = %v", target, err)
		}
	}
	if _, err := f.c.Repo.ReadPage("../AGENTS.md"); err == nil {
		t.Error("ReadPage left the KB")
	}
	// the nightly run drops the contradiction whose page doesn't match its key
	f.run(t)
	if q, _ := f.c.Repo.ReadQueue(); q.Find("aaaa0001") != nil {
		t.Errorf("queue %+v", q.Items)
	}
}
