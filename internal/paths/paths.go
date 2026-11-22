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
	State  string // ~/.local/state/bud: machine config, project registry
	Cache  string // ~/.cache/bud: Neovim's stdpath("cache"), logs and bytecode
	// Runtime is $XDG_RUNTIME_DIR/bud, or empty when that is unset or relative.
	// There is no fallback to a world-writable dir like /tmp: sockets that carry
	// approvals must fail closed, so ReviewSocket is empty and the reviewer refuses
	Runtime string
	BinDir  string // ~/.local/bin: where install.sh puts bud unless told otherwise
	Home    string // $HOME, for what lives outside bud's own dirs
}

// Resolve reads XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME and XDG_CACHE_HOME, falling back
// to the XDG defaults under $HOME. Like the spec says, relative values are ignored
func Resolve() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	runtime := ""
	if dir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(dir) {
		runtime = filepath.Join(dir, AppName)
	}

	return &Paths{
		Runtime: runtime,
		Config:  filepath.Join(xdg("XDG_CONFIG_HOME", home, ".config"), AppName),
		Data:    filepath.Join(xdg("XDG_DATA_HOME", home, ".local", "share"), AppName),
		State:   filepath.Join(xdg("XDG_STATE_HOME", home, ".local", "state"), AppName),
		Cache:   filepath.Join(xdg("XDG_CACHE_HOME", home, ".cache"), AppName),
		BinDir:  filepath.Join(home, ".local", "bin"),
		Home:    home,
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

// UpdateState records the last background update check (D9)
func (p *Paths) UpdateState() string { return filepath.Join(p.State, "update.json") }

// UpdateLog is where the detached background update check writes
func (p *Paths) UpdateLog() string { return filepath.Join(p.Cache, "update.log") }

// Chat is where bud's chat runs Claude Code (JGA-165), a dir that's no
// project's, so its conversations are kept apart; nvim's stdpath("state")/chat
func (p *Paths) Chat() string { return filepath.Join(p.State, "chat") }

// Migrations records which migrations/ scripts have run
func (p *Paths) Migrations() string { return filepath.Join(p.State, "migrations.json") }

// InstallReceipt is written by install.sh and names the binary it installed (D20)
func (p *Paths) InstallReceipt() string { return filepath.Join(p.State, "install.json") }

// ClaudeConfig is bud's own Claude Code config dir (JGA-135): its
// CLAUDE_CONFIG_DIR unless ai.claude.config is "home"
func (p *Paths) ClaudeConfig() string { return filepath.Join(p.Data, "claude") }

// Plugins is where bud extracts its embedded Claude Code plugin (D142), one
// dir per version
func (p *Paths) Plugins() string { return filepath.Join(p.Data, "plugins") }

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

// SameDir says whether a and b name one dir, links resolved where they can be
func SameDir(a, b string) bool {
	resolve := func(d string) string {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			return r
		}
		return filepath.Clean(d)
	}
	return resolve(a) == resolve(b)
}

// Overlay is the clone of the private overlay repo (Suite D21, D59)
func (p *Paths) Overlay() string { return filepath.Join(p.Data, "overlay") }

// Content holds one dir per content version bud sync unpacked (Suite D60)
func (p *Paths) Content() string { return filepath.Join(p.Data, "content") }

// ContentCurrent is the link to the content version in use; switching it
// is the update, switching it back the rollback
func (p *Paths) ContentCurrent() string { return filepath.Join(p.Content(), "current") }

// Active is the link to the set of skill and plugin dirs a device takes,
// each a link into a content version or the overlay
func (p *Paths) Active() string { return filepath.Join(p.Data, "active") }

// SyncLedger records what bud sync wrote: versions, the overlay commit and
// each file's hash and layer, for bud doctor and the next sync
func (p *Paths) SyncLedger() string { return filepath.Join(p.State, "sync.json") }

// OpenCodeConfig is OpenCode's own config dir, ~/.config/opencode
func (p *Paths) OpenCodeConfig() string { return filepath.Join(filepath.Dir(p.Config), "opencode") }

// OpenCodeEnv is the env file op run fills OpenCode's keys from: variable
// names and op:// references, never the values (Suite D7)
func (p *Paths) OpenCodeEnv() string { return filepath.Join(p.State, "opencode.env") }

// HermesConfig is Hermes's config.yaml, which bud sync renders on the home
// server (Suite D48). Hermes reads it from ~/.hermes unless HERMES_HOME says
// otherwise, and bud's units leave HERMES_HOME unset
func (p *Paths) HermesConfig() string { return filepath.Join(p.Home, ".hermes", "config.yaml") }

// HermesSkills is Hermes's own skills dir, where bud sync copies the synced
// skills so Hermes can lock them (Suite D48)
func (p *Paths) HermesSkills() string { return filepath.Join(p.Home, ".hermes", "skills") }

// HermesEnv is the private profile's .env, which Hermes loads into its
// environment at start; bud sync refuses one that sets a cloud provider's
// key (Suite D95)
func (p *Paths) HermesEnv() string { return filepath.Join(p.Home, ".hermes", ".env") }

// HermesCloud is the home of Hermes's cloud profile (`hermes -p cloud`,
// Suite D95), which bud sync renders beside the private one
func (p *Paths) HermesCloud() string { return filepath.Join(p.Home, ".hermes", "profiles", "cloud") }

// HermesPlugins is Hermes's own plugins dir, where bud sync installs
// bud-signal, the plugin Signal approvals are answered through (Suite D30)
func (p *Paths) HermesPlugins() string { return filepath.Join(p.Home, ".hermes", "plugins") }

// HermesTiers is the action tiers bud hermes-gate reads, which bud sync
// renders beside Hermes's config (Suite D10, D48). It lives in bud's state
// dir, outside the Docker sandbox the agent's commands run in
func (p *Paths) HermesTiers() string { return filepath.Join(p.State, "hermes-tiers.json") }

// MCPGateway is the home server's MCP gateway config (Suite D72)
func (p *Paths) MCPGateway() string { return filepath.Join(p.State, "mcp-gateway.json") }

// MCPAudit is where the MCP gateway logs every tool call until the shared
// audit log (JGA-348) exists
func (p *Paths) MCPAudit() string { return filepath.Join(p.State, "mcp-audit.jsonl") }

// MailActions is bud mail-mcp's log of every move, restore and send (Suite
// D36, D106), which the cleanup digest and restores read
func (p *Paths) MailActions() string { return filepath.Join(p.State, "mail-actions.jsonl") }

// RunLogs holds private logs from bud run.
func (p *Paths) RunLogs() string { return filepath.Join(p.State, "run") }

// RouterSpend is the router's spend ledger (Suite D24, D64)
func (p *Paths) RouterSpend() string { return filepath.Join(p.State, "router-spend.json") }

// MemoryReviewStatus is where bud memory-review writes how many memory
// records wait on review, for the ops reviews tool
func (p *Paths) MemoryReviewStatus() string { return filepath.Join(p.State, "memory-review.json") }

// MCPGatewayApprovals is the per-server approval tiers bud sync renders from
// the env fragments; the gateway reads it from beside its own config
const MCPGatewayApprovalsFile = "mcp-gateway-approvals.json"

func (p *Paths) MCPGatewayApprovals() string { return filepath.Join(p.State, MCPGatewayApprovalsFile) }

// ApprovalStore is the approval store (internal/approval), private to the trusted processes
func (p *Paths) ApprovalStore() string { return filepath.Join(p.State, "approvals.json") }

// ReviewSocket is the unix socket the Signal plugin's reviewer listens on. It is
// empty when there is no runtime dir, and callers must treat that as unavailable
func (p *Paths) ReviewSocket() string {
	if p.Runtime == "" {
		return ""
	}

	return filepath.Join(p.Runtime, "review.sock")
}

// MemorySocket is the working-memory service's socket, which its unit
// (server/memory/bud-memory.service) makes as %t/bud-memory/memory.sock: a
// sibling of Runtime, since that service owns its own runtime dir. It is
// empty when there is no runtime dir
func (p *Paths) MemorySocket() string {
	if p.Runtime == "" {
		return ""
	}

	return filepath.Join(filepath.Dir(p.Runtime), "bud-memory", "memory.sock")
}

// ReviewSecret is the 0600 file holding the reviewer secret, defense in depth on top of
// the socket's permissions. It lives in state so it survives a reboot
func (p *Paths) ReviewSecret() string { return filepath.Join(p.State, "review.secret") }

// KB is the home server's clone of bud-kb, the KB's pages (Suite D20, D122)
func (p *Paths) KB() string { return filepath.Join(p.Data, "kb") }

// KBSite holds the KB site's builds and the link to the live one
func (p *Paths) KBSite() string { return filepath.Join(p.State, "kb-site") }

// CuratorState is what the curator has read and promoted, kept outside the
// KB so a reverted curator commit isn't redone from the same evidence
func (p *Paths) CuratorState() string { return filepath.Join(p.State, "curator.json") }
