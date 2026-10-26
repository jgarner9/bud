package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"jgarner.dev/bud/internal/nvim"
)

func init() {
	RegisterCommand(Open())
}

func Open() *Command {
	cmd := &Command{
		Name:  "open",
		Usage: "Opens Bud to the dashboard",
		Flags: nil,
		Run:   open,
		Subs:  nil,
	}

	return cmd
}

func open(args []string) error {
	// default to the current directory, same as `bud .`
	target := "."
	if len(args) > 1 {
		return usageErrorf("open takes at most one path")
	}
	if len(args) == 1 {
		target = args[0]
	}

	ws, err := resolveWorkspace(target)
	if err != nil {
		return err
	}

	return nvim.Launch(ws.Root, ws.File)
}

func resolveWorkspace(target string) (*Workspace, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, usageErrorf("%q is not a command or an existing path", target)
	}
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return &Workspace{Root: abs}, nil
	}

	return &Workspace{Root: filepath.Dir(abs), File: abs}, nil
}
