package improve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/registry"
)

const testRegistry = `// test registry
{
  "version": 1,
  "providers": {
    "go": { "api": "openai", "endpoint": "https://go.example/v1" }
  },
  "models": {
    // kept by hand
    "alpha": { "provider": "go", "name": "alpha", "privacy": ["P1", "P2"], "cost": "C1", "context": 1000 }
  },
  "roles": {
    "coding": { "pool": ["alpha"], "needs": ["tools"] }
  }
}
`

// fakeGitHub serves the part of GitHub's API the loop uses, for one repo
type fakeGitHub struct {
	t     *testing.T
	mu    sync.Mutex
	files map[string]map[string]string // branch -> path -> content
	prs   map[int]*PullRequest
	heads map[int]string
	label map[int][]string
	// contains says which content tags hold which merge commits
	contains map[string]bool
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *httptest.Server) {
	f := &fakeGitHub{t: t, files: map[string]map[string]string{"main": {PublicRegistry: testRegistry}}, prs: map[int]*PullRequest{}, heads: map[int]string{}, label: map[int][]string{}, contains: map[string]bool{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no token", 401)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/repos/jgarner9/bud")
	var in map[string]any
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &in)
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == "GET" && p == "":
		reply(map[string]string{"default_branch": "main"})
	case r.Method == "GET" && strings.HasPrefix(p, "/contents/"):
		file := strings.TrimPrefix(p, "/contents/")
		c, ok := f.files[r.URL.Query().Get("ref")][file]
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(c)), "sha": "sha-" + file})
	case r.Method == "GET" && strings.HasPrefix(p, "/git/ref/heads/"):
		reply(map[string]any{"object": map[string]string{"sha": "head-" + strings.TrimPrefix(p, "/git/ref/heads/")}})
	case r.Method == "POST" && p == "/git/refs":
		branch := strings.TrimPrefix(in["ref"].(string), "refs/heads/")
		f.files[branch] = map[string]string{}
		for k, v := range f.files["main"] {
			f.files[branch][k] = v
		}
		w.WriteHeader(201)
		reply(map[string]string{})
	case r.Method == "PUT" && strings.HasPrefix(p, "/contents/"):
		b, _ := base64.StdEncoding.DecodeString(in["content"].(string))
		if in["sha"] == nil {
			f.t.Errorf("an update to %s carries no sha", p)
		}
		f.files[in["branch"].(string)][strings.TrimPrefix(p, "/contents/")] = string(b)
		reply(map[string]string{})
	case r.Method == "POST" && p == "/pulls":
		n := len(f.prs) + 1
		if in["draft"] != true {
			f.t.Errorf("pull request %d isn't a draft", n)
		}
		f.prs[n] = &PullRequest{Number: n, URL: fmt.Sprintf("https://github.example/pull/%d", n), State: "open"}
		f.heads[n] = in["head"].(string)
		w.WriteHeader(201)
		reply(f.prs[n])
	case r.Method == "POST" && strings.HasSuffix(p, "/labels"):
		var n int
		fmt.Sscanf(p, "/issues/%d/labels", &n)
		for _, l := range in["labels"].([]any) {
			f.label[n] = append(f.label[n], l.(string))
		}
		reply([]any{})
	case r.Method == "GET" && strings.HasPrefix(p, "/pulls/"):
		var n int
		fmt.Sscanf(p, "/pulls/%d", &n)
		pr, ok := f.prs[n]
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(pr)
	case r.Method == "GET" && strings.HasPrefix(p, "/compare/"):
		key := strings.TrimPrefix(p, "/compare/")
		if f.contains[key] {
			reply(map[string]string{"status": "ahead"})
		} else {
			reply(map[string]string{"status": "behind"})
		}
	default:
		f.t.Errorf("unexpected GitHub call %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
	}
}

// answers is a fake reviewer: each prompt takes the next answer
type answers struct {
	mu      sync.Mutex
	next    []string // "yes", "no", "none"
	prompts []approval.Prompt
}

func (a *answers) review(_ context.Context, p approval.Prompt) (approval.Reply, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prompts = append(a.prompts, p)
	if len(a.next) == 0 {
		return approval.Reply{}, errors.New("no answer queued")
	}
	ans := a.next[0]
	a.next = a.next[1:]
	switch ans {
	case "yes":
		return approval.Reply{ID: p.ID, Scope: Scope, Choice: approval.AllowOnce, Answered: true}, nil
	case "no":
		return approval.Reply{ID: p.ID, Scope: Scope, Choice: approval.DenyOnce, Answered: true}, nil
	default:
		return approval.Reply{ID: p.ID, Scope: Scope, Choice: approval.DenyOnce}, nil
	}
}

// TestLoopEndToEnd follows the loop from a scan's observation through a
// denied digest, an approved one, the pull request, its merge, the edge
// rollout, a regression and the revert proposal (JGA-321's validation)
func TestLoopEndToEnd(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })

	gh, srv := newFakeGitHub(t)
	dir := t.TempDir()
	var listed int // what the provider reports as alpha's context
	listed = 2000
	var topUp float64 // the month's top-up so far
	daily := 1.0
	ans := &answers{}
	edge := ""
	day := time.Date(2026, 11, 1, 11, 0, 0, 0, time.UTC) // a Sunday

	l := &Loop{
		State:  filepath.Join(dir, "improve.json"),
		GitHub: &GitHub{API: srv.URL, Token: "tok"},
		Public: "jgarner9/bud",
		Measure: func(context.Context) Snapshot {
			return Snapshot{Spend: map[string]float64{"alpha": topUp}, TopUp: topUp}
		},
		List: func(_ context.Context, id string, _ registry.Provider) ([]registry.Listing, error) {
			return []registry.Listing{
				{ID: "alpha", Facts: &registry.Facts{Context: listed, Caps: []string{"tools"}, HasCaps: true}},
				{ID: "beta", Facts: &registry.Facts{Context: 9000, Caps: []string{"tools"}, HasCaps: true}},
			}, nil
		},
		Runtimes: []RuntimePin{{Name: "Hermes Agent", Pinned: "0.23.2", Latest: func(context.Context) (string, string, error) {
			return "v2026.11.1", "Hermes Agent v0.24.0", nil
		}}},
		Channel: func(context.Context) (*content.Channel, error) {
			return &content.Channel{Channels: map[string]content.Versions{"edge": {Content: edge}}}, nil
		},
		Approvals:  approval.Store{Path: filepath.Join(dir, "approvals.json")},
		Review:     ans.review,
		DigestDay:  time.Sunday,
		DigestHour: 10,
		Logf:       t.Logf,
		Now:        func() time.Time { return day },
	}
	round := func() {
		t.Helper()
		if day.Day() == 1 {
			topUp = 0
		}
		topUp += daily
		if err := l.Round(context.Background()); err != nil {
			t.Fatalf("round on %s: %v", day.Format(time.DateOnly), err)
		}
	}
	state := func() *State {
		t.Helper()
		st, err := Load(l.State)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	// week 1: the scan sees alpha's context change and a new model, the
	// digest asks once, and Jason says no
	ans.next = []string{"no"}
	round()
	st := state()
	if len(st.Proposals) != 1 || st.Proposals[0].Status != Denied {
		t.Fatalf("after a no: %+v", st.Proposals)
	}
	if len(ans.prompts) != 1 {
		t.Fatalf("asked %d times, want once", len(ans.prompts))
	}
	var args struct{ Digest string }
	_ = json.Unmarshal(ans.prompts[0].Invocation.Arguments, &args)
	for _, want := range []string{"Refresh registry facts for alpha", "alpha.context: 1000 -> 2000", "go/beta (9k context): could fill coding", "Hermes Agent has a newer release"} {
		if !strings.Contains(args.Digest, want) {
			t.Errorf("the digest doesn't say %q:\n%s", want, args.Digest)
		}
	}

	// the rest of the week asks nothing, and the same change isn't
	// proposed again
	for range 7 {
		day = day.AddDate(0, 0, 1)
		round()
	}
	if len(ans.prompts) != 1 || len(state().Proposals) != 1 {
		t.Fatalf("a dropped change came back: %d prompts, %+v", len(ans.prompts), state().Proposals)
	}

	// week 3: the provider reports another context; nobody answers on
	// Sunday, so Monday asks again, and Jason says yes
	listed = 4000
	for range 6 {
		day = day.AddDate(0, 0, 1)
		round()
	}
	ans.next = []string{"none", "yes"}
	day = day.AddDate(0, 0, 1) // Sunday 11-15
	round()
	if st = state(); st.Proposals[1].Status != New {
		t.Fatalf("an unanswered digest changed the proposal: %+v", st.Proposals[1])
	}
	day = day.AddDate(0, 0, 1)
	round()
	st = state()
	p := st.Proposals[1]
	if p.Status != Open || p.PR != 1 || len(ans.prompts) != 3 {
		t.Fatalf("after a yes: %+v (%d prompts)", p, len(ans.prompts))
	}
	branch := gh.heads[1]
	if got := gh.files[branch][PublicRegistry]; !strings.Contains(got, `"context": 4000`) || !strings.Contains(got, "// kept by hand") {
		t.Errorf("the pull request's registry:\n%s", got)
	}
	if strings.Join(gh.label[1], ",") != Label {
		t.Errorf("labels %v", gh.label[1])
	}

	// Jason merges it; edge then carries it, and the top-up spend triples
	merged := day.Add(2 * time.Hour)
	gh.prs[1].Merged, gh.prs[1].State, gh.prs[1].MergedAt, gh.prs[1].MergeSHA = true, "closed", merged, "m1"
	edge = "2026.11.16.1"
	gh.contains["m1...content-2026.11.16.1"] = true
	daily = 3
	for range 9 {
		day = day.AddDate(0, 0, 1)
		round()
	}
	st = state()
	p = st.Proposals[1]
	if p.Edge != "2026.11.16.1" {
		t.Errorf("edge %q", p.Edge)
	}
	if p.Status != Regressed {
		t.Fatalf("after the check: %+v", p)
	}
	rev := st.Proposals[2]
	if rev.Reverts != p.ID || rev.Status != New || *rev.Files[0].After != *p.Files[0].Before {
		t.Fatalf("revert %+v", rev)
	}

	// the next Sunday's digest reports the regression and offers the revert
	for day.Weekday() != time.Sunday {
		day = day.AddDate(0, 0, 1)
		ans.next = []string{"none"}
		round()
	}
	last := ans.prompts[len(ans.prompts)-1]
	_ = json.Unmarshal(last.Invocation.Arguments, &args)
	if !strings.Contains(args.Digest, "Revert: Refresh registry facts") || !strings.Contains(args.Digest, "Got worse after merging") {
		t.Errorf("the digest after a regression:\n%s", args.Digest)
	}
}

func TestOpenPRDeletesTheBranchWhenItFails(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "h"}})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/git/refs"):
			w.WriteHeader(201)
		case r.Method == "PUT":
			http.Error(w, "nope", 422)
		case r.Method == "DELETE":
			deleted = r.URL.Path
		}
	}))
	t.Cleanup(srv.Close)
	g := &GitHub{API: srv.URL}
	_, err := g.OpenPR(t.Context(), "o/r", "main", "bud-proposal/p-1", "t", "b", Label, []File{{Path: "content/x", After: strp("y")}}, nil)
	if err == nil || deleted != "/repos/o/r/git/refs/heads/bud-proposal/p-1" {
		t.Fatalf("err %v, deleted %q", err, deleted)
	}
}
