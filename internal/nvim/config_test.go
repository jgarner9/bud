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
		"init.lua":         {Data: []byte("require('bud.core')\nrequire('bud.lazy')")},
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
