package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/lang"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Lang())
}

// Lang manages the languages (BASE.md 2, 5.8): which specs this machine enables,
// kept in config.json, and the user's own specs in user-tools/languages
func Lang() *Command {
	var enableYes, rmYes bool
	return &Command{
		Name:  "lang",
		Usage: "Lists, adds, removes, enables and disables languages",
		Subs: []*Command{
			{
				Name:  "ls",
				Usage: "list every language and whether it's enabled",
				Run:   func(_ *Globals, args []string) error { return langLs(args) },
			},
			{
				Name:  "enable",
				Usage: "enable languages and install their tools",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&enableYes, "yes", false, "install missing system packages without asking")
					fs.BoolVar(&enableYes, "y", false, "short for -yes")
				},
				Run: func(g *Globals, args []string) error { return langEnable(g, args, enableYes) },
			},
			{
				Name:  "disable",
				Usage: "disable languages (their tools stay installed)",
				Run:   func(_ *Globals, args []string) error { return langDisable(args) },
			},
			{
				Name:  "add",
				Usage: "start a new language spec from the template",
				Run:   func(_ *Globals, args []string) error { return langAdd(args) },
			},
			{
				Name:  "rm",
				Usage: "remove a language spec you added",
				Flags: func(fs *flag.FlagSet) {
					fs.BoolVar(&rmYes, "yes", false, "remove without asking")
					fs.BoolVar(&rmYes, "y", false, "short for -yes")
				},
				Run: func(_ *Globals, args []string) error { return langRm(args, rmYes) },
			},
		},
	}
}

// swappable in tests
var restorePlugins = nvim.RestorePlugins

// langEnv is what every lang command reads: the paths, config.json and the specs
type langEnv struct {
	p     *paths.Paths
	f     *config.File
	s     config.Settings
	specs *lang.Set
}

// specDirs are where specs are read from, after the shipped ones: the user's,
// then any dropped straight into the synced config
func specDirs(p *paths.Paths) []string {
	return []string{p.UserLanguageSpecs(), p.LanguageSpecs()}
}

func loadLangEnv() (*langEnv, error) {
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
		return nil, fmt.Errorf("%w\nfix or remove %s first", err, p.MachineConfig())
	}
	specs, err := loadLanguages(true, specDirs(p)...)
	if err != nil {
		return nil, err
	}

	return &langEnv{p: p, f: f, s: s, specs: specs}, nil
}

// shipped reports whether bud itself ships a spec named name
func shipped(name string) bool {
	_, err := fs.Stat(bud.Config(), lang.File(name))
	return err == nil
}

// enabledNames is the configured list, or the profile's preset when there's none
func (e *langEnv) enabledNames() []string {
	if e.s.Languages == nil {
		return e.specs.Preset(e.s.Profile)
	}

	return slices.Clone(e.s.Languages)
}

func (e *langEnv) saveLanguages(names []string) error {
	if err := e.f.Set("languages", names); err != nil {
		return err
	}

	return e.f.Save()
}

func langLs(args []string) error {
	if len(args) > 0 {
		return usageErrorf("lang ls takes no arguments")
	}
	e, err := loadLangEnv()
	if err != nil {
		return err
	}

	enabled := e.enabledNames()
	for _, name := range e.specs.Names() {
		state := "disabled"
		if slices.Contains(enabled, name) {
			state = "enabled"
		}
		source := "bud"
		if !shipped(name) {
			source = "yours"
		}
		fmt.Printf("%-12s %-9s %s\n", name, state, source)
	}
	for _, name := range slices.Sorted(maps.Keys(e.specs.Errors)) {
		fmt.Printf("%-12s %-9s %s\n", name, "broken", e.specs.Errors[name])
	}
	_, unknown := e.specs.Enabled(enabled, e.s.Profile)
	for _, name := range unknown {
		fmt.Printf("%-12s %-9s %s\n", name, "enabled", "no spec (remove it with 'bud lang disable "+name+"')")
	}
	if e.s.Languages == nil {
		fmt.Printf("\n(the %s profile's preset: no languages list in %s)\n", e.s.Profile, e.p.MachineConfig())
	}

	return nil
}

func langEnable(g *Globals, args []string, yes bool) error {
	if len(args) == 0 {
		return usageErrorf("lang enable needs at least one language")
	}
	e, err := loadLangEnv()
	if err != nil {
		return err
	}

	for _, name := range args {
		if msg, broken := e.specs.Errors[name]; broken {
			return fmt.Errorf("%s doesn't load: %s", name, msg)
		}
		if _, ok := e.specs.Get(name); !ok {
			return fmt.Errorf("no language %q (see 'bud lang ls')", name)
		}
		// the template's placeholders would go to the package manager and Mason
		if path := filepath.Join(e.p.UserLanguageSpecs(), name+".lua"); isTemplate(path) {
			return fmt.Errorf("%s is still the template: fill it in first", path)
		}
	}

	names := e.enabledNames()
	var added []string
	for _, name := range args {
		if !slices.Contains(names, name) && !slices.Contains(added, name) {
			added = append(added, name)
		}
	}
	if len(added) == 0 {
		fmt.Println("Already enabled.")
		return nil
	}
	if err := e.saveLanguages(append(names, added...)); err != nil {
		return err
	}
	fmt.Printf("Enabled %s.\n", strings.Join(added, ", "))

	var specs []lang.Spec
	for _, name := range added {
		spec, _ := e.specs.Get(name)
		specs = append(specs, spec)
	}
	if deps := installer.ForBins(lang.System(specs)); len(installer.Missing(deps)) > 0 {
		if err := installDepsShown(deps, terminalPrompter(yes), yes); err != nil {
			return err
		}
	}

	// parsers and Mason packages come from the restore; before bud install
	// there's no config to restore into, and install will do it
	if !nvim.HasManifest(e.p.Config) {
		fmt.Println("Run 'bud install' to set up the editor and these languages' tools.")
		return nil
	}
	g.Logf("restoring plugins, parsers and Mason packages")
	fmt.Println("Installing parsers and language tools...")

	return restorePlugins()
}

func langDisable(args []string) error {
	if len(args) == 0 {
		return usageErrorf("lang disable needs at least one language")
	}
	e, err := loadLangEnv()
	if err != nil {
		return err
	}

	names := e.enabledNames()
	var removed, notEnabled []string
	for _, name := range args {
		if slices.Contains(names, name) {
			removed = append(removed, name)
		} else {
			notEnabled = append(notEnabled, name)
		}
	}
	if len(notEnabled) > 0 {
		return fmt.Errorf("not enabled: %s", strings.Join(notEnabled, ", "))
	}

	names = slices.DeleteFunc(names, func(n string) bool { return slices.Contains(removed, n) })
	if err := e.saveLanguages(names); err != nil {
		return err
	}
	// Mason packages and parsers are shared and cheap to keep, so they stay
	fmt.Printf("Disabled %s. Reopen bud for it to take effect.\n", strings.Join(removed, ", "))

	return nil
}

// isTemplate reports whether the file at path is byte for byte languages/_template.lua
func isTemplate(path string) bool {
	have, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	template, err := fs.ReadFile(bud.Config(), lang.File("_template"))

	return err == nil && bytes.Equal(have, template)
}

// specName is what a spec's file name (and so its language name) can be
var specName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func langAdd(args []string) error {
	if len(args) != 1 {
		return usageErrorf("lang add takes one language name")
	}
	name := args[0]
	if !specName.MatchString(name) {
		return usageErrorf("%q isn't a language name: use lowercase letters, digits, - and _, starting with a letter", name)
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if shipped(name) {
		return fmt.Errorf("bud already ships %s: enable it with 'bud lang enable %s'", name, name)
	}

	path := filepath.Join(p.UserLanguageSpecs(), name+".lua")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	template, err := fs.ReadFile(bud.Config(), lang.File("_template"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// O_EXCL, so a file that appeared since the check above is never overwritten
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := out.Write(template); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	fmt.Printf("Created %s from the template.\n", path)
	fmt.Printf("Fill it in, then run 'bud lang enable %s'.\n", name)

	return nil
}

func langRm(args []string, yes bool) error {
	if len(args) != 1 {
		return usageErrorf("lang rm takes one language name")
	}
	name := args[0]
	if shipped(name) {
		return fmt.Errorf("%s is one of bud's own languages, so it can't be removed: disable it with 'bud lang disable %s'", name, name)
	}
	e, err := loadLangEnv()
	if err != nil {
		return err
	}

	path := filepath.Join(e.p.UserLanguageSpecs(), name+".lua")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no language spec of yours named %s in %s", name, e.p.UserLanguageSpecs())
		}
		return err
	}

	if err := confirmOnTerminal("Remove "+path+"?", yes, "bud lang rm --yes "+name); err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return err
	}
	// an enabled name with no spec only makes doctor warn, so drop it too
	if e.s.Languages != nil && slices.Contains(e.s.Languages, name) {
		names := slices.DeleteFunc(slices.Clone(e.s.Languages), func(n string) bool { return n == name })
		if err := e.saveLanguages(names); err != nil {
			return err
		}
	}
	fmt.Printf("Removed %s.\n", path)

	return nil
}
