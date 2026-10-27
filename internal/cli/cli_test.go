package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// recorder is a command that captures the args it was run with
type recorder struct {
	called bool
	args   []string
}

func (r *recorder) cmd(name string) *Command {
	return &Command{
		Name: name,
		Run: func(_ *Globals, args []string) error {
			r.called = true
			r.args = args
			return nil
		},
	}
}

func TestRegistry(t *testing.T) {
	for _, name := range []string{"open", "chat", "install", "update", "version", "doctor", "uninstall", "lang"} {
		if _, ok := CommandRegistry[name]; !ok {
			t.Errorf("%s not registered", name)
		}
	}
}

func TestRegisterCommandTwicePanics(t *testing.T) {
	orig := CommandRegistry
	CommandRegistry = map[string]*Command{}
	t.Cleanup(func() { CommandRegistry = orig })

	RegisterCommand(&Command{Name: "dup"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	RegisterCommand(&Command{Name: "dup"})
}

func TestAppRun(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCmd  string
		wantArgs []string
	}{
		{"no args defaults to open", nil, "open", []string{}},
		{"path defaults to open", []string{"some/dir"}, "open", []string{"some/dir"}},
		{"known command", []string{"chat"}, "chat", []string{}},
		{"command with args", []string{"chat", "a", "b"}, "chat", []string{"a", "b"}},
		{"explicit open", []string{"open", "x"}, "open", []string{"x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			open, chat := &recorder{}, &recorder{}
			app := &App{
				Cmds: map[string]*Command{"open": open.cmd("open"), "chat": chat.cmd("chat")},
				Args: tt.args,
			}
			if err := app.Run(); err != nil {
				t.Fatal(err)
			}

			got := map[string]*recorder{"open": open, "chat": chat}[tt.wantCmd]
			if !got.called {
				t.Fatalf("%s was not run", tt.wantCmd)
			}
			if !slices.Equal(got.args, tt.wantArgs) {
				t.Fatalf("args = %q, want %q", got.args, tt.wantArgs)
			}
		})
	}
}

func TestAppRunCommandFlags(t *testing.T) {
	var verbose bool
	var gotArgs []string
	app := &App{
		Cmds: map[string]*Command{
			"open": (&recorder{}).cmd("open"),
			"x": {
				Name:  "x",
				Flags: func(fs *flag.FlagSet) { fs.BoolVar(&verbose, "v", false, "") },
				Run:   func(_ *Globals, args []string) error { gotArgs = args; return nil },
			},
		},
		Args: []string{"x", "-v", "rest"},
	}
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if !verbose || !slices.Equal(gotArgs, []string{"rest"}) {
		t.Fatalf("verbose = %v, args = %q", verbose, gotArgs)
	}
}

func TestAppRunErrors(t *testing.T) {
	boom := errors.New("boom")
	cmds := map[string]*Command{
		"open": {Name: "open", Run: func(*Globals, []string) error { return boom }},
	}

	if err := (&App{Cmds: cmds}).Run(); !errors.Is(err, boom) {
		t.Errorf("run error = %v, want boom", err)
	}
	if err := (&App{Cmds: cmds, Args: []string{"-nope"}, Stderr: io.Discard}).Run(); err == nil {
		t.Error("expected error for unknown global flag")
	}
}

func TestOpenMissingPaths(t *testing.T) {
	if err := open(&Globals{}, []string{"a", "b"}); err == nil {
		t.Fatal("expected error for paths that don't exist")
	}
}

// touch creates each file under dir, with parent dirs, and returns their paths
func touch(t *testing.T, dir string, names ...string) []string {
	t.Helper()
	var out []string
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, path)
	}

	return out
}

func TestResolveWorkspace(t *testing.T) {
	// a project with a .git dir, and a loose dir outside any project
	root := t.TempDir()
	project := filepath.Join(root, "project")
	loose := filepath.Join(root, "loose")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := touch(t, project, "main.go", "pkg/a/a.go", "pkg/b/b.go")
	looseFile := touch(t, loose, "notes.txt")[0]

	t.Run("no args is the current dir", func(t *testing.T) {
		t.Chdir(loose)
		ws, err := resolveWorkspace(nil)
		if err != nil {
			t.Fatal(err)
		}
		if want, _ := filepath.EvalSymlinks(loose); evalSymlinks(t, ws.Root) != want || len(ws.Files) != 0 {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("dir", func(t *testing.T) {
		ws, err := resolveWorkspace([]string{project})
		if err != nil {
			t.Fatal(err)
		}
		if ws.Root != project || len(ws.Files) != 0 {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("file finds the git root", func(t *testing.T) {
		ws, err := resolveWorkspace([]string{files[1]})
		if err != nil {
			t.Fatal(err)
		}
		if ws.Root != project || !slices.Equal(ws.Files, files[1:2]) {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("several files keep their order", func(t *testing.T) {
		args := []string{files[2], files[0], files[1]}
		ws, err := resolveWorkspace(args)
		if err != nil {
			t.Fatal(err)
		}
		if ws.Root != project || !slices.Equal(ws.Files, args) {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("git file in a worktree counts", func(t *testing.T) {
		wt := filepath.Join(root, "worktree")
		f := touch(t, wt, ".git", "src/x.go")[1]
		ws, err := resolveWorkspace([]string{f})
		if err != nil {
			t.Fatal(err)
		}
		if ws.Root != wt {
			t.Fatalf("root = %q, want %q", ws.Root, wt)
		}
	})

	t.Run("no project falls back to the file's dir", func(t *testing.T) {
		ws, err := resolveWorkspace([]string{looseFile})
		if err != nil {
			t.Fatal(err)
		}
		// t.TempDir is never inside a repo, so the walk reaches / and gives up
		if ws.Root != loose {
			t.Fatalf("root = %q, want %q", ws.Root, loose)
		}
	})

	t.Run("relative", func(t *testing.T) {
		t.Chdir(project)
		ws, err := resolveWorkspace([]string{"main.go"})
		if err != nil {
			t.Fatal(err)
		}
		if len(ws.Files) != 1 || !filepath.IsAbs(ws.Files[0]) || filepath.Base(ws.Files[0]) != "main.go" {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("dir and files together", func(t *testing.T) {
		var usage *UsageError
		if _, err := resolveWorkspace([]string{project, files[0]}); !errors.As(err, &usage) {
			t.Fatalf("err = %v, want UsageError", err)
		}
		if _, err := resolveWorkspace([]string{project, loose}); !errors.As(err, &usage) {
			t.Fatalf("two dirs: err = %v, want UsageError", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := resolveWorkspace([]string{filepath.Join(root, "nope")})
		if err == nil || !strings.Contains(err.Error(), "not a command or an existing path") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("flag after the path", func(t *testing.T) {
		t.Chdir(project)
		_, err := resolveWorkspace([]string{".", "--layout", "focus"})
		if err == nil || !strings.Contains(err.Error(), "flags go before the path: bud --layout focus .") {
			t.Fatalf("err = %v", err)
		}
	})
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	// macOS temp dirs live behind the /var -> /private/var symlink, and Getwd reports the real path
	out, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func TestAppRunGlobals(t *testing.T) {
	var got *Globals
	cmds := map[string]*Command{
		"open":   {Name: "open", Run: func(g *Globals, _ []string) error { got = g; return nil }},
		"update": {Name: "update", Run: func(g *Globals, _ []string) error { got = g; return nil }},
	}

	app := &App{Cmds: cmds, Args: []string{"--verbose", "--no-update", "--profile", "minimal", "--layout", "focus", "."}}
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if !got.Verbose || !got.NoUpdate || got.Profile != "minimal" || got.Layout != "focus" {
		t.Fatalf("globals = %+v", got)
	}
}

func TestAppRunGlobalsErrors(t *testing.T) {
	cmds := map[string]*Command{
		"open":   (&recorder{}).cmd("open"),
		"update": (&recorder{}).cmd("update"),
	}

	tests := []struct {
		name string
		args []string
	}{
		{"unknown profile", []string{"--profile", "huge", "update"}},
		{"layout on another command", []string{"--layout", "focus", "update"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&App{Cmds: cmds, Args: tt.args, Stderr: io.Discard}).Run()
			var usage *UsageError
			if !errors.As(err, &usage) {
				t.Fatalf("err = %v, want UsageError", err)
			}
		})
	}
}

func TestFlagOrderHint(t *testing.T) {
	var stderr bytes.Buffer
	cmds := map[string]*Command{
		"open":   (&recorder{}).cmd("open"),
		"update": (&recorder{}).cmd("update"),
	}
	err := (&App{Cmds: cmds, Args: []string{"update", "--verbose"}, Stderr: &stderr}).Run()
	if err == nil {
		t.Fatal("expected a usage error")
	}
	if want := "--verbose is a global flag, so it goes before the command: bud --verbose update ...\n"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestGlobalsLogf(t *testing.T) {
	var buf bytes.Buffer
	(&Globals{stderr: &buf}).Logf("quiet %d", 1)
	(&Globals{Verbose: true, stderr: &buf}).Logf("loud %d", 2)
	if buf.String() != "bud: loud 2\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestAppRunSubcommands(t *testing.T) {
	var rec recorder
	group := &Command{Name: "lang", Subs: []*Command{rec.cmd("enable")}}
	app := func(args ...string) *App {
		return &App{Cmds: map[string]*Command{"lang": group, "open": (&recorder{}).cmd("open")}, Args: args, Stdout: io.Discard, Stderr: io.Discard}
	}

	if err := app("lang", "enable", "go", "zig").Run(); err != nil || !rec.called || !slices.Equal(rec.args, []string{"go", "zig"}) {
		t.Fatalf("enable: err %v, called %v, args %v", err, rec.called, rec.args)
	}

	var usage *UsageError
	if err := app("lang").Run(); !errors.As(err, &usage) || !strings.Contains(err.Error(), "needs a command: enable") {
		t.Errorf("no subcommand: %v", err)
	}
	if err := app("lang", "nope").Run(); !errors.As(err, &usage) || !strings.Contains(err.Error(), `unknown lang command "nope"`) {
		t.Errorf("unknown subcommand: %v", err)
	}

	// help lists the subcommands
	var out bytes.Buffer
	a := app("lang", "-h")
	a.Stdout = &out
	if err := a.Run(); !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "Commands:\n  enable") {
		t.Errorf("help: %v\n%s", err, out.String())
	}
}
