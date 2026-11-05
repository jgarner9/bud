package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/claude"
	"jgarner.dev/bud/internal/updater"
)

func TestColorOnlyOnTerminals(t *testing.T) {
	var buf bytes.Buffer
	if got := paint(&buf, ansiRed, "bud:"); got != "bud:" {
		t.Fatalf("a buffer got colour: %q", got)
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := paint(f, ansiRed, "bud:"); got != "bud:" {
		t.Fatalf("a file got colour: %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	if colorOn(os.Stdout) {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestStatusStyle(t *testing.T) {
	for status, want := range map[string]string{"ok": ansiGreen, "warn": ansiYellow, "fail": ansiRed} {
		if got := statusStyle(status); got != want {
			t.Errorf("%s: %q", status, got)
		}
	}
}

var ansiCode = regexp.MustCompile("\x1b\\[[0-9;]*m")

// colourMatchesPlain runs fn plain, then with colour forced on, and checks the
// colour is there and that taking it out gives back the plain output byte for byte
func colourMatchesPlain(t *testing.T, name string, fn func() error) {
	t.Helper()
	run := func() string {
		var err error
		out := captureStdout(t, func() { err = fn() })
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	plain := run()
	orig := colorOn
	colorOn = func(io.Writer) bool { return true }
	coloured := run()
	colorOn = orig
	if !strings.Contains(coloured, ansiReset) {
		t.Errorf("%s: no colour in %q", name, coloured)
	}
	if got := ansiCode.ReplaceAllString(coloured, ""); got != plain {
		t.Errorf("%s: without its colour\n%q\nisn't the plain output\n%q", name, got, plain)
	}
}

func TestCell(t *testing.T) {
	var buf bytes.Buffer
	for _, s := range []string{"go", "naïve", "longer than the column"} {
		if got, want := cell(&buf, ansiBold, s, 8), fmt.Sprintf("%-8s", s); got != want {
			t.Errorf("cell(%q) = %q, want %q", s, got, want)
		}
	}
	orig := colorOn
	t.Cleanup(func() { colorOn = orig })
	colorOn = func(io.Writer) bool { return true }
	if got := cell(&buf, ansiBold, "go", 4); got != ansiBold+"go"+ansiReset+"  " {
		t.Errorf("the padding went inside the colour: %q", got)
	}
}

func TestPaintJSONKeys(t *testing.T) {
	in := "{\n  \"a\\\": b\": {\n    \"list\": [\n      \"x: y\"\n    ]\n  },\n  \"n\": 1\n}"
	var buf bytes.Buffer
	if got := paintJSONKeys(&buf, in); got != in {
		t.Fatalf("a buffer got colour: %q", got)
	}
	orig := colorOn
	t.Cleanup(func() { colorOn = orig })
	colorOn = func(io.Writer) bool { return true }
	got := paintJSONKeys(&buf, in)
	for _, want := range []string{ansiBold + `"a\": b"` + ansiReset + ": {", ansiBold + `"list"` + ansiReset, ansiBold + `"n"` + ansiReset + ": 1", "      \"x: y\"\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}

// JGA-159: the list and status commands colour on a terminal and print the
// same text as before everywhere else
func TestListsColourLikePlain(t *testing.T) {
	t.Run("tools ls", func(t *testing.T) {
		toolsHome(t)
		captureStdout(t, func() { toolsDisable([]string{"mine"}, "boom") })
		colourMatchesPlain(t, "tools ls", func() error { return toolsLs(nil) })
	})
	t.Run("layout ls", func(t *testing.T) {
		layoutHome(t)
		colourMatchesPlain(t, "layout ls", func() error { return layoutLs(nil) })
	})
	t.Run("projects", func(t *testing.T) {
		withState(t)
		root := t.TempDir()
		for _, d := range []string{"a", "b"} {
			os.MkdirAll(filepath.Join(root, d), 0o755)
		}
		captureStdout(t, func() {
			projectsAdd([]string{filepath.Join(root, "a"), filepath.Join(root, "b")}, true)
			projectsPin([]string{"b"}, true)
		})
		os.Remove(filepath.Join(root, "a"))
		colourMatchesPlain(t, "projects", func() error { return projectsLs(nil) })
	})
	t.Run("kit", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		fakeKit(t, fstest.MapFS{"kit.json": {Data: []byte(`{"marketplaces": {"m": "https://example.com/m.git"}, "plugins": ["docs@m"]}`)}})
		colourMatchesPlain(t, "kit ls", func() error { return kitLs(nil) })
		colourMatchesPlain(t, "kit status", func() error { return kitStatus(nil) })
	})
	t.Run("config", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		colourMatchesPlain(t, "config ls", func() error { return configLs(nil, false) })
	})
	t.Run("claude status", func(t *testing.T) {
		for _, s := range []claude.Status{claudeMissing, claudeLoggedOut, claudeReady, claudeElsewhere} {
			fakeClaude(t, s)
			colourMatchesPlain(t, "claude status", func() error { return claudeStatus(nil) })
		}
	})
	t.Run("update --check", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		orig := checkRelease
		t.Cleanup(func() { checkRelease = orig })
		for _, st := range []*updater.Status{
			{Current: "0.5.1", Newer: true, Release: &updater.Release{Version: "0.6.0"}},
			{Current: "0.6.0", Release: &updater.Release{Version: "0.6.0"}},
			{Current: "dev", Dev: true, Release: &updater.Release{Version: "0.6.0"}},
		} {
			checkRelease = func(context.Context, string, string) (*updater.Status, error) { return st, nil }
			colourMatchesPlain(t, "update --check", func() error { return update(&Globals{}, nil, true, false, false, "") })
		}
	})
}
