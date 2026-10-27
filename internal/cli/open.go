package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
)

func init() {
	RegisterCommand(Open())
}

func Open() *Command {
	cmd := &Command{
		Name:  "open",
		Usage: "Opens a project directory, or one or more files",
		Flags: nil,
		Run:   open,
		Subs:  nil,
	}

	return cmd
}

func open(g *Globals, args []string) error {
	ws, err := resolveWorkspace(args)
	if err != nil {
		return err
	}
	g.Logf("workspace root %s, files %q", ws.Root, ws.Files)

	if ws.Project {
		// a registry that can't be written never stops the editor opening;
		// bud doctor reports a broken projects.json
		if err := registerProject(ws.Root, now()); err != nil {
			g.Logf("registering %s: %v", ws.Root, err)
		}
	}

	return nvim.Launch(ws.Root, ws.Files, g.Layout)
}

// swappable in tests
var now = time.Now

// registerProject adds dir to the registry, or updates when it was last opened
func registerProject(dir string, opened time.Time) error {
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	r, err := projects.Load(p.Projects())
	if err != nil {
		return err
	}
	if _, err := r.Add(dir, opened); err != nil {
		return err
	}

	return r.Save()
}

// resolveWorkspace turns open's args into where nvim runs and what it opens.
// No args is the current dir with nothing open (the dashboard, once it exists),
// one dir is a project, and files are opened from the project they're in
func resolveWorkspace(args []string) (*Workspace, error) {
	if len(args) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return &Workspace{Root: wd}, nil
	}

	var dirs, files []string
	for i, arg := range args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(abs)
		if errors.Is(err, fs.ErrNotExist) {
			if isGlobalFlag(arg) {
				// the flag (and its value) moved in front of the paths before it
				fixed := append(slices.Clone(args[i:]), args[:i]...)
				return nil, usageErrorf("flags go before the path: bud %s", strings.Join(fixed, " "))
			}
			return nil, usageErrorf("%q is not a command or an existing path", arg)
		}
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			dirs = append(dirs, abs)
		} else {
			files = append(files, abs)
		}
	}

	switch {
	case len(dirs) == 1 && len(files) == 0:
		return &Workspace{Root: dirs[0], Project: true}, nil
	case len(dirs) > 0:
		return nil, usageErrorf("open takes one directory, or one or more files")
	}

	// the first file picks the project, the same rule the Claude Code session uses (BASE.md 4.2)
	root, found := findRoot(filepath.Dir(files[0]))
	return &Workspace{Root: root, Files: files, Project: found}, nil
}

// rootMarkers mark a project root. Language specs add their own `root` markers
// in front of these once they exist
var rootMarkers = []string{".git"}

// findRoot walks up from dir to the nearest dir holding a root marker, or
// returns dir itself, and false, when there is none
func findRoot(dir string) (string, bool) {
	for cur := dir; ; {
		for _, marker := range rootMarkers {
			// .git is a file, not a dir, in worktrees and submodules, so any entry counts
			if _, err := os.Lstat(filepath.Join(cur, marker)); err == nil {
				return cur, true
			}
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			return dir, false
		}
		cur = parent
	}
}
