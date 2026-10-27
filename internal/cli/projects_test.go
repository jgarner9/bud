package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
)

// withState points XDG_STATE_HOME at a temp dir and returns the registry's path
func withState(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return p.Projects()
}

func registry(t *testing.T, path string) *projects.Registry {
	t.Helper()
	r, err := projects.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegisterProject(t *testing.T) {
	path := withState(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	if err := registerProject(dir, at); err != nil {
		t.Fatal(err)
	}
	if err := registerProject(dir, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r := registry(t, path)
	if len(r.Projects) != 1 || !r.Projects[0].LastOpened.Equal(at.Add(time.Hour)) {
		t.Fatalf("got %+v", r.Projects)
	}
}

func TestProjectsCommands(t *testing.T) {
	path := withState(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := projectsAdd([]string{filepath.Join(root, "a"), filepath.Join(root, "b")}); err != nil {
		t.Fatal(err)
	}
	// adding again is fine
	if err := projectsAdd([]string{filepath.Join(root, "a")}); err != nil {
		t.Fatal(err)
	}
	if err := projectsPin([]string{"b"}, true); err != nil {
		t.Fatal(err)
	}
	r := registry(t, path)
	var names []string
	for _, p := range r.Sorted() {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"b", "a"}) {
		t.Errorf("sorted = %v", names)
	}

	if err := projectsLs(nil); err != nil {
		t.Fatal(err)
	}
	if err := projectsRm([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := projectsRm([]string{"a"}); err == nil {
		t.Error("expected an error removing it twice")
	}
	if err := projectsPin([]string{"nope"}, true); err == nil {
		t.Error("expected an error pinning an unknown name")
	}
	if err := projectsPin([]string{"b"}, false); err != nil {
		t.Fatal(err)
	}
	if r := registry(t, path); len(r.Projects) != 1 || r.Projects[0].Pinned {
		t.Errorf("got %+v", r.Projects)
	}

	// pinning an unregistered dir registers it
	if err := projectsPin([]string{filepath.Join(root, "a")}, true); err != nil {
		t.Fatal(err)
	}
	if p, ok := registry(t, path).Get("a"); !ok || !p.Pinned {
		t.Errorf("a = %+v, %v", p, ok)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second:    "just now",
		5 * time.Minute:     "5m ago",
		3 * time.Hour:       "3h ago",
		50 * time.Hour:      "2d ago",
		30 * 24 * time.Hour: "4w ago",
	} {
		if got := ago(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}
