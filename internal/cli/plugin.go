package cli

import (
	"fmt"

	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/plugin"
)

func init() {
	RegisterCommand(Plugin())
}

// Plugin is bud's embedded bud-ide plugin (D142). The editor asks for its dir
// when it starts a Claude Code session
func Plugin() *Command {
	return &Command{
		Name:  "plugin",
		Usage: "The bud-ide Claude Code plugin bud ships, which only sessions the IDE starts load",
		Subs: []*Command{
			{Name: "path", Usage: "extract the plugin if needed and print its dir (what --plugin-dir takes)", Run: pluginPath},
		},
	}
}

func pluginPath(_ *Globals, args []string) error {
	if len(args) > 0 {
		return usageErrorf("plugin path takes no arguments")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	dir, err := plugin.Dir(p)
	if err != nil {
		return err
	}
	fmt.Println(dir)

	return nil
}
