package nvim

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

func TestSyncConfig(t *testing.T) {
	dst := t.TempDir()

	// a machine-local file bud didn't write must survive every sync
	local := filepath.Join(dst, "local", "config.json")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	v1 := fstest.MapFS{
		"init.lua":         {Data: []byte("require('bud.core')")},
		"lua/bud/old.lua":  {Data: []byte("return {}")},
		"lua/bud/keep.lua": {Data: []byte("return 1")},
	}
	res, err := SyncConfig(v1, dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 3 || len(res.Updated) != 0 || len(res.Removed) != 0 {
		t.Fatalf("install: got %+v", res)
	}

	res, err = SyncConfig(v1, dst)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed() {
		t.Fatalf("no-op sync changed files: %+v", res)
	}

	v2 := fstest.MapFS{
		"init.lua":         {Data: []byte("require('bud.core')\nrequire('bud.pack')")},
		"lua/bud/keep.lua": {Data: []byte("return 1")},
		"lua/bud/new.lua":  {Data: []byte("return 2")},
	}
	res, err = SyncConfig(v2, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"lua/bud/new.lua"}) ||
		!slices.Equal(res.Updated, []string{"init.lua"}) ||
		!slices.Equal(res.Removed, []string{"lua/bud/old.lua"}) {
		t.Fatalf("update: got %+v", res)
	}

	if _, err := os.Stat(filepath.Join(dst, "lua", "bud", "old.lua")); !os.IsNotExist(err) {
		t.Fatalf("old.lua should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(local); err != nil {
		t.Fatalf("local config was touched: %v", err)
	}
}

func TestCheckConfig(t *testing.T) {
	dst := t.TempDir()
	v1 := fstest.MapFS{
		"init.lua":      {Data: []byte("1")},
		"edited.lua":    {Data: []byte("2")},
		"deleted.lua":   {Data: []byte("3")},
		"dropped.lua":   {Data: []byte("4")},
		"unchanged.lua": {Data: []byte("5")},
	}
	if _, err := SyncConfig(v1, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "edited.lua"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dst, "deleted.lua")); err != nil {
		t.Fatal(err)
	}

	v2 := fstest.MapFS{
		"init.lua":      {Data: []byte("1")},
		"edited.lua":    {Data: []byte("2")},
		"deleted.lua":   {Data: []byte("3")},
		"unchanged.lua": {Data: []byte("5")},
	}
	res, err := CheckConfig(v2, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"deleted.lua"}) ||
		!slices.Equal(res.Updated, []string{"edited.lua"}) ||
		!slices.Equal(res.Removed, []string{"dropped.lua"}) {
		t.Fatalf("got %+v", res)
	}

	// nothing on disk may change
	if got, _ := os.ReadFile(filepath.Join(dst, "edited.lua")); string(got) != "hand edit" {
		t.Errorf("edited.lua = %q, check wrote to it", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "dropped.lua")); err != nil {
		t.Errorf("dropped.lua was removed: %v", err)
	}
	if files, _ := readManifest(dst); len(files) != 5 {
		t.Errorf("manifest rewritten: %v", files)
	}
}

func TestRemoveConfig(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "bud")
	src := fstest.MapFS{
		"init.lua":              {Data: []byte("1")},
		"lua/bud/core/a.lua":    {Data: []byte("2")},
		"lua/bud/plugins/b.lua": {Data: []byte("3")},
	}
	if _, err := SyncConfig(src, dst); err != nil {
		t.Fatal(err)
	}
	// something of the user's in a dir sync also writes to
	mine := filepath.Join(dst, "lua", "bud", "plugins", "mine.lua")
	if err := os.WriteFile(mine, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveConfig(dst); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(mine); err != nil {
		t.Fatalf("user file removed: %v", err)
	}
	for _, gone := range []string{"init.lua", manifestName, "lua/bud/core"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Errorf("%s still there (err = %v)", gone, err)
		}
	}

	// once the user's file is gone too, the whole dir goes
	if err := os.Remove(mine); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncConfig(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfig(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("config dir still there (err = %v)", err)
	}
}

func TestRemoveConfigSymlink(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "init.lua"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "bud")
	if err := os.Symlink(checkout, link); err != nil {
		t.Fatal(err)
	}

	if err := RemoveConfig(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("symlink still there")
	}
	if _, err := os.Stat(filepath.Join(checkout, "init.lua")); err != nil {
		t.Errorf("checkout was touched: %v", err)
	}
}

func TestRemoveConfigMissing(t *testing.T) {
	if err := RemoveConfig(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}
