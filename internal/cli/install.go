package cli

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/doctor"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Install())
}

func Install() *Command {
	var yes bool
	cmd := &Command{
		Name:  "install",
		Usage: "Sets up bud: asks a few questions, installs missing system packages and the Bud Neovim config",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "don't ask anything: take the defaults and run the package manager")
			fs.BoolVar(&yes, "y", false, "short for -yes")
		},
		Run:  func(g *Globals, args []string) error { return install(g, args, yes) },
		Subs: nil,
	}

	return cmd
}

// detectTerminals is installer.DetectTerminals, swappable in tests
var detectTerminals = installer.DetectTerminals

func install(g *Globals, args []string, yes bool) error {
	if len(args) > 0 {
		return usageErrorf("install takes no arguments")
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	f, err := config.Load(p.MachineConfig())
	if err != nil {
		return err
	}
	s, err := f.Settings()
	if err != nil {
		return fmt.Errorf("%w\nfix or remove %s, then run 'bud install' again", err, p.MachineConfig())
	}

	// questions need someone to answer them: --yes or no terminal takes the defaults
	var pr *prompter
	if !yes && isTerminal(os.Stdin) {
		pr = newPrompter(os.Stdin, os.Stdout)
	}
	if s, err = askSetup(pr, f, s, g.Profile); err != nil {
		return err
	}
	if err := saveSetup(f, s); err != nil {
		return err
	}
	g.Logf("profile %s, terminal %s, languages %s", s.Profile, s.Terminal, strings.Join(s.Languages, " "))

	fmt.Println()
	for _, dep := range installer.Needed(s.Profile) {
		status := "missing"
		if dep.Installed() {
			status = "ok"
		}
		fmt.Printf("%-8s %s\n", dep.Name, status)
	}
	if err := installDeps(installer.NewPlan(s.Profile), pr, yes); err != nil {
		return err
	}

	if err := nvim.Apply(); err != nil {
		return err
	}

	fmt.Println()
	if err := report(doctor.Run()); err != nil {
		return err
	}
	fmt.Println("\nAll set. Run 'bud' to start.")

	return nil
}

// installDeps shows the package manager command and runs it once confirmed.
// pr is nil when nobody can be asked, so without --yes nothing runs
func installDeps(plan *installer.Plan, pr *prompter, yes bool) error {
	argv := plan.Command()
	if argv == nil {
		// nothing missing, or no package manager to install with
		return plan.Err()
	}

	fmt.Printf("\nbud will run:\n  %s\n", strings.Join(argv, " "))
	if !yes {
		if pr == nil {
			return plan.Err()
		}
		ok, err := pr.confirm("Continue?")
		if err != nil {
			return err
		}
		if !ok {
			return plan.Err()
		}
	}

	return plan.Run()
}

// askSetup asks install's questions (BASE.md 5.2 step 3), defaulting to what's
// configured. A nil prompter takes every default. profileFlag, from --profile,
// answers the profile question
func askSetup(pr *prompter, f *config.File, s config.Settings, profileFlag string) (config.Settings, error) {
	var err error

	s.Profile = firstNonEmpty(profileFlag, s.Profile)
	if pr != nil && profileFlag == "" {
		if s.Profile, err = pr.choose("Profile", config.Profiles, s.Profile); err != nil {
			return s, err
		}
	}

	detected := detectTerminals()
	if _, set := f.Get("terminal"); !set && len(detected) > 0 {
		s.Terminal = detected[0]
	}
	if pr != nil {
		question := "Terminal"
		if len(detected) > 0 {
			question += ", found " + strings.Join(detected, " and ")
		}
		if s.Terminal, err = pr.choose(question, config.Terminals, s.Terminal); err != nil {
			return s, err
		}
	}

	if _, set := f.Get("languages"); !set {
		s.Languages = config.PresetLanguages(s.Profile)
	}
	if pr != nil {
		if s.Languages, err = pr.chooseMany("Languages", config.Languages, s.Languages); err != nil {
			return s, err
		}
	}

	return s, nil
}

// saveSetup writes install's answers to config.json. An answer that matches the
// default isn't written, unless the file already set that key, so a fresh
// machine that keeps every default gets no config.json at all
func saveSetup(f *config.File, s config.Settings) error {
	def := config.Defaults()
	answers := []struct {
		key       string
		value     any
		isDefault bool
	}{
		{"profile", s.Profile, s.Profile == def.Profile},
		{"terminal", s.Terminal, s.Terminal == def.Terminal},
		{"languages", s.Languages, slices.Equal(s.Languages, def.Languages)},
	}

	changed := false
	for _, a := range answers {
		if _, set := f.Get(a.key); !set && a.isDefault {
			continue
		}
		if err := f.Set(a.key, a.value); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}

	return f.Save()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}
