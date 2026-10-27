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
	for _, d := range Missing(Needed("full")) {
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

func withRoot(t *testing.T, root bool) {
	t.Helper()
	orig := geteuid
	geteuid = func() int {
		if root {
			return 0
		}
		return 1000
	}
	t.Cleanup(func() { geteuid = orig })
}

func TestCheck(t *testing.T) {
	withRoot(t, false)
	withDeps(t, []Dep{
		{Name: "git", Bins: []string{"git"}},
		{Name: "fd", Bins: []string{"fd"}, Pkgs: map[string][]string{"apt": {"fd-find"}}},
		{Name: "node", Bins: []string{"node"}, Pkgs: map[string][]string{"apt": {"nodejs", "npm"}}},
	})
	withGOOS(t, "linux")

	t.Run("nothing missing", func(t *testing.T) {
		fakePath(t, "git", "fd", "node")
		if err := NewPlan(Needed("full")).Err(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("uses the manager's package names", func(t *testing.T) {
		fakePath(t, "git", "apt-get")
		err := NewPlan(Needed("full")).Err()
		var missing *MissingError
		if !errors.As(err, &missing) {
			t.Fatalf("err = %v, want *MissingError", err)
		}
		want := "missing system dependencies: fd, node\n" +
			"install them with:\n  sudo apt-get install -y fd-find nodejs npm\n" +
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
		if err := NewPlan(Needed("full")).Err(); err == nil || err.Error() != want {
			t.Fatalf("got:\n%v\nwant:\n%s", err, want)
		}
	})
}

func TestForBins(t *testing.T) {
	withDeps(t, []Dep{{Name: "gcc", Bins: []string{"cc", "gcc"}}})

	got := ForBins([]string{"go", "cc", "zig", "python3", "go"})
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	// in the order asked for: cc is a base dep, go comes once, and zig is a package named zig
	if !slices.Equal(names, []string{"go", "zig", "python3"}) {
		t.Fatalf("got %v", names)
	}
	apt := linuxManagers[1]
	if pkgs := got[2].Packages(apt); !slices.Equal(pkgs, []string{"python3", "python3-venv"}) {
		t.Errorf("python3 on apt = %v", pkgs)
	}
	if pkgs := got[1].Packages(apt); !slices.Equal(pkgs, []string{"zig"}) || !slices.Equal(got[1].Bins, []string{"zig"}) {
		t.Errorf("zig = %+v", got[1])
	}
}

func TestDepsOrder(t *testing.T) {
	// config sync and plugin restore need nvim and git first
	if len(Deps) < 2 || Deps[0].Bins[0] != "nvim" || Deps[1].Bins[0] != "git" {
		t.Fatalf("nvim and git must be the first two deps")
	}
}

func TestNeeded(t *testing.T) {
	withDeps(t, []Dep{{Name: "git"}, {Name: "node", FullOnly: true}, {Name: "rg"}})

	names := func(deps []Dep) []string {
		var out []string
		for _, d := range deps {
			out = append(out, d.Name)
		}
		return out
	}
	if got := names(Needed("full")); !slices.Equal(got, []string{"git", "node", "rg"}) {
		t.Errorf("full = %v", got)
	}
	if got := names(Needed("minimal")); !slices.Equal(got, []string{"git", "rg"}) {
		t.Errorf("minimal = %v", got)
	}
}

func TestManagerCommand(t *testing.T) {
	apt := linuxManagers[1]

	withRoot(t, false)
	if got := apt.Command([]string{"rg"}); !slices.Equal(got, []string{"sudo", "apt-get", "install", "-y", "rg"}) {
		t.Errorf("as a user: %q", got)
	}
	if got := brew.Command([]string{"rg"}); !slices.Equal(got, []string{"brew", "install", "rg"}) {
		t.Errorf("brew: %q", got)
	}

	withRoot(t, true)
	if got := apt.Command([]string{"rg"}); !slices.Equal(got, []string{"apt-get", "install", "-y", "rg"}) {
		t.Errorf("as root: %q", got)
	}
}

func withRun(t *testing.T, fn func(argv []string) error) {
	t.Helper()
	orig := run
	run = fn
	t.Cleanup(func() { run = orig })
}

func TestPlanRun(t *testing.T) {
	withRoot(t, false)
	withGOOS(t, "linux")
	withDeps(t, []Dep{
		{Name: "git", Bins: []string{"git"}},
		{Name: "fd", Bins: []string{"fd", "fdfind"}, Pkgs: map[string][]string{"apt": {"fd-find"}}},
		{Name: "node", Bins: []string{"node"}, FullOnly: true},
	})

	t.Run("installs what's missing", func(t *testing.T) {
		fakePath(t, "git", "apt-get")
		var got []string
		withRun(t, func(argv []string) error {
			got = argv
			// the package manager puts fdfind on PATH, as apt does
			return os.WriteFile(filepath.Join(os.Getenv("PATH"), "fdfind"), []byte("#!/bin/sh\n"), 0o755)
		})

		plan := NewPlan(Needed("minimal"))
		if err := plan.Run(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"sudo", "apt-get", "install", "-y", "fd-find"}) {
			t.Fatalf("ran %q", got)
		}
	})

	t.Run("still missing after install", func(t *testing.T) {
		fakePath(t, "git", "apt-get")
		withRun(t, func([]string) error { return nil })

		var missing *MissingError
		if err := NewPlan(Needed("minimal")).Run(); !errors.As(err, &missing) || missing.Deps[0].Name != "fd" {
			t.Fatalf("err = %v, want fd still missing", err)
		}
	})

	t.Run("install fails", func(t *testing.T) {
		fakePath(t, "git", "apt-get")
		withRun(t, func([]string) error { return errors.New("exit status 100") })

		err := NewPlan(Needed("minimal")).Run()
		if err == nil || err.Error() != "sudo apt-get install -y fd-find: exit status 100" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no package manager", func(t *testing.T) {
		fakePath(t, "git")
		withRun(t, func([]string) error { t.Fatal("nothing should run"); return nil })

		plan := NewPlan(Needed("minimal"))
		if plan.Command() != nil {
			t.Fatalf("command = %q, want none", plan.Command())
		}
		var missing *MissingError
		if err := plan.Run(); !errors.As(err, &missing) || missing.Found {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("nothing missing", func(t *testing.T) {
		fakePath(t, "git", "fd", "apt-get")
		withRun(t, func([]string) error { t.Fatal("nothing should run"); return nil })

		if err := NewPlan(Needed("minimal")).Run(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDetectTerminals(t *testing.T) {
	apps := t.TempDir()
	orig := appDirs
	appDirs = func() []string { return []string{apps} }
	t.Cleanup(func() { appDirs = orig })

	t.Run("on PATH", func(t *testing.T) {
		withGOOS(t, "linux")
		fakePath(t, "ghostty")
		if got := DetectTerminals(); !slices.Equal(got, []string{"ghostty"}) {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("mac app bundle", func(t *testing.T) {
		withGOOS(t, "darwin")
		fakePath(t)
		if err := os.Mkdir(filepath.Join(apps, "kitty.app"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := DetectTerminals(); !slices.Equal(got, []string{"kitty"}) {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("app bundles only count on macOS", func(t *testing.T) {
		withGOOS(t, "linux")
		fakePath(t)
		if got := DetectTerminals(); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
}

func TestReadReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install.json")

	if r, err := ReadReceipt(path); r != nil || err != nil {
		t.Fatalf("missing: got %+v, %v", r, err)
	}

	write := func(data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"method": "install.sh", "path": "/home/me/.local/bin/bud"}`)
	if r, err := ReadReceipt(path); err != nil || r.Path != "/home/me/.local/bin/bud" || r.Method != "install.sh" {
		t.Fatalf("got %+v, %v", r, err)
	}

	for _, bad := range []string{`{"path": "relative/bud"}`, `{"path": "/etc/passwd"}`, `{"path": ""}`, `not json`} {
		write(bad)
		if _, err := ReadReceipt(path); err == nil {
			t.Errorf("%s: expected an error", bad)
		}
	}
}
