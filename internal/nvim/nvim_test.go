package nvim

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEnv(t *testing.T) {
	t.Setenv("NVIM_APPNAME", "other")
	t.Setenv("BUD_TEST_VAR", "1")

	vars := env()

	var appnames []string
	for _, kv := range vars {
		if strings.HasPrefix(kv, "NVIM_APPNAME=") {
			appnames = append(appnames, kv)
		}
	}
	if !slices.Equal(appnames, []string{"NVIM_APPNAME=" + AppName}) {
		t.Fatalf("NVIM_APPNAME entries = %v, want only bud's", appnames)
	}
	if !slices.Contains(vars, "BUD_TEST_VAR=1") {
		t.Fatal("other env vars should pass through")
	}
}

func TestSyncConfigRemovedFileAlreadyGone(t *testing.T) {
	dst := t.TempDir()

	v1 := fstest.MapFS{
		"init.lua": {Data: []byte("1")},
		"gone.lua": {Data: []byte("2")},
	}
	if _, err := SyncConfig(v1, dst); err != nil {
		t.Fatal(err)
	}

	// user deleted it by hand; dropping it from src shouldn't error
	if err := os.Remove(filepath.Join(dst, "gone.lua")); err != nil {
		t.Fatal(err)
	}

	res, err := SyncConfig(fstest.MapFS{"init.lua": {Data: []byte("1")}}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Removed, []string{"gone.lua"}) {
		t.Fatalf("removed = %v", res.Removed)
	}
}

func TestSyncConfigRestoresLocallyEditedFile(t *testing.T) {
	dst := t.TempDir()
	src := fstest.MapFS{"init.lua": {Data: []byte("bud")}}

	if _, err := SyncConfig(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "init.lua"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := SyncConfig(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Updated, []string{"init.lua"}) {
		t.Fatalf("updated = %v", res.Updated)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "init.lua"))
	if string(got) != "bud" {
		t.Fatalf("init.lua = %q, want %q", got, "bud")
	}
}

func TestSyncConfigCorruptManifest(t *testing.T) {
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, manifestName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncConfig(fstest.MapFS{"init.lua": {Data: []byte("1")}}, dst); err == nil {
		t.Fatal("expected error for corrupt manifest")
	}
}

func TestSyncConfigManifestNotSynced(t *testing.T) {
	dst := t.TempDir()
	res, err := SyncConfig(fstest.MapFS{"init.lua": {Data: []byte("1")}}, dst)
	if err != nil {
		t.Fatal(err)
	}

	files, err := readManifest(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, []string{"init.lua"}) {
		t.Fatalf("manifest = %v", files)
	}
	if slices.Contains(res.Added, manifestName) {
		t.Fatal("manifest should not be reported as a synced file")
	}
}

func TestSyncConfigManifestEscape(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "bud")
	outside := filepath.Join(root, "precious.txt")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"../precious.txt", outside, ""} {
		t.Run(bad, func(t *testing.T) {
			if err := writeManifest(dst, []string{bad}); err != nil {
				t.Fatal(err)
			}
			if _, err := SyncConfig(fstest.MapFS{"init.lua": {Data: []byte("1")}}, dst); err == nil {
				t.Fatal("expected error for manifest path outside dst")
			}
			if _, err := os.Stat(outside); err != nil {
				t.Fatalf("file outside dst was removed: %v", err)
			}
		})
	}
}
