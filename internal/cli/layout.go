package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
)

func init() {
	RegisterCommand(Layout())
}

// Layout lists the pane layouts and picks which one projects open in (D45):
// the default in config.json, or one project's own in projects.json
func Layout() *Command {
	return &Command{
		Name:  "layout",
		Usage: "Lists pane layouts and picks the one projects open in",
		Subs: []*Command{
			{Name: "ls", Usage: "list the layouts, marking the default and this project's", Run: func(_ *Globals, args []string) error { return layoutLs(args) }},
			{Name: "set-default", Usage: "set the layout projects open in: set-default <name>", Run: func(_ *Globals, args []string) error { return layoutSetDefault(args) }},
			{Name: "set", Usage: "set one project's layout: set <name> [project] (default: the current dir)", Run: func(_ *Globals, args []string) error { return layoutSet(args) }},
			{Name: "unset", Usage: "send a project back to the default layout: unset [project]", Run: func(_ *Globals, args []string) error { return layoutUnset(args) }},
		},
	}
}

type layoutInfo struct {
	Name, Description string
}

var descriptionPattern = regexp.MustCompile(`(?m)^\s*description\s*=\s*"([^"]*)"`)

// listLayouts reads the synced layouts/ dir. The description is read from the
// file's text: running each through nvim isn't worth it for one string
func listLayouts(p *paths.Paths) ([]layoutInfo, error) {
	entries, err := os.ReadDir(p.Layouts())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no layouts in %s yet; run bud update", p.Layouts())
	}
	if err != nil {
		return nil, err
	}

	var out []layoutInfo
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".lua")
		if !ok {
			continue
		}
		info := layoutInfo{Name: name}
		if data, err := os.ReadFile(p.Layouts() + "/" + e.Name()); err == nil {
			if m := descriptionPattern.FindSubmatch(data); m != nil {
				info.Description = string(m[1])
			}
		}
		out = append(out, info)
	}

	return out, nil
}

// layoutEnv is what the layout commands share: paths, config.json and the
// layout names
type layoutEnv struct {
	p       *paths.Paths
	f       *config.File
	s       config.Settings
	layouts []layoutInfo
}

func loadLayoutEnv() (*layoutEnv, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return nil, err
	}
	s, err := f.Settings()
	if err != nil {
		return nil, err
	}
	layouts, err := listLayouts(p)
	if err != nil {
		return nil, err
	}

	return &layoutEnv{p, f, s, layouts}, nil
}

// known checks name is a layout, as a usage error naming the ones there are
func (e *layoutEnv) known(name string) error {
	var names []string
	for _, l := range e.layouts {
		if l.Name == name {
			return nil
		}
		names = append(names, l.Name)
	}

	return usageErrorf("no layout %q (have %s)", name, strings.Join(names, ", "))
}

func layoutLs(args []string) error {
	if len(args) > 0 {
		return usageErrorf("layout ls takes no arguments")
	}
	e, err := loadLayoutEnv()
	if err != nil {
		return err
	}
	r, err := projects.Load(e.p.Projects())
	if err != nil {
		return err
	}
	var here projects.Project
	if wd, err := os.Getwd(); err == nil {
		here, _ = r.Get(wd)
	}

	width := 0
	for _, l := range e.layouts {
		width = max(width, len(l.Name))
	}
	for _, l := range e.layouts {
		var marks []string
		if l.Name == e.s.Layout {
			marks = append(marks, "default")
		}
		if here.Layout == l.Name {
			marks = append(marks, here.Name)
		}
		mark := ""
		if len(marks) > 0 {
			mark = "  (" + strings.Join(marks, ", ") + ")"
		}
		fmt.Printf("%-*s  %s%s\n", width, l.Name, l.Description, mark)
	}

	return nil
}

func layoutSetDefault(args []string) error {
	if len(args) != 1 {
		return usageErrorf("layout set-default takes one layout name")
	}
	e, err := loadLayoutEnv()
	if err != nil {
		return err
	}
	if err := e.known(args[0]); err != nil {
		return err
	}
	if err := e.f.Set("layout", args[0]); err != nil {
		return err
	}
	if err := e.f.Save(); err != nil {
		return err
	}
	fmt.Printf("Projects open in the %s layout (a project's own layout still wins).\n", args[0])

	return nil
}

func layoutSet(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return usageErrorf("layout set takes a layout name and, optionally, a project")
	}
	e, err := loadLayoutEnv()
	if err != nil {
		return err
	}
	if err := e.known(args[0]); err != nil {
		return err
	}
	p, err := setProjectLayout(e.p, projectArg(args[1:]), args[0])
	if err != nil {
		return err
	}
	fmt.Printf("%s opens in the %s layout.\n", p.Name, args[0])

	return nil
}

func layoutUnset(args []string) error {
	if len(args) > 1 {
		return usageErrorf("layout unset takes at most one project")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	proj, err := setProjectLayout(p, projectArg(args), "")
	if err != nil {
		return err
	}
	fmt.Printf("%s opens in the default layout.\n", proj.Name)

	return nil
}

// projectArg is the project a layout command names, the current dir by default
func projectArg(args []string) string {
	if len(args) == 0 {
		return "."
	}

	return args[0]
}

// setProjectLayout records a project's layout, registering a dir that isn't a
// project yet (as pin does)
func setProjectLayout(p *paths.Paths, nameOrPath, layout string) (projects.Project, error) {
	r, err := projects.Load(p.Projects())
	if err != nil {
		return projects.Project{}, err
	}
	if _, ok := r.Get(nameOrPath); !ok {
		info, statErr := os.Stat(nameOrPath)
		if statErr != nil || !info.IsDir() || layout == "" {
			return projects.Project{}, fmt.Errorf("no project %q (see 'bud projects ls')", nameOrPath)
		}
		if _, err := r.Add(nameOrPath, time.Time{}); err != nil {
			return projects.Project{}, err
		}
	}
	proj, err := r.SetLayout(nameOrPath, layout)
	if err != nil {
		return projects.Project{}, err
	}

	return proj, r.Save()
}
