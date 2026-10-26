package installer

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fakePath puts an executable for each name in a fresh dir and makes it the whole PATH
func fakePath(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func withDeps(t *testing.T, deps []Dep) {
	t.Helper()
	orig := Deps
	Deps = deps
	t.Cleanup(func() { Deps = orig })
}

func TestInstalled(t *testing.T) {
	fakePath(t, "present")

	if !(Dep{Bin: "present"}).Installed() {
		t.Error("present should be installed")
	}
	if (Dep{Bin: "absent"}).Installed() {
		t.Error("absent should not be installed")
	}
}

func TestMissingKeepsOrder(t *testing.T) {
	fakePath(t, "b")
	withDeps(t, []Dep{{Name: "a", Bin: "a"}, {Name: "b", Bin: "b"}, {Name: "c", Bin: "c"}})

	var names []string
	for _, d := range Missing() {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"a", "c"}) {
		t.Fatalf("missing = %v", names)
	}
}

func TestEnsureAll(t *testing.T) {
	fakePath(t, "have")

	var ran []string
	install := func(name string, err error) func() error {
		return func() error {
			ran = append(ran, name)
			return err
		}
	}

	t.Run("installs only missing", func(t *testing.T) {
		ran = nil
		withDeps(t, []Dep{
			{Name: "have", Bin: "have", Install: install("have", nil)},
			{Name: "need", Bin: "need", Install: install("need", nil)},
		})
		if err := EnsureAll(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ran, []string{"need"}) {
			t.Fatalf("ran = %v", ran)
		}
	})

	t.Run("stops at first failure", func(t *testing.T) {
		ran = nil
		boom := errors.New("boom")
		withDeps(t, []Dep{
			{Name: "x", Bin: "x", Install: install("x", boom)},
			{Name: "y", Bin: "y", Install: install("y", nil)},
		})
		err := EnsureAll()
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want wrapped boom", err)
		}
		if err.Error() != "installing x: boom" {
			t.Fatalf("err = %q", err)
		}
		if !slices.Equal(ran, []string{"x"}) {
			t.Fatalf("ran = %v", ran)
		}
	})
}

func TestDepsOrder(t *testing.T) {
	// config sync and plugin restore need nvim and git first
	if len(Deps) < 2 || Deps[0].Bin != "nvim" || Deps[1].Bin != "git" {
		t.Fatalf("nvim and git must be the first two deps")
	}
	for _, d := range Deps {
		if d.Install == nil {
			t.Errorf("%s has no Install func", d.Name)
		}
	}
}
