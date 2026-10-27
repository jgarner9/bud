package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/launcher"
)

type launched struct {
	terminal string
	window   launcher.Window
}

// fakeLaunch records launches instead of opening windows, with found as the
// installed terminals
func fakeLaunch(t *testing.T, found ...string) *[]launched {
	t.Helper()
	var got []launched
	origWindow, origTerms := launchWindow, launchTerminals
	launchWindow = func(name string, w launcher.Window) error {
		got = append(got, launched{name, w})
		return nil
	}
	launchTerminals = func() []string { return found }
	t.Cleanup(func() { launchWindow, launchTerminals = origWindow, origTerms })

	return &got
}

func TestLaunch(t *testing.T) {
	p := layoutHome(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	os.WriteFile(file, nil, 0o644)

	t.Run("a project in the first installed terminal", func(t *testing.T) {
		got := fakeLaunch(t, "ghostty")
		if err := launch(&Globals{NoUpdate: true}, []string{dir}, "", "focus"); err != nil {
			t.Fatal(err)
		}
		l := (*got)[0]
		self, _ := os.Executable()
		want := []string{"/usr/bin/env", "PATH=" + os.Getenv("PATH"), self, "--no-update", "--layout", "focus", "open", dir}
		if l.terminal != "ghostty" || l.window.Dir != dir || !slices.Equal(l.window.Argv, want) {
			t.Fatalf("launched %+v", l)
		}
	})

	t.Run("a file opens from its dir, with the global --layout", func(t *testing.T) {
		got := fakeLaunch(t, "kitty")
		if err := launch(&Globals{Layout: "default"}, []string{file}, "", ""); err != nil {
			t.Fatal(err)
		}
		argv := (*got)[0].window.Argv
		if (*got)[0].window.Dir != dir || !slices.Equal(argv[len(argv)-4:], []string{"--layout", "default", "open", file}) {
			t.Fatalf("launched %+v", (*got)[0])
		}
	})

	t.Run("no path opens the dashboard", func(t *testing.T) {
		got := fakeLaunch(t, "kitty")
		if err := launch(&Globals{}, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if argv := (*got)[0].window.Argv; argv[len(argv)-1] != "open" {
			t.Fatalf("argv %q", argv)
		}
	})

	t.Run("config.json's terminal wins over what's installed", func(t *testing.T) {
		os.MkdirAll(filepath.Dir(p.MachineConfig()), 0o755)
		if err := os.WriteFile(p.MachineConfig(), []byte(`{"terminal": "ghostty"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(p.MachineConfig()) })
		got := fakeLaunch(t, "kitty")
		if err := launch(&Globals{}, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if (*got)[0].terminal != "ghostty" {
			t.Fatalf("launched %s", (*got)[0].terminal)
		}
	})

	t.Run("refused before any window opens", func(t *testing.T) {
		got := fakeLaunch(t)
		var usage *UsageError
		for _, c := range []struct {
			args             []string
			terminal, layout string
			want             string
		}{
			{[]string{filepath.Join(dir, "nope")}, "", "", "not a command or an existing path"},
			{nil, "", "nope", `no layout "nope"`},
			{nil, "xterm", "", `--terminal "xterm"`},
		} {
			if err := launch(&Globals{}, c.args, c.terminal, c.layout); !errors.As(err, &usage) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%v: err = %v, want %q", c, err, c.want)
			}
		}
		if err := launch(&Globals{}, nil, "", ""); err == nil || !strings.Contains(err.Error(), "no terminal") {
			t.Errorf("nothing installed: err = %v", err)
		}
		if len(*got) > 0 {
			t.Fatalf("launched %+v", *got)
		}
	})
}
