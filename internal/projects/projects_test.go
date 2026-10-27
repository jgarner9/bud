package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// mkdirs makes dirs under a fresh temp root, returning the root with symlinks
// resolved (macOS's temp dir sits behind one)
func mkdirs(t *testing.T, dirs ...string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(projects []Project) []string {
	var out []string
	for _, p := range projects {
		out = append(out, p.Name)
	}
	return out
}

func TestLoadMissing(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "nope", "projects.json"))
	if err != nil || len(r.Projects) != 0 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestAddAndNames(t *testing.T) {
	root := mkdirs(t, "work/api", "home/api", "other/work/api", "bud")
	r := &Registry{}
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	for _, dir := range []string{"work/api", "home/api", "other/work/api", "bud"} {
		if _, err := r.Add(filepath.Join(root, dir), now); err != nil {
			t.Fatal(err)
		}
	}
	// a clash takes more of the path, one dir at a time
	if got := names(r.Projects); !slices.Equal(got, []string{"api", "home/api", "work/api", "bud"}) {
		t.Errorf("names = %v", got)
	}
	if p, _ := r.Get("work/api"); p.Path != filepath.Join(root, "other/work/api") {
		t.Errorf("work/api = %+v", p)
	}

	// the same dir again, through a symlink, is the same project
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "bud"), link); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	p, err := r.Add(link, later)
	if err != nil || p.Name != "bud" || len(r.Projects) != 4 || !p.LastOpened.Equal(later) {
		t.Errorf("re-add = %+v, %v (%d projects)", p, err, len(r.Projects))
	}
	// adding without opening keeps the last opened time
	if p, _ := r.Add(link, time.Time{}); !p.LastOpened.Equal(later) {
		t.Errorf("last opened = %v", p.LastOpened)
	}

	if _, err := r.Add(filepath.Join(root, "nope"), now); err == nil {
		t.Error("expected an error for a missing dir")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(file, now); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file: %v", err)
	}
}

func TestRemovePinAndSort(t *testing.T) {
	root := mkdirs(t, "a", "b", "c", "d")
	r := &Registry{}
	base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	r.Add(filepath.Join(root, "a"), base)
	r.Add(filepath.Join(root, "b"), base.Add(2*time.Hour))
	r.Add(filepath.Join(root, "c"), base.Add(time.Hour))
	r.Add(filepath.Join(root, "d"), time.Time{})

	if got := names(r.Sorted()); !slices.Equal(got, []string{"b", "c", "a", "d"}) {
		t.Errorf("by last opened = %v", got)
	}
	if _, err := r.SetPinned("a", true); err != nil {
		t.Fatal(err)
	}
	if got := names(r.Sorted()); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("pinned first = %v", got)
	}

	// by path works too
	if _, err := r.Remove(filepath.Join(root, "c")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Remove("c"); err == nil {
		t.Error("expected an error removing it twice")
	}
	if _, err := r.SetPinned("zzz", true); err == nil {
		t.Error("expected an error pinning an unknown project")
	}
	if got := names(r.Projects); !slices.Equal(got, []string{"a", "b", "d"}) {
		t.Errorf("after remove = %v", got)
	}
}

func TestMissing(t *testing.T) {
	root := mkdirs(t, "gone")
	r := &Registry{}
	p, _ := r.Add(filepath.Join(root, "gone"), time.Time{})
	if p.Missing() {
		t.Fatal("not missing yet")
	}
	if err := os.Remove(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}
	if !p.Missing() {
		t.Error("should be missing")
	}
	// still registered, and still removable by name
	if _, err := r.Remove("gone"); err != nil {
		t.Error(err)
	}
}

func TestRoundTripKeepsUnknownKeys(t *testing.T) {
	root := mkdirs(t, "a")
	path := filepath.Join(root, "state", "projects.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{
  "from_the_future": {"x": 1},
  "projects": [
    {"name": "a", "path": "` + filepath.Join(root, "a") + `", "pinned": true,
     "last_opened": "2026-10-27T09:00:00Z", "color": "green"}
  ]
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := r.Get("a"); !ok || !p.Pinned || p.LastOpened.Hour() != 9 {
		t.Fatalf("a = %+v", p)
	}
	if _, err := r.SetPinned("a", false); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Future   map[string]any   `json:"from_the_future"`
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Future["x"] != 1.0 || got.Projects[0]["color"] != "green" || got.Projects[0]["pinned"] != false {
		t.Errorf("round trip lost something:\n%s", data)
	}
	// no temp file left behind
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("dir has %d entries", len(entries))
	}
}

func TestSaveEmptyIsAList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := (&Registry{Path: path}).Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	// Lua reads this, and null would need a special case there
	if !strings.Contains(string(data), `"projects": []`) {
		t.Errorf("got %s", data)
	}

	// indented all the way down, and never-opened has no last_opened
	r := &Registry{Path: path}
	if _, err := r.Add(t.TempDir(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "\n      \"name\": ") || strings.Contains(string(data), "last_opened") {
		t.Errorf("got %s", data)
	}
}

func TestLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	os.WriteFile(path, []byte("{nope"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("expected an error")
	}
}
