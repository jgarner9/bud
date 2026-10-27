package migrate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/paths"
)

func TestRun(t *testing.T) {
	src := fstest.MapFS{
		"0002_second.lua": {Data: []byte("2")},
		"0001_first.lua":  {Data: []byte("1")},
		"0003_third.lua":  {Data: []byte("3")},
		"README.md":       {Data: []byte("not a migration")},
	}
	statePath := filepath.Join(t.TempDir(), "state", "migrations.json")

	var calls []string
	fail := "0002_second"
	run := func(name string, script []byte) error {
		calls = append(calls, name+"="+string(script))
		if name == fail {
			return errors.New("boom")
		}
		return nil
	}

	// stops at the failure; the first is recorded
	ran, err := Run(src, statePath, run)
	if err == nil || !strings.Contains(err.Error(), "0002_second: boom") {
		t.Fatalf("want failure in 0002, got %v", err)
	}
	if !slices.Equal(ran, []string{"0001_first"}) || !slices.Equal(calls, []string{"0001_first=1", "0002_second=2"}) {
		t.Fatalf("ran %v, calls %v", ran, calls)
	}

	// next time picks up where it stopped
	fail, calls = "", nil
	ran, err = Run(src, statePath, run)
	if err != nil || !slices.Equal(ran, []string{"0002_second", "0003_third"}) {
		t.Fatalf("ran %v, %v", ran, err)
	}

	// and then there's nothing left
	calls = nil
	if ran, err = Run(src, statePath, run); err != nil || len(ran) != 0 || len(calls) != 0 {
		t.Fatalf("ran %v, calls %v, %v", ran, calls, err)
	}
}

func TestMarkAll(t *testing.T) {
	src := fstest.MapFS{"0001_a.lua": {}, "0002_b.lua": {}}
	statePath := filepath.Join(t.TempDir(), "migrations.json")
	if err := MarkAll(src, statePath); err != nil {
		t.Fatal(err)
	}
	ran, err := Run(src, statePath, func(string, []byte) error {
		t.Fatal("ran a marked migration")
		return nil
	})
	if err != nil || len(ran) != 0 {
		t.Fatalf("ran %v, %v", ran, err)
	}
}

func TestListBadName(t *testing.T) {
	if _, err := List(fstest.MapFS{"1_oops.lua": {}}); err == nil {
		t.Fatal("want an error for a misnamed migration")
	}
}

// every shipped migration is named right and runs in a clean nvim
func TestShipped(t *testing.T) {
	names, err := List(bud.Migrations())
	if err != nil || len(names) == 0 {
		t.Fatalf("shipped migrations: %v, %v", names, err)
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("no nvim")
	}
	root := t.TempDir()
	p := &paths.Paths{
		Config: filepath.Join(root, "config"),
		Data:   filepath.Join(root, "data"),
		State:  filepath.Join(root, "state"),
		Cache:  filepath.Join(root, "cache"),
	}
	for _, d := range []string{p.Config, p.Data, p.State, p.Cache} {
		os.MkdirAll(d, 0o755)
	}
	ran, err := Run(bud.Migrations(), p.Migrations(), Nvim(p))
	if err != nil || !slices.Equal(ran, names) {
		t.Fatalf("ran %v of %v: %v", ran, names, err)
	}

	// a Lua error fails the run
	bad := fstest.MapFS{"0001_bad.lua": {Data: []byte(`error("nope")`)}}
	if _, err := Run(bad, filepath.Join(root, "bad.json"), Nvim(p)); err == nil {
		t.Fatal("want an error from a failing script")
	}
}
