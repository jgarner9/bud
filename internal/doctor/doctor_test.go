package doctor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
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

func withDeps(t *testing.T, deps []installer.Dep) {
	t.Helper()
	orig := installer.Deps
	installer.Deps = deps
	t.Cleanup(func() { installer.Deps = orig })
}

func withNvimVersion(t *testing.T, out string, err error) {
	t.Helper()
	orig := nvimVersion
	nvimVersion = func() (string, error) { return out, err }
	t.Cleanup(func() { nvimVersion = orig })
}

func withEmbedded(t *testing.T, src fs.FS) {
	t.Helper()
	orig := embedded
	embedded = func() fs.FS { return src }
	t.Cleanup(func() { embedded = orig })
}

// tempPaths points every XDG dir into a fresh temp dir
func tempPaths(t *testing.T) *paths.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestCheckTreeSitter(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		err    error
		status Status
		detail string
	}{
		{"oldest supported", "tree-sitter 0.26.1\n", nil, OK, "0.26.1"},
		{"newer, with a hash", "tree-sitter 0.27.1 (6bd5d5d1e1e1)\n", nil, OK, "0.27.1"},
		{"too old", "tree-sitter 0.22.6\n", nil, Warn, "0.22.6, parsers need 0.26.1 or newer"},
		{"unreadable", "something else\n", nil, Warn, `can't read the version from "something else"`},
		{"won't run", "", errors.New("exit status 1"), Warn, "tree-sitter --version: exit status 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakePath(t, "tree-sitter")
			orig := treeSitterVersion
			treeSitterVersion = func() (string, error) { return tt.out, tt.err }
			t.Cleanup(func() { treeSitterVersion = orig })

			r, ok := checkTreeSitter()
			if !ok {
				t.Fatal("check was skipped")
			}
			if r.Status != tt.status || r.Detail != tt.detail {
				t.Fatalf("got %v %q, want %v %q", r.Status, r.Detail, tt.status, tt.detail)
			}
		})
	}

	t.Run("skipped without tree-sitter", func(t *testing.T) {
		fakePath(t)
		if _, ok := checkTreeSitter(); ok {
			t.Fatal("expected the check to be skipped")
		}
	})
}

func TestParseNvimVersion(t *testing.T) {
	tests := []struct {
		out          string
		version      string
		major, minor int
		ok           bool
	}{
		{"NVIM v0.11.4\nBuild type: Release\n", "v0.11.4", 0, 11, true},
		{"NVIM v0.12.0-dev-1234+gabcdef\n", "v0.12.0-dev-1234+gabcdef", 0, 12, true},
		{"NVIM v1.0.0", "v1.0.0", 1, 0, true},
		{"VIM - Vi IMproved 9.1", "", 0, 0, false},
		{"", "", 0, 0, false},
	}

	for _, tt := range tests {
		version, major, minor, ok := parseNvimVersion(tt.out)
		if version != tt.version || major != tt.major || minor != tt.minor || ok != tt.ok {
			t.Errorf("%q: got %q %d.%d %v", tt.out, version, major, minor, ok)
		}
	}
}

func TestCheckNeovim(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		err    error
		status Status
	}{
		{"oldest supported", "NVIM v0.12.0\n", nil, OK},
		{"newer", "NVIM v0.13.1\n", nil, OK},
		{"too old", "NVIM v0.11.7\n", nil, Fail},
		{"unreadable", "something else\n", nil, Fail},
		{"won't run", "", errors.New("exit status 1"), Fail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakePath(t, "nvim")
			withNvimVersion(t, tt.out, tt.err)

			r, ok := checkNeovim()
			if !ok {
				t.Fatal("check was skipped")
			}
			if r.Status != tt.status {
				t.Fatalf("status = %v, want %v (%s)", r.Status, tt.status, r.Detail)
			}
		})
	}

	t.Run("skipped without nvim", func(t *testing.T) {
		fakePath(t)
		if _, ok := checkNeovim(); ok {
			t.Fatal("expected the check to be skipped")
		}
	})
}

func TestCheckDeps(t *testing.T) {
	fakePath(t, "here", "brew")
	withDeps(t, []installer.Dep{
		{Name: "here", Bins: []string{"here"}},
		{Name: "gone", Bins: []string{"gone"}},
		{Name: "desktop", Bins: []string{"desktop"}, FullOnly: true},
	})

	if results := checkDeps("full"); len(results) != 3 {
		t.Fatalf("full profile checked %d deps, want 3", len(results))
	}
	results := checkDeps("minimal")
	if len(results) != 2 || results[0].Status != OK || results[1].Status != Fail {
		t.Fatalf("got %+v", results)
	}
	// on linux the fake brew isn't a candidate, so only check that a fix is offered
	if results[1].Fix == "" {
		t.Error("missing dep has no fix")
	}
}

func TestCheckConfigSync(t *testing.T) {
	src := fstest.MapFS{
		"init.lua":         {Data: []byte("1")},
		"lua/bud/core.lua": {Data: []byte("2")},
	}
	withEmbedded(t, src)

	t.Run("not installed", func(t *testing.T) {
		p := tempPaths(t)
		if r := checkConfigSync(p); r.Status != Fail || r.Fix != "bud install" {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("in sync", func(t *testing.T) {
		p := tempPaths(t)
		if _, err := nvim.SyncConfig(src, p.Config); err != nil {
			t.Fatal(err)
		}
		if r := checkConfigSync(p); r.Status != OK {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("edited", func(t *testing.T) {
		p := tempPaths(t)
		if _, err := nvim.SyncConfig(src, p.Config); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.Config, "init.lua"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if r := checkConfigSync(p); r.Status != Warn || !strings.Contains(r.Detail, "changed by hand") {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		p := tempPaths(t)
		if _, err := nvim.SyncConfig(src, p.Config); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(p.Config, "lua", "bud", "core.lua")); err != nil {
			t.Fatal(err)
		}
		if r := checkConfigSync(p); r.Status != Fail || r.Fix != "bud update" {
			t.Fatalf("got %+v", r)
		}
	})
}

func TestCheckMachineConfig(t *testing.T) {
	p := tempPaths(t)
	if _, r := checkMachineConfig(p); r.Status != OK {
		t.Fatalf("no config.json: got %+v", r)
	}

	if err := os.MkdirAll(p.State, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.MachineConfig(), []byte(`{"update": {"channel": "beta"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, r := checkMachineConfig(p)
	if r.Status != Fail {
		t.Fatalf("bad channel: got %+v", r)
	}
	if s.Profile != "full" {
		t.Fatalf("a broken config should fall back to the defaults, got %+v", s)
	}
}

func TestFailed(t *testing.T) {
	results := []Result{{Status: OK}, {Status: Warn}, {Status: Fail}, {Status: Fail}}
	if n := Failed(results); n != 2 {
		t.Fatalf("Failed = %d, want 2", n)
	}
}
