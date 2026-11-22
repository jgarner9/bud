package improve

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"jgarner.dev/bud/internal/approval"
)

func prop(target, path, before, after string) Proposal {
	p := Proposal{Title: "t", Evidence: "e", Target: target}
	f := File{Path: path}
	if before != "" {
		f.Before = strp(before)
	}
	if after != "" {
		f.After = strp(after)
	}
	p.Files = []File{f}
	return p
}

func TestValidate(t *testing.T) {
	leaks, err := NewLeaks("secret-word\n\n")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		p    Proposal
		want string // "" means valid
	}{
		{"public content", prop(Public, "content/skills/x/SKILL.md", "a", "b"), ""},
		{"overlay anywhere", prop(Overlay, "env/registry.jsonc", "a", "b"), ""},
		{"new file", prop(Public, "content/x.md", "", "b"), ""},
		{"delete", prop(Overlay, "skills/x.md", "a", ""), ""},
		{"public outside content", prop(Public, "internal/x.go", "a", "b"), "only content/"},
		{"unclean path", prop(Overlay, "env/../x", "a", "b"), "clean path"},
		{"absolute", prop(Overlay, "/etc/x", "a", "b"), "clean path"},
		{"git dir", prop(Overlay, ".git/config", "a", "b"), "git or CI"},
		{"ci", prop(Overlay, ".github/workflows/x.yml", "a", "b"), "git or CI"},
		{"no change", prop(Public, "content/x", "a", "a"), "doesn't change"},
		{"bad target", prop("main", "content/x", "a", "b"), "target"},
		{"tailnet", prop(Public, "content/x", "a", "host.real-net"+".ts.net"), "Tailscale"},
		{"placeholder tailnet", prop(Public, "content/x", "a", "host.example.ts.net"), ""},
		{"vault", prop(Public, "content/x", "a", "op:"+"//Private/item/field"), "1Password"},
		{"placeholder vault", prop(Public, "content/x", "a", "op:"+"//vault/item/field"), ""},
		{"home", prop(Public, "content/x", "a", "/ho"+"me/jason/x"), "home directory"},
		{"own pattern", prop(Public, "content/x", "a", "a secret-word here"), "BUD_LEAK_PATTERNS"},
		{"overlay may be personal", prop(Overlay, "env/x", "a", "/ho"+"me/jason/x secret-word"), ""},
	}
	for _, c := range cases {
		err := c.p.Validate(leaks)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: got %v, want an error about %q", c.name, err, c.want)
		}
	}

	bad := prop(Public, "content/x", "a", "b")
	bad.Criteria = []Criterion{{Metric: "spend.everything"}}
	if err := bad.Validate(leaks); err == nil {
		t.Error("an unknown metric passed")
	}
	if _, err := NewLeaks("("); err == nil || strings.Contains(err.Error(), "(") {
		t.Errorf("a bad pattern: %v", err)
	}
}

func TestAddDedupesAndRemembersDrops(t *testing.T) {
	st := &State{}
	now := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	p := prop(Public, "content/x", "a", "b")
	id, added, err := st.Add(p, nil, now)
	if err != nil || !added || id != "p-20261122-1" {
		t.Fatalf("first add: %s %v %v", id, added, err)
	}
	p.Title = "same change, other words"
	if id2, added, _ := st.Add(p, nil, now); added || id2 != id {
		t.Fatalf("a duplicate was queued as %s", id2)
	}
	st.drop(st.Find(id))
	st.Proposals = nil
	if _, added, _ := st.Add(p, nil, now); added {
		t.Fatal("a dropped change was queued again")
	}
	if id3, _, _ := st.Add(prop(Public, "content/y", "a", "b"), nil, now); id3 != "p-20261122-1" {
		t.Fatalf("id %s", id3)
	}
}

func TestRevertSwapsFiles(t *testing.T) {
	r := Revert(prop(Public, "content/x", "", "new"), "worse")
	if r.Files[0].Before == nil || *r.Files[0].Before != "new" || r.Files[0].After != nil {
		t.Fatalf("revert of a new file: %+v", r.Files[0])
	}
	if err := r.Validate(nil); err != nil {
		t.Fatal(err)
	}
}

func TestDailySpendAcrossMonths(t *testing.T) {
	st := &State{Snapshots: []Snapshot{
		{Day: "2026-10-30", Month: "2026-10", TopUp: 5},
		{Day: "2026-10-31", Month: "2026-10", TopUp: 7},
		{Day: "2026-11-01", Month: "2026-11", TopUp: 1},
		{Day: "2026-11-02", Month: "2026-11", TopUp: 4},
		// a missed day: 11-04 has no day before, so no value
		{Day: "2026-11-04", Month: "2026-11", TopUp: 9},
		{Day: "2026-11-05", Month: "2026-11", NoSpend: true, DoctorFail: 2},
	}}
	got := st.daily("spend.topup")
	want := map[string]float64{"2026-10-31": 2, "2026-11-01": 1, "2026-11-02": 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %v, want %v", k, got[k], v)
		}
	}
	if d := st.daily("doctor.fail"); d["2026-11-05"] != 2 || len(d) != 6 {
		t.Errorf("doctor levels %v", d)
	}
}

func TestCompareNeedsDaysOnBothSides(t *testing.T) {
	st := &State{}
	merged := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for d := 3; d <= 12; d++ {
		st.Snapshots = append(st.Snapshots, Snapshot{Day: time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC).Format(time.DateOnly), Month: "2026-11"})
	}
	if _, ok := st.Compare([]Criterion{{Metric: "doctor.fail"}}, merged); ok {
		t.Fatal("two days after the merge were enough")
	}
	st.Snapshots = append(st.Snapshots, Snapshot{Day: "2026-11-13", Month: "2026-11", DoctorFail: 1})
	res, ok := st.Compare([]Criterion{{Metric: "doctor.fail"}}, merged)
	if !ok || !res[0].Worse {
		t.Fatalf("a new failure: %+v %v", res, ok)
	}
}

func TestNextSlot(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	sat := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if got := nextSlot(sat, time.Sunday, 10); !got.Equal(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("from Saturday: %v", got)
	}
	sun := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	if got := nextSlot(sun, time.Sunday, 10); !got.Equal(sun.AddDate(0, 0, 7)) {
		t.Errorf("from the slot itself: %v", got)
	}
}

func TestFitKeepsThePromptUnderTheLimit(t *testing.T) {
	long := strings.Repeat("a line of the digest\n", 600)
	got := Fit(long, []string{"p-1"})
	p := approval.Prompt{Invocation: approval.Invocation{Action: Action, Target: "github", Arguments: Arguments(got, []string{"p-1"})}, ExpiresAt: time.Now().Add(time.Hour)}
	if n := utf8.RuneCountInString(approval.Summary(p)); n > approval.MaxSummary {
		t.Fatalf("summary is %d characters", n)
	}
	if short := Fit("short", nil); short != "short" {
		t.Errorf("a short digest changed: %q", short)
	}
}

func TestRenderCapsAndSections(t *testing.T) {
	st := &State{}
	for i := range 40 {
		st.Proposals = append(st.Proposals, Proposal{ID: "p-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Title: "Change", Evidence: strings.Repeat("x", 600), Target: Public, Status: New, Files: []File{{Path: "content/x"}}})
	}
	st.Proposals = append(st.Proposals, Proposal{ID: "p-done", Title: "Done", Target: Public, Status: Passed, Edge: "2026.11.20.1"})
	d := Render(st, "2026.11.21.2")
	if len(d.Shown) == 0 || len(d.Shown) == 40 {
		t.Fatalf("shown %d of 40", len(d.Shown))
	}
	if !strings.Contains(d.Text, "more; bud improve list") || !strings.Contains(d.Text, "version=2026.11.21.2") {
		t.Errorf("digest:\n%s", d.Text)
	}
	if utf8.RuneCountInString(d.Text) > digestBudget+100 {
		t.Errorf("digest is %d characters", utf8.RuneCountInString(d.Text))
	}
}

func TestOpenGoesStaleWhenMainMoved(t *testing.T) {
	gh, srv := newFakeGitHub(t)
	l := &Loop{GitHub: &GitHub{API: srv.URL, Token: "tok"}, Public: "jgarner9/bud"}
	p := prop(Public, PublicRegistry, "an older registry", "a newer one")
	if err := l.open(t.Context(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Status != Stale || len(gh.prs) != 0 {
		t.Fatalf("status %s, %d pull requests", p.Status, len(gh.prs))
	}
}
