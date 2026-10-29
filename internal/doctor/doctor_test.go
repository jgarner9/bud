package doctor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/lang"
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

func withLanguages(t *testing.T, set *lang.Set, err error) {
	t.Helper()
	orig := loadLanguages
	loadLanguages = func(bool, ...string) (*lang.Set, error) { return set, err }
	t.Cleanup(func() { loadLanguages = orig })
}

func TestCheckLanguages(t *testing.T) {
	// a synced config where go.lua is bud's and zig.lua the user's
	synced := func(t *testing.T) *paths.Paths {
		t.Helper()
		p := tempPaths(t)
		src := fstest.MapFS{"init.lua": {Data: []byte("1")}, "languages/go.lua": {Data: []byte("2")}}
		if _, err := nvim.SyncConfig(src, p.Config); err != nil {
			t.Fatal(err)
		}
		return p
	}
	specs := []lang.Spec{
		{Name: "go", Builtin: true, System: []string{"go"}},
		{Name: "python", Builtin: true, Minimal: true, System: []string{"python3"}},
	}

	t.Run("all good", func(t *testing.T) {
		fakePath(t, "nvim")
		withLanguages(t, &lang.Set{Specs: specs}, nil)
		results, enabled := checkLanguages(synced(t), config.Defaults())
		if len(results) != 1 || results[0].Status != OK || results[0].Detail != "go, python" {
			t.Fatalf("got %+v", results)
		}
		if len(enabled) != 2 || enabled[0].Name != "go" || enabled[1].Name != "python" {
			t.Fatalf("enabled = %+v", enabled)
		}
	})

	t.Run("broken shipped spec fails, broken user spec warns", func(t *testing.T) {
		fakePath(t, "nvim")
		withLanguages(t, &lang.Set{Errors: map[string]string{"go": "oops", "zig": "typo"}}, nil)
		results, _ := checkLanguages(synced(t), config.Settings{Profile: "full", Languages: []string{}})
		if len(results) != 3 || results[0].Detail != "none enabled" {
			t.Fatalf("got %+v", results)
		}
		if r := results[1]; r.Name != "language go" || r.Status != Fail || r.Fix != "bud update" {
			t.Errorf("go = %+v", r)
		}
		if r := results[2]; r.Name != "language zig" || r.Status != Warn || !strings.HasSuffix(r.Fix, "zig.lua") {
			t.Errorf("zig = %+v", r)
		}
	})

	t.Run("enabled name with no spec warns", func(t *testing.T) {
		fakePath(t, "nvim")
		withLanguages(t, &lang.Set{Specs: specs}, nil)
		results, _ := checkLanguages(synced(t), config.Settings{Profile: "full", Languages: []string{"go", "zig"}})
		if len(results) != 2 || results[1].Status != Warn || results[1].Detail != "no spec for zig" {
			t.Fatalf("got %+v", results)
		}
	})

	t.Run("specs can't be read", func(t *testing.T) {
		fakePath(t, "nvim")
		withLanguages(t, nil, errors.New("boom"))
		results, _ := checkLanguages(synced(t), config.Defaults())
		if len(results) != 1 || results[0].Status != Fail {
			t.Fatalf("got %+v", results)
		}
	})

	t.Run("skipped before the first sync or without nvim", func(t *testing.T) {
		withLanguages(t, nil, errors.New("must not be called"))
		fakePath(t, "nvim")
		if results, _ := checkLanguages(tempPaths(t), config.Defaults()); results != nil {
			t.Errorf("not synced: %+v", results)
		}
		fakePath(t)
		if results, _ := checkLanguages(synced(t), config.Defaults()); results != nil {
			t.Errorf("no nvim: %+v", results)
		}
	})
}

func TestCheckLanguageDeps(t *testing.T) {
	fakePath(t, "go")
	results := checkLanguageDeps([]installer.Dep{
		{Name: "go", Bins: []string{"go"}},
		{Name: "node", Bins: []string{"node"}},
	})
	if len(results) != 2 || results[0].Status != OK || results[1].Status != Warn || results[1].Fix == "" {
		t.Fatalf("got %+v", results)
	}
}

func withStartup(t *testing.T, times map[string]time.Duration) *[]string {
	t.Helper()
	var calls []string
	orig := startupTime
	startupTime = func(args ...string) (time.Duration, error) {
		key := strings.Join(args, " ")
		calls = append(calls, key)
		return times[key], nil
	}
	t.Cleanup(func() { startupTime = orig })
	return &calls
}

func TestCheckStartup(t *testing.T) {
	synced := func(t *testing.T) *paths.Paths {
		t.Helper()
		p := tempPaths(t)
		if _, err := nvim.SyncConfig(fstest.MapFS{"init.lua": {Data: []byte("1")}}, p.Config); err != nil {
			t.Fatal(err)
		}
		return p
	}
	specs := []lang.Spec{{Name: "go", Filetypes: []string{"go", "gomod"}}, {Name: "html", Filetypes: []string{"html"}}}
	ms := time.Millisecond

	t.Run("within budget", func(t *testing.T) {
		fakePath(t, "nvim")
		calls := withStartup(t, map[string]time.Duration{"": 28 * ms, "-c setfiletype go": 40 * ms, "-c setfiletype html": 45 * ms})
		r, ok := checkStartup(synced(t), specs)
		if !ok || r.Status != OK || r.Detail != "28 ms, slowest language html 45 ms" {
			t.Fatalf("got %+v", r)
		}
		// three for the median of an empty start, then one per language
		if len(*calls) != 5 {
			t.Errorf("ran nvim %d times: %v", len(*calls), *calls)
		}
	})

	t.Run("over budget warns, after measuring again", func(t *testing.T) {
		fakePath(t, "nvim")
		calls := withStartup(t, map[string]time.Duration{"": 28 * ms, "-c setfiletype go": 130 * ms})
		r, _ := checkStartup(synced(t), specs[:1])
		if r.Status != Warn || !strings.Contains(r.Detail, "go 130 ms, over the 50 ms budget") || r.Fix == "" {
			t.Fatalf("got %+v", r)
		}
		if len(*calls) != 3+1+3 {
			t.Errorf("ran nvim %d times: %v", len(*calls), *calls)
		}
	})

	t.Run("skipped before the first sync or without nvim", func(t *testing.T) {
		withStartup(t, nil)
		fakePath(t, "nvim")
		if _, ok := checkStartup(tempPaths(t), specs); ok {
			t.Error("not synced: expected a skip")
		}
		fakePath(t)
		if _, ok := checkStartup(synced(t), specs); ok {
			t.Error("no nvim: expected a skip")
		}
	})
}

func TestCheckProjects(t *testing.T) {
	p := tempPaths(t)
	if r := checkProjects(p); r.Status != OK || r.Detail != "0 projects registered" {
		t.Fatalf("empty: %+v", r)
	}

	dir := t.TempDir()
	doc := `{"projects": [{"name": "here", "path": "` + dir + `"}, {"name": "gone", "path": "` + filepath.Join(dir, "gone") + `"}]}`
	if err := os.MkdirAll(p.State, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Projects(), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := checkProjects(p); r.Status != OK || !strings.Contains(r.Detail, "2 projects registered, 1 missing") {
		t.Fatalf("got %+v", r)
	}

	if err := os.WriteFile(p.Projects(), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := checkProjects(p); r.Status != Warn || r.Fix == "" {
		t.Fatalf("corrupt: %+v", r)
	}
}

func TestCheckClaude(t *testing.T) {
	orig := claudeCheck
	t.Cleanup(func() { claudeCheck = orig })
	on := config.Defaults()
	off := config.Defaults()
	off.AI.Claude.Autostart = false

	for _, c := range []struct {
		name   string
		cfg    config.Settings
		status claude.Status
		err    error
		want   Status
		detail string
	}{
		{"logged in", on, claude.Status{Bin: "/c", Version: "2.1.0", LoggedIn: true, Method: "claude.ai"}, nil, OK, "2.1.0, logged in with claude.ai"},
		{"logged out", on, claude.Status{Bin: "/c", Version: "2.1.0"}, nil, Warn, "not logged in"},
		{"missing", on, claude.Status{}, nil, Warn, "not installed"},
		{"provider elsewhere", on, claude.Status{Bin: "/c", Version: "2.1.0", Elsewhere: "Amazon Bedrock"}, nil, Warn, "runs it on Amazon Bedrock"},
		{"missing and not wanted", off, claude.Status{}, nil, OK, "autostart is off"},
		{"broken", on, claude.Status{Bin: "/c"}, errors.New("boom"), Warn, "boom"},
	} {
		claudeCheck = func() (claude.Status, error) { return c.status, c.err }
		r := checkClaude(c.cfg)
		if r.Status != c.want || !strings.Contains(r.Detail, c.detail) {
			t.Errorf("%s: got %v %q", c.name, r.Status, r.Detail)
		}
		want := "bud claude setup"
		if c.status.Elsewhere != "" {
			want = "bud config set ai.claude.config home"
		}
		if r.Status == Warn && c.err == nil && r.Fix != want {
			t.Errorf("%s: fix %q", c.name, r.Fix)
		}
	}
}

func TestCheckClaudeCommand(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	cfg := config.Defaults()
	if _, ok := checkClaudeCommand(cfg); ok {
		t.Fatal("checked with no command set")
	}

	cfg.AI.Claude.Command = []string{"headroom", "wrap", "claude", "--"}
	lookPath = func(string) (string, error) { return "/bin/headroom", nil }
	if r, ok := checkClaudeCommand(cfg); !ok || r.Status != OK || !strings.Contains(r.Detail, "headroom wrap claude") {
		t.Fatalf("present: %+v", r)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if r, _ := checkClaudeCommand(cfg); r.Status != Warn || r.Fix != "bud config unset ai.claude.command" {
		t.Fatalf("missing: %+v", r)
	}
}
