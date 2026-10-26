package cli

import (
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
		Run: func(args []string) error {
			r.called = true
			r.args = args
			return nil
		},
	}
}

func TestRegistry(t *testing.T) {
	for _, name := range []string{"open", "chat", "install", "update"} {
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
				Run:   func(args []string) error { gotArgs = args; return nil },
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
		"open": {Name: "open", Run: func([]string) error { return boom }},
	}

	if err := (&App{Cmds: cmds}).Run(); !errors.Is(err, boom) {
		t.Errorf("run error = %v, want boom", err)
	}
	if err := (&App{Cmds: cmds, Args: []string{"-nope"}, Stderr: io.Discard}).Run(); err == nil {
		t.Error("expected error for unknown global flag")
	}
}

func TestOpenTooManyArgs(t *testing.T) {
	if err := open([]string{"a", "b"}); err == nil {
		t.Fatal("expected error for two paths")
	}
}

func TestResolveWorkspace(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("dir", func(t *testing.T) {
		ws, err := resolveWorkspace(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ws.Root != dir || ws.File != "" {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("file", func(t *testing.T) {
		ws, err := resolveWorkspace(file)
		if err != nil {
			t.Fatal(err)
		}
		if ws.Root != dir || ws.File != file {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("relative", func(t *testing.T) {
		t.Chdir(dir)
		ws, err := resolveWorkspace("main.go")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(ws.File) != "main.go" || !filepath.IsAbs(ws.Root) {
			t.Fatalf("got %+v", ws)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := resolveWorkspace(filepath.Join(dir, "nope"))
		if err == nil || !strings.Contains(err.Error(), "not a command or an existing path") {
			t.Fatalf("err = %v", err)
		}
	})
}
