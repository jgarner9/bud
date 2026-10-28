// Package paths resolves every location bud reads or writes on a machine, so
// nothing else hardcodes one. macOS uses the same XDG layout as Linux (D13),
// which is also where Neovim looks on both
package paths

import (
	"os"
	"path/filepath"
	"strconv"
)

// AppName names bud's directories. It's also NVIM_APPNAME, so Neovim's
// stdpath("config"/"data"/"state") land on the same dirs as Config, Data and State
const AppName = "bud"

type Paths struct {
	Config string // ~/.config/bud: synced copy of the embedded nvim/ (bud-owned, D21)
	Data   string // ~/.local/share/bud: plugins, Mason, bud-managed binaries, user tools
	State  string // ~/.local/state/bud: machine config, project registry, kit ledger
	Cache  string // ~/.cache/bud: Neovim's stdpath("cache"), logs and bytecode
	BinDir string // ~/.local/bin: where install.sh puts bud unless told otherwise
}

// Resolve reads XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME and XDG_CACHE_HOME, falling back
// to the XDG defaults under $HOME. Like the spec says, relative values are ignored
func Resolve() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return &Paths{
		Config: filepath.Join(xdg("XDG_CONFIG_HOME", home, ".config"), AppName),
		Data:   filepath.Join(xdg("XDG_DATA_HOME", home, ".local", "share"), AppName),
		State:  filepath.Join(xdg("XDG_STATE_HOME", home, ".local", "state"), AppName),
		Cache:  filepath.Join(xdg("XDG_CACHE_HOME", home, ".cache"), AppName),
		BinDir: filepath.Join(home, ".local", "bin"),
	}, nil
}

func xdg(env, home string, def ...string) string {
	if dir := os.Getenv(env); filepath.IsAbs(dir) {
		return dir
	}

	return filepath.Join(append([]string{home}, def...)...)
}

// MachineConfig is config.json, kept in state so `bud dev link` can turn
// Config into a symlink without taking local settings with it (D13)
func (p *Paths) MachineConfig() string { return filepath.Join(p.State, "config.json") }

// Projects is the project registry the dashboard reads
func (p *Paths) Projects() string { return filepath.Join(p.State, "projects.json") }

// KitLedger records everything `bud kit sync` wrote into Claude Code's config
func (p *Paths) KitLedger() string { return filepath.Join(p.State, "kit-ledger.json") }

// UpdateState records the last background update check (D9)
func (p *Paths) UpdateState() string { return filepath.Join(p.State, "update.json") }

// UpdateLog is where the detached background update check writes
func (p *Paths) UpdateLog() string { return filepath.Join(p.Cache, "update.log") }

// Migrations records which migrations/ scripts have run
func (p *Paths) Migrations() string { return filepath.Join(p.State, "migrations.json") }

// InstallReceipt is written by install.sh and names the binary it installed (D20)
func (p *Paths) InstallReceipt() string { return filepath.Join(p.State, "install.json") }

// ClaudeConfig is bud's own Claude Code config dir (JGA-135): its
// CLAUDE_CONFIG_DIR unless ai.claude.config is "home"
func (p *Paths) ClaudeConfig() string { return filepath.Join(p.Data, "claude") }

// UserTools holds generated tools that haven't been promoted into the repo
func (p *Paths) UserTools() string { return filepath.Join(p.Data, "user-tools") }

// LanguageSpecs is where sync puts the language specs bud ships
func (p *Paths) LanguageSpecs() string { return filepath.Join(p.Config, "languages") }

// ToolsState records why a tool was turned off (bud tools disable --reason)
func (p *Paths) ToolsState() string { return filepath.Join(p.State, "tools.json") }

// Tools is the synced dir of tools bud ships (bud.tools loads them)
func (p *Paths) Tools() string { return filepath.Join(p.Config, "tools") }

// Layouts is the synced layouts dir (bud.layout reads it)
func (p *Paths) Layouts() string { return filepath.Join(p.Config, "layouts") }

// UserLanguageSpecs holds the user's own specs (bud lang add), kept out of the
// synced config since that's bud's (D21, D34)
func (p *Paths) UserLanguageSpecs() string { return filepath.Join(p.UserTools(), "languages") }

// Bin holds binaries bud downloads itself, as opposed to the system package manager
func (p *Paths) Bin() string { return filepath.Join(p.Data, "bin") }

// Mason is Mason's install root, its default under stdpath("data")
func (p *Paths) Mason() string { return filepath.Join(p.Data, "mason") }

// PrevBinary is the binary a self-update replaced, kept for rollback
func (p *Paths) PrevBinary() string { return filepath.Join(p.Data, "bud.prev") }

// DesktopEntry is the Linux app launcher entry, ~/.local/share/applications/bud.desktop
func (p *Paths) DesktopEntry() string {
	return filepath.Join(filepath.Dir(p.Data), "applications", AppName+".desktop")
}

// DesktopIcon is the entry's icon at one size, in the hicolor theme under
// ~/.local/share/icons
func (p *Paths) DesktopIcon(size int) string {
	px := strconv.Itoa(size)
	return filepath.Join(filepath.Dir(p.Data), "icons", "hicolor", px+"x"+px, "apps", AppName+".png")
}
