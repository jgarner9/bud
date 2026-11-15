package overlay

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points git's config at nothing, so the tests don't depend on the
// machine's, and names a committer
func isolate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// remote makes a bare repo with one commit and returns its URL and a work
// clone to push more from
func remote(t *testing.T) (url, work string) {
	t.Helper()
	root := t.TempDir()
	url = filepath.Join(root, "overlay.git")
	run(t, root, "init", "--quiet", "--bare", "--initial-branch=main", url)
	work = filepath.Join(root, "work")
	run(t, root, "clone", "--quiet", url, work)
	write(t, filepath.Join(work, "overlay.json"), `{"format": 1}`)
	run(t, work, "add", "-A")
	run(t, work, "commit", "--quiet", "-m", "one")
	run(t, work, "push", "--quiet", "origin", "HEAD:main")

	return url, work
}

func push(t *testing.T, work, file string) string {
	t.Helper()
	write(t, filepath.Join(work, file), file)
	run(t, work, "add", "-A")
	run(t, work, "commit", "--quiet", "-m", file)
	run(t, work, "push", "--quiet", "origin", "HEAD:main")

	return run(t, work, "rev-parse", "HEAD")
}

func TestFetchClonesThenFastForwards(t *testing.T) {
	isolate(t)
	url, work := remote(t)
	dir := filepath.Join(t.TempDir(), "share", "bud", "overlay")
	ctx := context.Background()

	first, err := Fetch(ctx, url, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := run(t, work, "rev-parse", "HEAD"); first != want {
		t.Fatalf("clone at %s, want %s", first, want)
	}

	want := push(t, work, "env/laptop.jsonc")
	got, err := Fetch(ctx, url, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("fetched %s, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "env", "laptop.jsonc")); err != nil {
		t.Fatal(err)
	}
}

func TestFetchStopsOnDivergenceAndChangesNothing(t *testing.T) {
	isolate(t)
	url, work := remote(t)
	dir := filepath.Join(t.TempDir(), "overlay")
	ctx := context.Background()
	if _, err := Fetch(ctx, url, dir); err != nil {
		t.Fatal(err)
	}
	// a local commit, and a different one upstream
	write(t, filepath.Join(dir, "local.md"), "mine")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", "local")
	before := run(t, dir, "rev-parse", "HEAD")
	push(t, work, "upstream.md")

	_, err := Fetch(ctx, url, dir)
	if err == nil || !strings.Contains(err.Error(), "diverged") || !strings.Contains(err.Error(), "nothing changed") {
		t.Fatalf("got %v", err)
	}
	if after := run(t, dir, "rev-parse", "HEAD"); after != before {
		t.Fatal("a diverged overlay was moved")
	}
}

func TestFetchRefusesLocalChangesAndOtherRepos(t *testing.T) {
	isolate(t)
	url, _ := remote(t)
	dir := filepath.Join(t.TempDir(), "overlay")
	ctx := context.Background()
	if _, err := Fetch(ctx, url, dir); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(dir, "dirty.md"), "x")
	if _, err := Fetch(ctx, url, dir); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("dirty: %v", err)
	}
	os.Remove(filepath.Join(dir, "dirty.md"))

	other, _ := remote(t)
	if _, err := Fetch(ctx, other, dir); err == nil || !strings.Contains(err.Error(), "is a clone of") {
		t.Fatalf("other repo: %v", err)
	}
	if _, err := Fetch(ctx, "", dir); err == nil {
		t.Fatal("no url: want an error")
	}
	notGit := t.TempDir()
	if _, err := Fetch(ctx, url, notGit); err == nil || !strings.Contains(err.Error(), "isn't a git clone") {
		t.Fatalf("not a clone: %v", err)
	}
}

func TestFetchWontActOnAnEnclosingClone(t *testing.T) {
	isolate(t)
	url, work := remote(t)
	// a plain dir inside a clean clone of the same repo
	inner := filepath.Join(work, "nested")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	before := run(t, work, "rev-parse", "HEAD")
	if _, err := Fetch(context.Background(), url, inner); err == nil || !strings.Contains(err.Error(), "isn't a git clone") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if after := run(t, work, "rev-parse", "HEAD"); after != before {
		t.Fatal("the enclosing clone was moved")
	}
}

func TestFetchNeedsABranchThatTracksOrigin(t *testing.T) {
	isolate(t)
	url, work := remote(t)
	dir := filepath.Join(t.TempDir(), "overlay")
	ctx := context.Background()
	if _, err := Fetch(ctx, url, dir); err != nil {
		t.Fatal(err)
	}
	other, _ := remote(t)
	run(t, dir, "remote", "add", "other", other)
	run(t, dir, "fetch", "--quiet", "other")
	run(t, dir, "branch", "--quiet", "--set-upstream-to=other/main")
	push(t, work, "new.md")
	before := run(t, dir, "rev-parse", "HEAD")
	if _, err := Fetch(ctx, url, dir); err == nil || !strings.Contains(err.Error(), "doesn't track origin") {
		t.Fatalf("tracking another remote: %v", err)
	}
	if after := run(t, dir, "rev-parse", "HEAD"); after != before {
		t.Fatal("an overlay tracking another remote was moved")
	}

	run(t, dir, "branch", "--quiet", "--set-upstream-to=origin/main")
	run(t, dir, "checkout", "--quiet", "--detach")
	if _, err := Fetch(ctx, url, dir); err == nil || !strings.Contains(err.Error(), "isn't on a branch") {
		t.Fatalf("detached: %v", err)
	}
}

func TestCheckFormat(t *testing.T) {
	dir := t.TempDir()
	if err := CheckFormat(dir); err != nil {
		t.Fatalf("no overlay.json is fine: %v", err)
	}
	write(t, filepath.Join(dir, "overlay.json"), `{"format": 2}`)
	if err := CheckFormat(dir); err == nil || !strings.Contains(err.Error(), "format 2") {
		t.Fatalf("got %v", err)
	}
}

// tree writes files under root, each "path" -> body
func tree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		write(t, filepath.Join(root, filepath.FromSlash(p)), body)
	}
}

func summary(items []Item) string {
	var lines []string
	for _, it := range items {
		s := it.Kind + "/" + it.ID + " " + it.Layer
		if it.Overrides {
			s += " override"
		}
		lines = append(lines, s)
	}

	return strings.Join(lines, "\n")
}

func TestTheOverlayWinsByIDAndEnvAppliesLast(t *testing.T) {
	pub := t.TempDir()
	tree(t, pub, map[string]string{
		"AGENTS.md":                  "generic",
		"skills/learn/SKILL.md":      "public learn",
		"skills/learn/extra.md":      "only in the public learn",
		"skills/planning/SKILL.md":   "public planning",
		"agents/plan.md":             "public plan",
		"commands/check.md":          "public check",
		"env/registry.jsonc":         "{}",
		"env/base.jsonc":             "{}",
		"content.json":               "{}",
		"skills/.hidden/SKILL.md":    "dot dirs are skipped",
		"plugins/approvals/index.ts": "public",
	})
	ov := t.TempDir()
	tree(t, ov, map[string]string{
		"overlay.json":                  `{"format": 1}`,
		"content/AGENTS.md":             "persona",
		"content/skills/learn/SKILL.md": "my learn",
		"content/skills/mail/SKILL.md":  "personal skill",
		"content/agents/plan.md":        "my plan",
		"env/registry.jsonc":            "{}",
		"env/laptop.jsonc":              "{}",
		"profiles/laptop.json":          "{}",
	})

	items, err := Layer(pub, ov)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"agents/plan overlay override",
		"commands/check public",
		"env/base public",
		"env/registry public",
		"env/laptop overlay",
		"env/registry overlay",
		"instructions/AGENTS public",
		"instructions/AGENTS overlay",
		"plugins/approvals public",
		"skills/learn overlay override",
		"skills/mail overlay",
		"skills/planning public",
	}, "\n")
	if got := summary(items); got != want {
		t.Fatalf("got\n%s\n\nwant\n%s", got, want)
	}
	for _, it := range items {
		if it.Kind == "skills" && it.ID == "learn" && it.Path != filepath.Join(ov, "content", "skills", "learn") {
			t.Fatalf("the overlay's learn should be the whole dir, got %s", it.Path)
		}
	}
}

func TestNoOverlayMeansThePublicContentAlone(t *testing.T) {
	pub := t.TempDir()
	tree(t, pub, map[string]string{"skills/learn/SKILL.md": "x", "env/base.jsonc": "{}"})
	items, err := Layer(pub, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := summary(items); got != "env/base public\nskills/learn public" {
		t.Fatalf("got\n%s", got)
	}
}

func TestASkillDirWithoutSkillMDIsAnError(t *testing.T) {
	pub := t.TempDir()
	tree(t, pub, map[string]string{"skills/broken/notes.md": "x"})
	if _, err := Layer(pub, ""); err == nil || !strings.Contains(err.Error(), "needs a SKILL.md") {
		t.Fatalf("got %v", err)
	}
}

func TestLayerRefusesAnOverlayOfAnotherFormat(t *testing.T) {
	pub, ov := t.TempDir(), t.TempDir()
	write(t, filepath.Join(ov, "overlay.json"), `{"format": 9}`)
	if _, err := Layer(pub, ov); err == nil {
		t.Fatal("want an error")
	}
}
