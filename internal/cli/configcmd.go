package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/nvim"
	"jgarner.dev/bud/internal/paths"
)

func init() {
	RegisterCommand(Config())
}

// Config reads and writes machine config (config.json, D2, D13) by dotted
// key, so it rarely needs editing by hand. Keys bud doesn't know survive
// every write
func Config() *Command {
	var force, changed bool
	return &Command{
		Name:  "config",
		Usage: "Reads and writes bud's machine config by dotted key (update.channel, ai.claude.resume, ...)",
		Subs: []*Command{
			{Name: "ls", Usage: "print the whole config, defaults filled in; --changed only what config.json sets", Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&changed, "changed", false, "only what config.json sets, not the defaults")
			}, Run: func(_ *Globals, args []string) error { return configLs(args, changed) }},
			{Name: "get", Usage: "print one setting: get <key>", Run: func(_ *Globals, args []string) error { return configGet(args) }},
			{Name: "set", Usage: "change one setting: set <key> <value> (JSON, or a plain string)", Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&force, "force", false, "set a key bud doesn't know")
			}, Run: func(_ *Globals, args []string) error { return configSet(args, force) }},
			{Name: "unset", Usage: "put a setting back to its default: unset <key>", Run: func(_ *Globals, args []string) error { return configUnset(args) }},
			{Name: "edit", Usage: "open config.json in bud's editor, then check it", Run: func(_ *Globals, args []string) error { return configEdit(args) }},
			{Name: "path", Usage: "print where config.json is", Run: func(_ *Globals, args []string) error { return configPath(args) }},
		},
	}
}

func loadConfigFile() (*config.File, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}

	return config.Load(p.MachineConfig())
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))

	return nil
}

func configLs(args []string, changed bool) error {
	if len(args) > 0 {
		return usageErrorf("config ls takes no arguments")
	}
	f, err := loadConfigFile()
	if err != nil {
		return err
	}
	if changed {
		return printJSON(f.Doc())
	}
	s, err := f.Settings()
	if err != nil {
		return err
	}

	return printJSON(s)
}

func configGet(args []string) error {
	if len(args) != 1 {
		return usageErrorf("config get takes one key")
	}
	f, err := loadConfigFile()
	if err != nil {
		return err
	}
	v, ok := f.Effective(args[0])
	if !ok {
		return usageErrorf("no setting %q (bud config ls shows them all)", args[0])
	}
	if s, isString := v.(string); isString {
		fmt.Println(s)
		return nil
	}

	return printJSON(v)
}

// parseValue reads a value from the command line: JSON when it is (true, 30,
// ["go","lua"], "quoted"), else the text as a string
func parseValue(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v
	}

	return raw
}

func configSet(args []string, force bool) error {
	if len(args) != 2 {
		return usageErrorf("config set takes a key and a value")
	}
	key, raw := args[0], args[1]
	if !config.Known(key) && !force {
		return usageErrorf("bud has no setting %q (a typo? bud config ls shows them; --force sets it anyway)", key)
	}
	f, err := loadConfigFile()
	if err != nil {
		return err
	}
	if err := f.Set(key, parseValue(raw)); err != nil {
		return err
	}
	// the whole file has to make sense afterwards: the right type, an allowed value
	if _, err := f.Settings(); err != nil {
		return fmt.Errorf("not saved: %w", err)
	}
	if err := f.Save(); err != nil {
		return err
	}
	v, _ := f.Effective(key)
	data, _ := json.Marshal(v)
	fmt.Printf("%s = %s\n", key, data)

	return nil
}

func configUnset(args []string) error {
	if len(args) != 1 {
		return usageErrorf("config unset takes one key")
	}
	f, err := loadConfigFile()
	if err != nil {
		return err
	}
	f.Unset(args[0])
	if err := f.Save(); err != nil {
		return err
	}
	if v, ok := f.Effective(args[0]); ok {
		data, _ := json.Marshal(v)
		fmt.Printf("%s is back to its default: %s\n", args[0], data)
	} else {
		fmt.Printf("%s is unset.\n", args[0])
	}

	return nil
}

func configPath(args []string) error {
	if len(args) > 0 {
		return usageErrorf("config path takes no arguments")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	fmt.Println(p.MachineConfig())

	return nil
}

// swappable in tests: runs the editor on path, on the terminal
var editFile = func(path string) error {
	bin, err := exec.LookPath("nvim")
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, path)
	cmd.Env = append(os.Environ(), "NVIM_APPNAME="+nvim.AppName)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func configEdit(args []string) error {
	if len(args) > 0 {
		return usageErrorf("config edit takes no arguments")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	path := p.MachineConfig()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			return err
		}
	}
	if err := editFile(path); err != nil {
		return err
	}
	f, err := config.Load(path)
	if err == nil {
		_, err = f.Settings()
	}
	if err != nil {
		return fmt.Errorf("%w\nbud config edit opens it again", err)
	}
	var unknown []string
	walkKeys(f.Doc(), "", func(key string) {
		if !config.Known(key) {
			unknown = append(unknown, key)
		}
	})
	if len(unknown) > 0 {
		fmt.Printf("Saved. bud doesn't use: %s\n", strings.Join(unknown, ", "))
	} else {
		fmt.Println("Saved, and it checks out.")
	}

	return nil
}

// walkKeys calls fn with each leaf's dotted key
func walkKeys(m map[string]any, prefix string, fn func(string)) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok && len(child) > 0 {
			walkKeys(child, key, fn)
			continue
		}
		fn(key)
	}
}
