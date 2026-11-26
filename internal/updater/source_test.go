package updater

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/version"
)

func TestSourceVersion(t *testing.T) {
	built := time.Date(2026, 11, 26, 18, 5, 0, 0, time.UTC)
	got := SourceVersion("1.1.1", built, "0123456789abcdef")
	if want := "1.1.1-nightly.202611261805.main.0123456"; got != want {
		t.Fatalf("SourceVersion = %q, want %q", got, want)
	}
	if _, ok := version.Parse(got); !ok {
		t.Errorf("%q isn't a version bud can read", got)
	}
	if c := SourceCommit(got); c != "0123456" {
		t.Errorf("SourceCommit(%q) = %q", got, c)
	}

	// a real nightly, a release and a development build name no commit
	for _, v := range []string{"1.1.1-nightly.202611260917", "1.1.0", "dev", "1.1.1-nightly.x.main.0123456", ""} {
		if c := SourceCommit(v); c != "" {
			t.Errorf("SourceCommit(%q) = %q, want none", v, c)
		}
	}

	// a source build is newer than the nightly built before it, older than the one after
	parse := func(s string) version.Semver { v, _ := version.Parse(s); return v }
	if version.Compare(parse(got), parse("1.1.1-nightly.202611260917")) <= 0 {
		t.Errorf("%s should be newer than an earlier nightly", got)
	}
	if version.Compare(parse(got), parse("1.1.1-nightly.202611270917")) >= 0 {
		t.Errorf("%s should be older than a later nightly", got)
	}
}

func TestSourceBase(t *testing.T) {
	dir := t.TempDir()
	for content, want := range map[string]string{"1.1.0\n": "1.1.1", "2.0.9": "2.0.10"} {
		os.WriteFile(filepath.Join(dir, "VERSION"), []byte(content), 0o644)
		if got, err := SourceBase(dir); err != nil || got != want {
			t.Errorf("VERSION %q: %q, %v; want %q", content, got, err, want)
		}
	}
	os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1.2.0-rc.1\n"), 0o644)
	if _, err := SourceBase(dir); err == nil || !strings.Contains(err.Error(), "VERSION") {
		t.Errorf("a prerelease VERSION gave %v", err)
	}
	if _, err := SourceBase(t.TempDir()); err == nil {
		t.Error("a missing VERSION gave no error")
	}
}

// sh runs a command in dir and fails the test if it does
func sh(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// fakeMain makes a throwaway git repo shaped like bud (a module that prints
// version.Version) with one commit on main, and points SourceURL at it
func fakeMain(t *testing.T) string {
	t.Helper()
	origin := t.TempDir()
	files := map[string]string{
		"go.mod":                      "module jgarner.dev/bud\n\ngo 1.21\n",
		"VERSION":                     "1.1.0\n",
		"internal/version/version.go": "package version\n\nvar Version = \"\"\n",
		"cmd/bud/main.go":             "package main\n\nimport (\n\t\"fmt\"\n\n\t\"jgarner.dev/bud/internal/version\"\n)\n\nfunc main() { fmt.Print(version.Version) }\n",
	}
	for name, body := range files {
		path := filepath.Join(origin, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sh(t, origin, "git", "init", "--quiet", "--initial-branch", "main")
	sh(t, origin, "git", "-c", "user.name=t", "-c", "user.email=t@example.com", "add", ".")
	sh(t, origin, "git", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "first")

	orig := SourceURL
	t.Cleanup(func() { SourceURL = orig })
	SourceURL = "file://" + origin

	return origin
}

func TestFetchAndBuildMain(t *testing.T) {
	origin := fakeMain(t)
	dir := filepath.Join(t.TempDir(), "cache", "source")
	ctx := context.Background()

	first, err := FetchMain(ctx, dir)
	if err != nil || len(first) != 40 {
		t.Fatalf("clone: %q, %v", first, err)
	}

	// a new commit on main, and a stray file in the cache, which the next fetch clears
	os.WriteFile(filepath.Join(origin, "VERSION"), []byte("1.2.0\n"), 0o644)
	sh(t, origin, "git", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-am", "second")
	os.WriteFile(filepath.Join(dir, "stray"), []byte("x"), 0o644)
	second, err := FetchMain(ctx, dir)
	if err != nil || second == first {
		t.Fatalf("fetch: %q, %v (first %q)", second, err, first)
	}
	if _, err := os.Stat(filepath.Join(dir, "stray")); err == nil {
		t.Error("the stray file survived the fetch")
	}
	if base, err := SourceBase(dir); err != nil || base != "1.2.1" {
		t.Errorf("SourceBase = %q, %v; want 1.2.1", base, err)
	}

	out := filepath.Join(t.TempDir(), "bud")
	v := SourceVersion("1.2.1", time.Now(), second)
	if err := BuildMain(ctx, dir, out, v); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(out).Output()
	if err != nil || string(got) != v {
		t.Errorf("built bud prints %q, %v; want %q", got, err, v)
	}
}

func TestFetchMainUnreachable(t *testing.T) {
	orig := SourceURL
	t.Cleanup(func() { SourceURL = orig })
	SourceURL = "file://" + filepath.Join(t.TempDir(), "missing")
	if _, err := FetchMain(context.Background(), filepath.Join(t.TempDir(), "source")); err == nil {
		t.Error("fetching a missing repo gave no error")
	}
}

func TestLockSource(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache", "source")
	unlock, err := LockSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockSource(dir); err == nil || !strings.Contains(err.Error(), "already building") {
		t.Errorf("second lock: %v, want an already-building error", err)
	}
	unlock()
	again, err := LockSource(dir)
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	again()
}
