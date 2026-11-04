package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
)

func init() {
	RegisterCommand(Projects())
}

// Projects manages the project registry (D36) that the dashboard and the
// project picker list. Opening a dir with bud registers it too
func Projects() *Command {
	var opened bool
	return &Command{
		Name:  "projects",
		Usage: "Lists, adds, removes and pins registered projects",
		Subs: []*Command{
			{Name: "ls", Usage: "list registered projects, pinned first", Run: func(_ *Globals, args []string) error { return projectsLs(args) }},
			{
				Name:  "add",
				Usage: "register a directory without opening it",
				Flags: func(fs *flag.FlagSet) {
					// the editor's project picker uses this, since only the CLI writes projects.json
					fs.BoolVar(&opened, "opened", false, "also record it as opened just now")
				},
				Run: func(_ *Globals, args []string) error { return projectsAdd(args, opened) },
			},
			{Name: "rm", Usage: "forget a project (its files are untouched)", Run: func(_ *Globals, args []string) error { return projectsRm(args) }},
			{Name: "pin", Usage: "pin a project to the top of the dashboard", Run: func(_ *Globals, args []string) error { return projectsPin(args, true) }},
			{Name: "unpin", Usage: "unpin a project", Run: func(_ *Globals, args []string) error { return projectsPin(args, false) }},
		},
	}
}

func loadRegistry() (*projects.Registry, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}

	return projects.Load(p.Projects())
}

func projectsLs(args []string) error {
	if len(args) > 0 {
		return usageErrorf("projects ls takes no arguments")
	}
	r, err := loadRegistry()
	if err != nil {
		return err
	}
	if len(r.Projects) == 0 {
		fmt.Println("No projects yet: open one with 'bud <dir>', or 'bud projects add <dir>'.")
		return nil
	}

	for _, p := range r.Sorted() {
		mark := " "
		if p.Pinned {
			mark = "*"
		}
		when := "never opened"
		if !p.LastOpened.IsZero() {
			when = ago(now().Sub(p.LastOpened))
		}
		if p.Missing() {
			when = "missing"
		}
		fmt.Printf("%s %-20s %-14s %s\n", mark, p.Name, when, p.Path)
	}

	return nil
}

// ago is a rough age: just now, 5m, 3h, 2d, 6w
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
	}
}

func projectsAdd(args []string, opened bool) error {
	if len(args) == 0 {
		return usageErrorf("projects add needs a directory")
	}
	r, err := loadRegistry()
	if err != nil {
		return err
	}
	at := time.Time{}
	if opened {
		at = now()
	}
	for _, dir := range args {
		before := len(r.Projects)
		p, err := r.Add(dir, at)
		if err != nil {
			return err
		}
		if len(r.Projects) == before {
			fmt.Printf("%s is already registered as %s.\n", p.Path, p.Name)
		} else {
			fmt.Printf("Added %s (%s).\n", p.Name, p.Path)
		}
	}

	return r.Save()
}

func projectsRm(args []string) error {
	if len(args) == 0 {
		return usageErrorf("projects rm needs a project name or path")
	}
	r, err := loadRegistry()
	if err != nil {
		return err
	}
	for _, name := range args {
		p, err := r.Remove(name)
		if err != nil {
			return err
		}
		fmt.Printf("Removed %s from the registry (%s is untouched).\n", p.Name, p.Path)
	}

	return r.Save()
}

func projectsPin(args []string, pinned bool) error {
	if len(args) == 0 {
		return usageErrorf("needs a project name or path")
	}
	r, err := loadRegistry()
	if err != nil {
		return err
	}
	for _, name := range args {
		p, err := r.SetPinned(name, pinned)
		if err != nil {
			// a path that isn't registered yet gets registered, then pinned
			if _, statErr := os.Stat(name); !pinned || statErr != nil {
				return err
			}
			if p, err = r.Add(name, time.Time{}); err != nil {
				return err
			}
			if p, err = r.SetPinned(p.Name, true); err != nil {
				return err
			}
		}
		verb := "Pinned"
		if !pinned {
			verb = "Unpinned"
		}
		fmt.Printf("%s %s.\n", verb, p.Name)
	}

	return r.Save()
}
