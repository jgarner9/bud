package launcher

import (
	"slices"
	"strings"
	"testing"
)

func withGOOS(t *testing.T, os string) {
	t.Helper()
	orig := goos
	goos = os
	t.Cleanup(func() { goos = orig })
}

var window = Window{Dir: "/src/app", Title: "bud", Argv: []string{"/usr/bin/env", "PATH=/bin", "/bin/bud", "open"}}

func TestCommand(t *testing.T) {
	cases := []struct {
		name, goos, term string
		found            Found
		want             []string
	}{
		{"kitty on linux", "linux", "kitty", Found{Bin: "/usr/bin/kitty"},
			[]string{"/usr/bin/kitty", "--directory=/src/app", "--title=bud", "/usr/bin/env", "PATH=/bin", "/bin/bud", "open"}},
		{"kitty app on macOS", "darwin", "kitty", Found{Bin: "/opt/homebrew/bin/kitty", App: "/Applications/kitty.app"},
			[]string{"/usr/bin/open", "-n", "-a", "/Applications/kitty.app", "--args", "--directory=/src/app", "--title=bud", "/usr/bin/env", "PATH=/bin", "/bin/bud", "open"}},
		{"kitty on macOS without the app", "darwin", "kitty", Found{Bin: "/opt/homebrew/bin/kitty"},
			[]string{"/opt/homebrew/bin/kitty", "--directory=/src/app", "--title=bud", "/usr/bin/env", "PATH=/bin", "/bin/bud", "open"}},
		{"ghostty on linux", "linux", "ghostty", Found{Bin: "/usr/bin/ghostty"},
			[]string{"/usr/bin/ghostty", "--working-directory=/src/app", "--title=bud", "-e", "/usr/bin/env", "PATH=/bin", "/bin/bud", "open"}},
		{"ghostty app on macOS", "darwin", "ghostty", Found{Bin: "/opt/homebrew/bin/ghostty", App: "/Applications/Ghostty.app"},
			[]string{"/usr/bin/open", "-n", "-a", "/Applications/Ghostty.app", "--args", "--working-directory=/src/app", "--title=bud", "-e", "/usr/bin/env", "PATH=/bin", "/bin/bud", "open"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withGOOS(t, c.goos)
			got, err := Command(c.term, window, c.found)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestCommandErrors(t *testing.T) {
	withGOOS(t, "darwin")
	for _, c := range []struct {
		term  string
		found Found
		want  string
	}{
		{"wezterm", Found{Bin: "/bin/wezterm"}, "can't launch wezterm"},
		{"kitty", Found{}, "kitty isn't installed"},
		{"ghostty", Found{Bin: "/opt/homebrew/bin/ghostty"}, "Ghostty.app"},
	} {
		if _, err := Command(c.term, window, c.found); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.term, err, c.want)
		}
	}
}
