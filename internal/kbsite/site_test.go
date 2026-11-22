package kbsite

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	bud "jgarner.dev/bud"
	"jgarner.dev/bud/internal/kb"
)

func gitRepo(t *testing.T) kb.Repo {
	t.Helper()
	r := kb.Repo{Dir: t.TempDir()}
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"config", "user.name", "Jason"},
		{"config", "user.email", "jason@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := r.Git(context.Background(), args...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func commit(t *testing.T, r kb.Repo, files map[string]string, msg string) string {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(r.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if _, err := r.Git(ctx, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git(ctx, "commit", "--quiet", "-m", msg); err != nil {
		t.Fatal(err)
	}
	head, _ := r.Head(ctx)
	return head
}

// fakeHugo renders each content page's body as a page, linking home, so the
// tests run without hugo. A body with "BROKEN" links to a page that isn't there
func fakeHugo(ctx context.Context, src, dst string) error {
	if _, err := os.Stat(filepath.Join(src, "hugo.json")); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dst, "index.html"), []byte(`<a href="/">home</a>`), 0o644); err != nil {
		return err
	}
	return filepath.WalkDir(filepath.Join(src, "content"), func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(src, "content"), p)
		out := filepath.Join(dst, strings.TrimSuffix(rel, ".md"), "index.html")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		html := `<a href="/">home</a><a href="https://linear.app/x">out</a><p>` + string(body) + `</p>`
		if strings.Contains(string(body), "BROKEN") {
			html += `<a href="../missing/">missing</a>`
		}
		return os.WriteFile(out, []byte(html), 0o644)
	})
}

const page = "{\n  \"title\": \"archie\",\n  \"kind\": \"system\"\n}\n\n## Router\n\nVERSION\n"

func live(t *testing.T, b *Builder) string {
	t.Helper()
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/systems/archie/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestBuildFollowsCommitsAndReverts(t *testing.T) {
	ctx := context.Background()
	r := gitRepo(t)
	b := &Builder{Repo: r, Site: fstest.MapFS{"hugo.json": {Data: []byte("{}")}}, Hugo: fakeHugo, Dir: t.TempDir()}

	first := commit(t, r, map[string]string{"content/systems/archie.md": strings.Replace(page, "VERSION", "port 8795", 1)}, "first")
	if changed, err := b.Build(ctx, false); err != nil || !changed {
		t.Fatalf("first build: changed %v, %v", changed, err)
	}
	if !strings.Contains(live(t, b), "port 8795") {
		t.Fatal("the site doesn't show the first commit")
	}
	if changed, err := b.Build(ctx, false); err != nil || changed {
		t.Fatalf("an unchanged HEAD rebuilt: %v %v", changed, err)
	}

	commit(t, r, map[string]string{"content/systems/archie.md": strings.Replace(page, "VERSION", "port 8800", 1)}, "edit")
	if _, err := b.Build(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(live(t, b), "port 8800") {
		t.Fatal("the site doesn't show the edit")
	}

	if _, err := r.Git(ctx, "revert", "--no-edit", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(ctx, false); err != nil {
		t.Fatal(err)
	}
	if body := live(t, b); !strings.Contains(body, "port 8795") || strings.Contains(body, "port 8800") {
		t.Fatalf("the site doesn't show the revert: %s", body)
	}
	st, _ := b.ReadStatus()
	if st.Commit == first || st.Commit == "" || st.Error != "" {
		t.Errorf("status = %+v", st)
	}

	// only the live build and the one before it are kept
	entries, _ := os.ReadDir(b.Dir)
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "site-") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d builds kept, want 2", n)
	}
}

func TestBadCommitsKeepTheLiveSite(t *testing.T) {
	ctx := context.Background()
	for name, body := range map[string]string{
		"secret":      strings.Replace(page, "VERSION", "key ghp_"+strings.Repeat("a", 36), 1),
		"broken link": strings.Replace(page, "VERSION", "BROKEN", 1),
	} {
		t.Run(name, func(t *testing.T) {
			r := gitRepo(t)
			b := &Builder{Repo: r, Site: fstest.MapFS{"hugo.json": {Data: []byte("{}")}}, Hugo: fakeHugo, Dir: t.TempDir()}
			commit(t, r, map[string]string{"content/systems/archie.md": strings.Replace(page, "VERSION", "good", 1)}, "good")
			if _, err := b.Build(ctx, false); err != nil {
				t.Fatal(err)
			}
			bad := commit(t, r, map[string]string{"content/systems/archie.md": body}, "bad")
			if _, err := b.Build(ctx, false); err == nil {
				t.Fatal("a bad commit built")
			}
			if !strings.Contains(live(t, b), "good") {
				t.Fatal("the live site changed")
			}
			st, _ := b.ReadStatus()
			if st.Failed != bad || st.Error == "" {
				t.Errorf("status = %+v", st)
			}
			// not tried again until it changes or an hour passes
			if changed, err := b.Build(ctx, false); err != nil || changed {
				t.Errorf("retried at once: %v %v", changed, err)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	b := &Builder{Dir: t.TempDir()}
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("before a build: %d", resp.StatusCode)
	}
	resp, err = http.Post(srv.URL+"/", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Content-Security-Policy") == "" {
		t.Errorf("POST: %d, headers %v", resp.StatusCode, resp.Header)
	}
}

func TestInternal(t *testing.T) {
	for link, want := range map[string]string{
		"/":                  "/",
		"../missing/":        "/systems/missing",
		"#router":            "",
		"https://linear.app": "",
		"//cdn.example/x":    "",
		"style.css?v=1":      "/systems/archie/style.css",
	} {
		got, ok := internal("/systems/archie/index.html", link)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("internal(%q) = %q, want %q", link, got, want)
		}
	}
}

// With hugo installed, the real layouts render a fixture KB: navigation,
// a review badge, sources, an archive banner and the review queue
func TestRealHugo(t *testing.T) {
	bin, err := exec.LookPath("hugo")
	if err != nil {
		t.Skip("hugo isn't installed")
	}
	ctx := context.Background()
	r := gitRepo(t)
	commit(t, r, map[string]string{
		"content/_index.md": "{\"title\": \"Bud KB\"}\n\nJason's systems and projects.\n",
		"content/systems/archie.md": `{
  "title": "archie", "kind": "system", "evergreen": true,
  "sections": {"router": {"status": "review"}},
  "facts": {"router/port": {"section": "router", "value": "8795", "updated": "2026-11-22",
    "sources": [{"record": 12, "source": "hermes", "date": "2026-11-21"}]}}
}

## Router

- The router listens on 8795. <!-- fact:router/port -->
`,
		"content/projects/bud.md":           "{\"title\": \"Bud\", \"kind\": \"project\", \"linear\": \"https://linear.app/example/project/bud\"}\n\nThe Suite.\n",
		"content/archive/systems/archie.md": "{\"title\": \"archie (archived)\", \"kind\": \"system\", \"archived\": true}\n\n## Old mail\n\nGone.\n",
		"data/review.json":                  `{"items": [{"id": "ab12cd34", "kind": "contradiction", "page": "content/systems/archie.md", "section": "router", "key": "systems/archie#router/port", "current": {"value": "8795"}, "proposed": {"value": "8800"}, "opened": "2026-11-22"}]}`,
	}, "fixture")
	b := &Builder{Repo: r, Site: bud.KBSite(), Hugo: ExecHugo(bin), Dir: t.TempDir()}
	if _, err := b.Build(ctx, false); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(b.Current(), rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	home := read("index.html")
	for _, want := range []string{"ab12cd34", "8800", "/systems/archie/", "/projects/bud/", "1 to review"} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	archie := read("systems/archie/index.html")
	for _, want := range []string{`id="router"`, "badge review", "#12 hermes", "The router listens on 8795."} {
		if !strings.Contains(archie, want) {
			t.Errorf("archie lacks %q", want)
		}
	}
	if strings.Contains(archie, "fact:router/port") {
		t.Error("the fact marker shows")
	}
	if !strings.Contains(read("archive/systems/archie/index.html"), "banner archived") {
		t.Error("no archive banner")
	}
}
