package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
)

func init() {
	RegisterCommand(Issue())
}

// Issue says where issues filed from the editor go (JGA-123): a project's own
// Linear team and project in projects.json, else config.json's ai.issues, and
// bud's own tracker for issues about bud itself
func Issue() *Command {
	var bud, asJSON, global, unsetCheckout bool
	var team, project string
	return &Command{
		Name:  "issue",
		Usage: "Says and sets where issues filed from the editor go in Linear",
		Subs: []*Command{
			{
				Name:  "target",
				Usage: "where an issue filed from a project goes: target [dir] (default: the current dir)",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&bud, "bud", false, "for an issue about bud itself")
					fs.BoolVar(&asJSON, "json", false, "print it as JSON, for the editor")
				},
				Run: func(_ *Globals, args []string) error { return issueTarget(args, bud, asJSON) },
			},
			{
				Name:  "set",
				Usage: "set where a project's issues go: set --team T [--project P] [project]",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&team, "team", "", "the Linear team (name or key)")
					fs.StringVar(&project, "project", "", "the Linear project in it, if any")
					fs.BoolVar(&global, "global", false, "for every project without its own (config.json)")
				},
				Run: func(_ *Globals, args []string) error { return issueSet(args, team, project, global) },
			},
			{
				Name:  "checkout",
				Usage: "the bud checkout <leader>aB works in: checkout [path] saves it, checkout --unset forgets it, with neither it says what's saved",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&unsetCheckout, "unset", false, "forget the saved checkout")
				},
				Run: func(_ *Globals, args []string) error { return issueCheckout(args, unsetCheckout) },
			},
			{Name: "unset", Usage: "send a project's issues back to the default target: unset [project]", Run: func(_ *Globals, args []string) error { return issueUnset(args) }},
		},
	}
}

// target is where an issue goes, and the checkout Claude works in for it
type target struct {
	Team    string `json:"team"`
	Project string `json:"project"`
	// "project" (projects.json), "config" (ai.issues), "bud" (ai.issues.bud), or "" for none
	Source string `json:"source"`
	// the project's dir, or for bud its dev-linked or remembered checkout; ""
	// when there is none (or the remembered one is gone, see Stale)
	Root string `json:"root"`
	// the remembered bud checkout that no longer works, and why, so the editor
	// can say so and ask again
	Stale    string `json:"stale,omitempty"`
	StaleWhy string `json:"stale_why,omitempty"`
}

// resolveTarget works out where an issue filed from dir goes
func resolveTarget(p *paths.Paths, s config.Settings, dir string, bud bool) (target, error) {
	if bud {
		t := target{Team: s.AI.Issues.Bud.Team, Project: s.AI.Issues.Bud.Project, Source: "bud"}
		if t.Team == "" {
			t.Source = ""
		}
		if root, ok := nvim.DevLink(p.Config); ok {
			t.Root = root
		} else if c := s.AI.Issues.Bud.Checkout; c != "" {
			// remembered, but checked every time: it may have moved since
			if err := nvim.CheckBudRepo(c); err != nil {
				t.Stale, t.StaleWhy = c, err.Error()
			} else {
				t.Root = c
			}
		}
		return t, nil
	}

	root, err := filepath.Abs(dir)
	if err != nil {
		return target{}, err
	}
	r, err := projects.Load(p.Projects())
	if err != nil {
		return target{}, err
	}
	if proj, ok := r.Get(dir); ok {
		root = proj.Path
		if proj.Issues != nil && proj.Issues.Team != "" {
			return target{Team: proj.Issues.Team, Project: proj.Issues.Project, Source: "project", Root: root}, nil
		}
	}
	if s.AI.Issues.Team != "" {
		return target{Team: s.AI.Issues.Team, Project: s.AI.Issues.Project, Source: "config", Root: root}, nil
	}

	return target{Root: root}, nil
}

func loadSettings() (*paths.Paths, *config.File, config.Settings, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, nil, config.Settings{}, err
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return nil, nil, config.Settings{}, err
	}
	s, err := f.Settings()

	return p, f, s, err
}

func issueTarget(args []string, bud, asJSON bool) error {
	if len(args) > 1 {
		return usageErrorf("issue target takes at most one dir")
	}
	p, _, s, err := loadSettings()
	if err != nil {
		return err
	}
	t, err := resolveTarget(p, s, projectArg(args), bud)
	if err != nil {
		return err
	}
	if asJSON {
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		_, err = fmt.Println(string(data))
		return err
	}

	switch {
	case t.Source == "" && bud:
		return fmt.Errorf("no tracker for bud itself: set ai.issues.bud.team with bud config set")
	case t.Source == "":
		return fmt.Errorf("no issue target for %s: set one with bud issue set --team T [--project P]", t.Root)
	}
	where := t.Team
	if t.Project != "" {
		where += " / " + t.Project
	}
	fmt.Printf("%s  (%s)\n", where, t.Source)
	if t.Root != "" {
		fmt.Printf("worked in %s\n", t.Root)
	}

	return nil
}

func issueSet(args []string, team, project string, global bool) error {
	if team == "" {
		return usageErrorf("issue set needs --team")
	}
	p, f, _, err := loadSettings()
	if err != nil {
		return err
	}

	if global {
		if len(args) > 0 {
			return usageErrorf("issue set --global takes no project")
		}
		if err := f.Set("ai.issues.team", team); err != nil {
			return err
		}
		if err := f.Set("ai.issues.project", project); err != nil {
			return err
		}
		if err := f.Save(); err != nil {
			return err
		}
		fmt.Printf("Issues go to %s unless a project has its own.\n", describeTarget(team, project))
		return nil
	}

	if len(args) > 1 {
		return usageErrorf("issue set takes at most one project")
	}
	proj, err := setProjectIssues(p, projectArg(args), &projects.IssueTarget{Team: team, Project: project})
	if err != nil {
		return err
	}
	fmt.Printf("Issues from %s go to %s.\n", proj.Name, describeTarget(team, project))

	return nil
}

func issueUnset(args []string) error {
	if len(args) > 1 {
		return usageErrorf("issue unset takes at most one project")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	proj, err := setProjectIssues(p, projectArg(args), nil)
	if err != nil {
		return err
	}
	fmt.Printf("Issues from %s go to the default target.\n", proj.Name)

	return nil
}

func describeTarget(team, project string) string {
	if project == "" {
		return team
	}

	return team + " / " + project
}

// setProjectIssues records a project's issue target, registering a dir that
// isn't a project yet (as layout set does)
func setProjectIssues(p *paths.Paths, nameOrPath string, t *projects.IssueTarget) (projects.Project, error) {
	r, err := projects.Load(p.Projects())
	if err != nil {
		return projects.Project{}, err
	}
	if _, ok := r.Get(nameOrPath); !ok {
		info, statErr := os.Stat(nameOrPath)
		if statErr != nil || !info.IsDir() || t == nil {
			return projects.Project{}, fmt.Errorf("no project %q (see 'bud projects ls')", nameOrPath)
		}
		if _, err := r.Add(nameOrPath, time.Time{}); err != nil {
			return projects.Project{}, err
		}
	}
	proj, err := r.SetIssues(nameOrPath, t)
	if err != nil {
		return projects.Project{}, err
	}

	return proj, r.Save()
}

// issueCheckout saves, forgets or reports the bud checkout issues about bud are
// worked on in
func issueCheckout(args []string, unset bool) error {
	if len(args) > 1 || (unset && len(args) > 0) {
		return usageErrorf("issue checkout takes one path, or --unset")
	}
	_, f, s, err := loadSettings()
	if err != nil {
		return err
	}

	switch {
	case unset:
		if err := f.Set("ai.issues.bud.checkout", ""); err != nil {
			return err
		}
		if err := f.Save(); err != nil {
			return err
		}
		fmt.Println("Forgot the bud checkout.")
		return nil
	case len(args) == 1:
		dir, err := filepath.Abs(expandHome(args[0]))
		if err != nil {
			return err
		}
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		if err := nvim.CheckBudRepo(dir); err != nil {
			return err
		}
		if err := f.Set("ai.issues.bud.checkout", dir); err != nil {
			return err
		}
		if err := f.Save(); err != nil {
			return err
		}
		fmt.Printf("Issues about bud are worked on in %s.\n", dir)
		return nil
	}

	c := s.AI.Issues.Bud.Checkout
	if c == "" {
		fmt.Println("No bud checkout saved: bud issue checkout <path> saves one.")
		return nil
	}
	if err := nvim.CheckBudRepo(c); err != nil {
		return fmt.Errorf("saved checkout is unusable: %w\nsave another with bud issue checkout <path>, or forget it with --unset", err)
	}
	fmt.Println(c)

	return nil
}

// expandHome turns a leading ~ into the home dir; the editor passes what was typed
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}

	return path
}
