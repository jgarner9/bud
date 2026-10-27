package nvim

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

// fakeCheckout makes a repo-shaped dir with nvim/ and languages/
func fakeCheckout(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for path, body := range map[string]string{
		"nvim/init.lua":         "-- init",
		"nvim/lua/bud/core.lua": "-- core",
		"languages/go.lua":      "return {}",
	} {
		full := filepath.Join(repo, filepath.FromSlash(path))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}

	return repo
}

func TestLinkUnlink(t *testing.T) {
	repo := fakeCheckout(t)
	dst := filepath.Join(t.TempDir(), "bud")

	// start from a synced copy plus a file of the user's
	src := fstest.MapFS{"init.lua": {Data: []byte("-- shipped")}, "lua/bud/old.lua": {Data: []byte("x")}}
	if _, err := SyncConfig(src, dst); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dst, "mine.lua"), []byte("keep"), 0o644)

	if err := Link(repo, dst); err != nil {
		t.Fatal(err)
	}
	if got, ok := DevLink(dst); !ok || got != repo {
		t.Fatalf("DevLink = %q, %v", got, ok)
	}
	for path, want := range map[string]string{
		"init.lua":         "-- init",
		"lua/bud/core.lua": "-- core",
		"languages/go.lua": "return {}",
		"mine.lua":         "keep",
	} {
		if b, err := os.ReadFile(filepath.Join(dst, path)); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", path, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "lua/bud/old.lua")); err == nil {
		t.Error("the synced copy should be gone")
	}

	// edits in the checkout show through
	os.WriteFile(filepath.Join(repo, "nvim/init.lua"), []byte("-- edited"), 0o644)
	if b, _ := os.ReadFile(filepath.Join(dst, "init.lua")); string(b) != "-- edited" {
		t.Errorf("init.lua = %q", b)
	}

	// sync and check leave a link alone
	res, err := SyncConfig(src, dst)
	if err != nil || res.Changed() {
		t.Fatalf("sync touched a dev link: %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "nvim/init.lua")); string(b) != "-- edited" {
		t.Fatal("sync wrote through the link into the checkout")
	}

	// linking again (another checkout) replaces it
	other := fakeCheckout(t)
	if err := Link(other, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := DevLink(dst); got != other {
		t.Fatalf("relinked to %q", got)
	}

	got, err := Unlink(dst)
	if err != nil || got != other {
		t.Fatalf("Unlink = %q, %v", got, err)
	}
	entries, _ := os.ReadDir(dst)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"mine.lua"}) {
		t.Fatalf("left %v, want only mine.lua", names)
	}
	if _, ok := DevLink(dst); ok {
		t.Fatal("still linked")
	}
}

func TestLinkRefusesNonCheckout(t *testing.T) {
	if err := Link(t.TempDir(), filepath.Join(t.TempDir(), "bud")); err == nil {
		t.Fatal("want an error for a dir that isn't a checkout")
	}
}

func TestLinkInTheWay(t *testing.T) {
	repo := fakeCheckout(t)
	dst := t.TempDir()
	os.WriteFile(filepath.Join(dst, "init.lua"), []byte("not bud's"), 0o644)
	if err := Link(repo, dst); err == nil {
		t.Fatal("want an error when a file of the user's is in the way")
	}
}

func TestRemoveConfigDevLink(t *testing.T) {
	repo := fakeCheckout(t)
	dst := filepath.Join(t.TempDir(), "bud")
	if err := Link(repo, dst); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfig(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("dir still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "nvim/init.lua")); err != nil {
		t.Fatal("removing the config deleted the checkout's files")
	}
}
