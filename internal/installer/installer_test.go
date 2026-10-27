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

func withGOOS(t *testing.T, os string) {
	t.Helper()
	orig := goos
	goos = os
	t.Cleanup(func() { goos = orig })
}

func TestInstalled(t *testing.T) {
	fakePath(t, "present", "fdfind")

	if !(Dep{Bins: []string{"present"}}).Installed() {
		t.Error("present should be installed")
	}
	if (Dep{Bins: []string{"absent"}}).Installed() {
		t.Error("absent should not be installed")
	}
	if !(Dep{Bins: []string{"fd", "fdfind"}}).Installed() {
		t.Error("any listed binary should count")
	}
}

func TestMissingKeepsOrder(t *testing.T) {
	fakePath(t, "b")
	withDeps(t, []Dep{{Name: "a", Bins: []string{"a"}}, {Name: "b", Bins: []string{"b"}}, {Name: "c", Bins: []string{"c"}}})

	var names []string
	for _, d := range Missing() {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"a", "c"}) {
		t.Fatalf("missing = %v", names)
	}
}

func TestDetectManager(t *testing.T) {
	tests := []struct {
		name string
		goos string
		path []string
		want string
	}{
		{"pacman first", "linux", []string{"pacman", "apt-get", "dnf"}, "pacman"},
		{"apt before dnf", "linux", []string{"apt-get", "dnf"}, "apt"},
		{"dnf", "linux", []string{"dnf"}, "dnf"},
		{"brew on mac", "darwin", []string{"brew", "pacman"}, "brew"},
		{"no brew on linux", "linux", []string{"brew"}, ""},
		{"none", "linux", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withGOOS(t, tt.goos)
			fakePath(t, tt.path...)
			m, found := DetectManager()
			if found != (tt.want != "") || m.Name != tt.want {
				t.Fatalf("got %q (found=%v), want %q", m.Name, found, tt.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	withDeps(t, []Dep{
		{Name: "git", Bins: []string{"git"}},
		{Name: "fd", Bins: []string{"fd"}, Pkgs: map[string][]string{"apt": {"fd-find"}}},
		{Name: "node", Bins: []string{"node"}, Pkgs: map[string][]string{"apt": {"nodejs", "npm"}}},
	})
	withGOOS(t, "linux")

	t.Run("nothing missing", func(t *testing.T) {
		fakePath(t, "git", "fd", "node")
		if err := Check(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("uses the manager's package names", func(t *testing.T) {
		fakePath(t, "git", "apt-get")
		err := Check()
		var missing *MissingError
		if !errors.As(err, &missing) {
			t.Fatalf("err = %v, want *MissingError", err)
		}
		want := "missing system dependencies: fd, node\n" +
			"install them with:\n  sudo apt install fd-find nodejs npm\n" +
			"then run 'bud install' again"
		if err.Error() != want {
			t.Fatalf("got:\n%s\nwant:\n%s", err, want)
		}
	})

	t.Run("no package manager", func(t *testing.T) {
		fakePath(t)
		want := "missing system dependencies: git, fd, node\n" +
			"install them with your system's package manager\n" +
			"then run 'bud install' again"
		if err := Check(); err == nil || err.Error() != want {
			t.Fatalf("got:\n%v\nwant:\n%s", err, want)
		}
	})
}

func TestDepsOrder(t *testing.T) {
	// config sync and plugin restore need nvim and git first
	if len(Deps) < 2 || Deps[0].Bins[0] != "nvim" || Deps[1].Bins[0] != "git" {
		t.Fatalf("nvim and git must be the first two deps")
	}
}
