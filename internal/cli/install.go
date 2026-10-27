package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/doctor"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/lang"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Install())
}

func Install() *Command {
	var yes bool
	return &Command{
		Name:  "install",
		Usage: "Sets up bud: asks a few questions, installs missing system packages and the Bud Neovim config",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "don't ask anything: take the defaults and run the package manager")
			fs.BoolVar(&yes, "y", false, "short for -yes")
		},
		Run: func(g *Globals, args []string) error { return install(g, args, yes) },
	}
}

// swappable in tests
var (
	detectTerminals = installer.DetectTerminals
	loadLanguages   = lang.Load
	nvimInstalled   = func() bool { _, err := exec.LookPath("nvim"); return err == nil }
)

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
	pr := terminalPrompter(yes)
	if s, err = askSetup(pr, f, s, g.Profile); err != nil {
		return err
	}
	if err := saveSetup(f, s, nil); err != nil {
		return err
	}

	// the language question needs the specs, which need nvim to read. With nvim
	// already here, every question comes first and one install covers it all;
	// on a fresh machine the base deps go in first, nvim among them
	userSpecs := specDirs(p)
	deps := installer.Needed(s.Profile)
	var specs *lang.Set
	if nvimInstalled() {
		if specs, s, err = askLanguages(pr, f, s, userSpecs); err != nil {
			return err
		}
		deps = append(deps, languageDeps(specs, s)...)
	}
	if err := installDepsShown(deps, pr, yes); err != nil {
		return err
	}
	if specs == nil {
		if specs, s, err = askLanguages(pr, f, s, userSpecs); err != nil {
			return err
		}
		if extra := languageDeps(specs, s); len(installer.Missing(extra)) > 0 {
			if err := installDepsShown(extra, pr, yes); err != nil {
				return err
			}
		}
	}
	g.Logf("profile %s, terminal %s, languages %s", s.Profile, s.Terminal, strings.Join(s.Languages, " "))

	if err := nvim.Apply(); err != nil {
		return err
	}
	if s.Profile == "full" {
		// the editor works without the app, so a failure here only warns
		fmt.Println()
		if err := installBundle(g); err != nil {
			fmt.Printf("Couldn't install the app (%v); 'bud bundle' tries again.\n", err)
		}
	}

	if s.AI.Claude.Autostart {
		fmt.Println()
		if err := installClaude(pr, yes); err != nil {
			fmt.Printf("Claude Code isn't set up (%v); 'bud claude setup' tries again.\n", err)
		}
		if err := askKitSecrets(pr); err != nil {
			fmt.Printf("Kit secrets: %v; 'bud kit secret set NAME' stores one.\n", err)
		}
	}

	fmt.Println()
	if err := report(doctor.Run(doctor.Options{})); err != nil {
		return err
	}
	fmt.Println("\nAll set. Run 'bud' to start.")

	return nil
}

// swappable in tests: the desktop app on the full profile (BASE.md 5.2 step 5)
var installBundle = func(g *Globals) error { return bundleCmd(g, nil, false, false, "") }

// installClaude offers Claude Code's setup (BASE.md 4.2). bud works without
// it, so saying no, or having nobody to ask, is fine
var installClaude = func(pr *prompter, yes bool) error {
	s, err := checkClaude()
	if err != nil {
		return err
	}
	if s.Bin != "" && s.LoggedIn {
		fmt.Printf("Claude Code %s is ready.\n", s.Version)
		return nil
	}
	if !yes {
		if pr == nil {
			fmt.Println("Claude Code isn't set up; run 'bud claude setup' for the AI parts.")
			return nil
		}
		ok, err := pr.confirm("Set up Claude Code for bud's AI now?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Skipped; 'bud claude setup' does it later.")
			return nil
		}
	}

	return claudeSetup(pr, true)
}

// askLanguages reads the specs (this binary's, then the user's in userSpecs),
// asks which to enable and saves the answer
func askLanguages(pr *prompter, f *config.File, s config.Settings, userSpecs []string) (*lang.Set, config.Settings, error) {
	specs, err := loadLanguages(true, userSpecs...)
	if err != nil {
		return nil, s, err
	}
	if s, err = chooseLanguages(pr, specs, s); err != nil {
		return nil, s, err
	}

	return specs, s, saveSetup(f, s, specs.Preset(s.Profile))
}

// languageDeps are the system deps of the languages s enables
func languageDeps(specs *lang.Set, s config.Settings) []installer.Dep {
	enabled, _ := specs.Enabled(s.Languages, s.Profile)
	return installer.ForBins(lang.System(enabled))
}

// installDepsShown lists deps with whether each is installed, then installs the missing ones
func installDepsShown(deps []installer.Dep, pr *prompter, yes bool) error {
	fmt.Println()
	for _, dep := range deps {
		status, style := "missing", ansiRed
		if dep.Installed() {
			status, style = "ok", ansiGreen
		}
		fmt.Printf("%-15s %s\n", dep.Name, paint(os.Stdout, style, status))
	}

	return installDeps(installer.NewPlan(deps), pr, yes)
}

// installDeps shows the package manager command and runs it once confirmed.
// pr is nil when nobody can be asked, so without --yes nothing runs
func installDeps(plan *installer.Plan, pr *prompter, yes bool) error {
	argv := plan.Command()
	if argv == nil {
		// nothing missing, or no package manager to install with
		return plan.Err()
	}

	fmt.Printf("\nbud will run:\n  %s\n", paint(os.Stdout, ansiBold, strings.Join(argv, " ")))
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

// askSetup asks install's profile and terminal questions (BASE.md 5.2 step 3),
// defaulting to what's configured. A nil prompter takes every default.
// profileFlag, from --profile, answers the profile question
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

	return s, nil
}

// chooseLanguages asks which specs to enable, defaulting to the configured list
// or, with none, the profile's preset. A configured name with no spec is
// dropped from the default, since it can't be picked
func chooseLanguages(pr *prompter, specs *lang.Set, s config.Settings) (config.Settings, error) {
	names := specs.Names()
	def := specs.Preset(s.Profile)
	if s.Languages != nil {
		def = slices.DeleteFunc(slices.Clone(s.Languages), func(n string) bool { return !slices.Contains(names, n) })
	}
	if pr == nil {
		// nothing asked, so a configured list stays exactly as it is
		if s.Languages == nil {
			s.Languages = def
		}
		return s, nil
	}

	var err error
	s.Languages, err = pr.chooseMany("Languages", names, def)

	return s, err
}

// saveSetup writes install's answers to config.json. An answer that matches the
// default isn't written, unless the file already set that key, so a fresh
// machine that keeps every default gets no config.json at all. preset is the
// profile's preset of languages, the default for that answer; nil means the
// languages haven't been asked yet
func saveSetup(f *config.File, s config.Settings, preset []string) error {
	def := config.Defaults()
	answers := []struct {
		key       string
		value     any
		isDefault bool
	}{
		{"profile", s.Profile, s.Profile == def.Profile},
		{"terminal", s.Terminal, s.Terminal == def.Terminal},
		{"languages", s.Languages, s.Languages == nil || preset == nil || slices.Equal(s.Languages, preset)},
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
