// Package agent knows the AI backends bud talks to over the Agent Client
// Protocol (D131): how each one starts, and whether the configured one is
// ready. The editor's bud.ai.agent builds the same command lines; this side
// checks them for bud ai status and bud doctor.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jgarner.dev/bud/internal/config"
)

// Install says how to get each backend's command, for messages
var Install = map[string]string{
	"openclaw": "npm install -g openclaw",
	"claude":   "npm install -g @agentclientprotocol/claude-agent-acp",
	"codex":    "npm install -g @zed-industries/codex-acp",
	"opencode": "npm install -g @opencode/cli",
}

// Argv is the command line that starts the backend s names as an ACP agent
func Argv(s config.Settings) ([]string, error) {
	switch s.AI.Backend {
	case "openclaw", "":
		argv := []string{"openclaw", "acp"}
		if u := s.AI.OpenClaw.URL; u != "" {
			argv = append(argv, "--url", u)
		}
		if f := s.AI.OpenClaw.TokenFile; f != "" {
			argv = append(argv, "--token-file", expandHome(f))
		}
		return argv, nil
	case "claude":
		return []string{"claude-agent-acp"}, nil
	case "codex":
		return []string{"codex-acp"}, nil
	case "opencode":
		return []string{"opencode", "acp"}, nil
	case "custom":
		if len(s.AI.Custom.Command) == 0 {
			return nil, fmt.Errorf("ai.backend is custom but ai.custom.command is empty")
		}
		return append([]string{}, s.AI.Custom.Command...), nil
	}
	return nil, fmt.Errorf("ai.backend: %q isn't a backend bud knows (%s)", s.AI.Backend, strings.Join(config.Backends, ", "))
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Status is what Check found
type Status struct {
	Backend string
	Argv    []string
	Bin     string // the command's path, empty when it isn't installed
	Version string
	// for openclaw: the Gateway's address, and whether it answered
	Gateway   string
	Reachable bool
	// what's wrong, each with what to do about it; empty when it's ready
	Problems []Problem
}

type Problem struct {
	What, Fix string
	// Hard means the backend can't work at all; otherwise it may still
	Hard bool
}

// Ready says whether nothing stops the backend working
func (s Status) Ready() bool {
	for _, p := range s.Problems {
		if p.Hard {
			return false
		}
	}
	return true
}

// swappable in tests
var (
	lookPath = exec.LookPath
	// version runs bin --version with a short limit; an agent adapter that
	// has no --version is fine, so failures just leave it blank
	versionOf = func(bin string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "--version").Output()
		if err != nil {
			return ""
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		return line
	}
	dial = func(addr string) error {
		c, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			c.Close()
		}
		return err
	}
)

// Check looks at the backend s names: its command installed, and for
// openclaw the Gateway's URL, token file and whether it answers. quick
// skips anything that waits on the network
func Check(s config.Settings, quick bool) Status {
	st := Status{Backend: s.AI.Backend}
	if st.Backend == "" {
		st.Backend = "openclaw"
	}
	argv, err := Argv(s)
	if err != nil {
		st.Problems = append(st.Problems, Problem{What: err.Error(), Fix: "bud config set ai.backend openclaw", Hard: true})
		return st
	}
	st.Argv = argv
	bin, err := lookPath(argv[0])
	if err != nil {
		fix := Install[st.Backend]
		if fix == "" {
			fix = "install " + argv[0] + " or change ai.custom.command"
		}
		st.Problems = append(st.Problems, Problem{What: argv[0] + " isn't installed", Fix: fix, Hard: true})
	} else {
		st.Bin = bin
		if !quick {
			st.Version = versionOf(bin)
		}
	}
	if st.Backend == "openclaw" {
		checkOpenClaw(&st, s.AI.OpenClaw, quick, st.Bin)
	}
	return st
}

// gatewayHealth asks the openclaw CLI about the Gateway it reaches with no
// --url, which proves the token too: ok, or OpenClaw's reason it isn't.
// With --url it wants the token on its command line, where any user's ps
// would show it, so bud only asks this of the CLI's own Gateway; swappable
var gatewayHealth = func(bin string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, "gateway", "health", "--json", "--timeout", "8000").Output()
	var h struct {
		OK    bool `json:"ok"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(out, &h) != nil {
		return false, "openclaw gateway health gave no answer"
	}
	if h.OK {
		return true, ""
	}
	if h.Error != nil {
		line, _, _ := strings.Cut(h.Error.Message, "\n")
		return false, line
	}
	return false, "the Gateway isn't healthy"
}

// checkOpenClaw checks the Gateway settings (D132)
func checkOpenClaw(st *Status, oc config.OpenClaw, quick bool, bin string) {
	if f := oc.TokenFile; f != "" {
		path := expandHome(f)
		info, err := os.Stat(path)
		switch {
		case err != nil:
			st.Problems = append(st.Problems, Problem{
				What: "the Gateway token file " + f + " isn't there",
				Fix:  "put the Gateway token in " + f + " (chmod 600), or bud config unset ai.openclaw.token_file",
				Hard: true,
			})
		case info.Mode().Perm()&0o077 != 0:
			st.Problems = append(st.Problems, Problem{
				What: fmt.Sprintf("the Gateway token file %s can be read by others (%#o)", f, info.Mode().Perm()),
				Fix:  "chmod 600 " + f,
			})
		}
	}
	target := oc.URL
	if target == "" {
		// the CLI's own: gateway.remote.url, else a Gateway on this machine
		target = cliGateway()
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" {
		st.Problems = append(st.Problems, Problem{
			What: fmt.Sprintf("ai.openclaw.url %q isn't a ws:// or wss:// URL", target),
			Fix:  "bud config set ai.openclaw.url wss://HOST:18789",
			Hard: true,
		})
		return
	}
	if u.Scheme == "ws" && !plainOK(u.Hostname()) {
		st.Problems = append(st.Problems, Problem{
			What: "ai.openclaw.url uses plain ws:// to " + u.Hostname() + ", which OpenClaw refuses off loopback, private and Tailscale addresses",
			Fix:  "use wss://, or reach the Gateway over Tailscale (a .ts.net name)",
			Hard: true,
		})
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"ws": "80", "wss": "443"}[u.Scheme]
	}
	st.Gateway = net.JoinHostPort(u.Hostname(), port)
	if quick {
		return
	}
	if oc.URL == "" && bin != "" {
		// the CLI's own Gateway: a real health check, token and all
		if ok, why := gatewayHealth(bin); ok {
			st.Reachable = true
		} else {
			st.Problems = append(st.Problems, Problem{
				What: "the Gateway at " + st.Gateway + " isn't answering bud: " + why,
				Fix:  "start it (openclaw gateway, or its service), or check its token (openclaw gateway auth-token)",
			})
		}
		return
	}
	if err := dial(st.Gateway); err != nil {
		st.Problems = append(st.Problems, Problem{
			What: "the Gateway at " + st.Gateway + " didn't answer: " + err.Error(),
			Fix:  "start it on the server (openclaw gateway, or its service), and check Tailscale is up",
		})
		return
	}
	st.Reachable = true
}

// openclawConfig is OpenClaw's own config file; swappable in tests
var openclawConfig = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

// cliGateway is the Gateway the openclaw CLI reaches with no --url: its
// gateway.remote.url, else this machine's on gateway.port (18789 by
// default). A config bud can't read as plain JSON counts as none
func cliGateway() string {
	local := "ws://127.0.0.1:18789"
	data, err := os.ReadFile(openclawConfig())
	if err != nil {
		return local
	}
	var cfg struct {
		Gateway struct {
			Port   int `json:"port"`
			Remote struct {
				URL string `json:"url"`
			} `json:"remote"`
		} `json:"gateway"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return local
	}
	if cfg.Gateway.Remote.URL != "" {
		return cfg.Gateway.Remote.URL
	}
	if cfg.Gateway.Port > 0 {
		return fmt.Sprintf("ws://127.0.0.1:%d", cfg.Gateway.Port)
	}
	return local
}

// plainOK says whether OpenClaw takes plain ws:// to host: loopback,
// private, link-local and CGNAT addresses, .local and .ts.net names
func plainOK(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".ts.net") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

// SavedSession is the ACP session id the editor saved for the project at
// root on backend (bud.ai.agent, D139), empty when there's none yet
func SavedSession(stateDir, root, backend string) string {
	data, err := os.ReadFile(filepath.Join(stateDir, "agent-sessions.json"))
	if err != nil {
		return ""
	}
	var ids map[string]struct {
		Backend string `json:"backend"`
		ID      string `json:"id"`
	}
	if json.Unmarshal(data, &ids) != nil {
		return ""
	}
	if saved, ok := ids[root]; ok && saved.Backend == backend {
		return saved.ID
	}
	return ""
}

// BridgeKey is the Gateway session key OpenClaw gives the session its ACP
// bridge made with id
func BridgeKey(id, agentID string) string {
	if agentID == "" {
		agentID = "main"
	}
	return "agent:" + agentID + ":acp-bridge:" + id
}

// ChatKey is the Gateway session bud chat and the editor's chat use: one
// conversation that's no project's
func ChatKey(agentID string) string {
	if agentID == "" {
		agentID = "main"
	}
	return "agent:" + agentID + ":bud:chat"
}

// TUI is the backend's own TUI for the session key (OpenClaw) or dir (the
// others): its argv and the environment it adds. The claude backend's TUI
// is Claude Code's, which the claude commands build; custom has none
func TUI(s config.Settings, key, dir string) ([]string, []string, error) {
	switch s.AI.Backend {
	case "openclaw", "":
		argv := []string{"openclaw", "tui"}
		if u := s.AI.OpenClaw.URL; u != "" {
			argv = append(argv, "--url", u)
		}
		// no key: OpenClaw's own main session
		if key != "" {
			argv = append(argv, "--session", key)
		}
		var env []string
		if f := s.AI.OpenClaw.TokenFile; f != "" {
			data, err := os.ReadFile(expandHome(f))
			if err != nil {
				return nil, nil, fmt.Errorf("reading the Gateway token: %w", err)
			}
			line, _, _ := strings.Cut(string(data), "\n")
			// the TUI takes a token only on its command line or here
			env = append(env, "OPENCLAW_GATEWAY_TOKEN="+strings.TrimSpace(line))
		}
		return argv, env, nil
	case "codex":
		return []string{"codex"}, nil, nil
	case "opencode":
		return []string{"opencode", dir}, nil, nil
	}
	return nil, nil, fmt.Errorf("the %s backend has no TUI bud knows", s.AI.Backend)
}
