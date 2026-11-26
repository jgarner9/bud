// Package env reads env fragments (Suite D57) and the bud block inside them
// that every runtime adapter shares. A fragment is JSONC laid over a
// runtime's config: its top level is OpenCode's, and the "bud" key holds
// what isn't any one runtime's, so each adapter renders it in its own shape.
// The MCP servers a device uses, the MCP gateway they go through and the
// privacy tier the device's sessions send are described once there, and
// rendered into both OpenCode and Hermes (Suite D72-D79, D81, D85-D87).
// The bud block is documented in docs/internals/sync.md
package env

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/argrule"
	"jgarner.dev/bud/internal/registry"
)

// Fragment is one env fragment's JSONC
type Fragment struct {
	Name string // for errors, such as public/base or overlay/laptop
	Src  []byte
}

// Merge lays the fragments over each other in order: objects merge key by
// key, arrays append (so an overlay's permission rules come after the
// public ones), and anything else replaces
func Merge(frags []Fragment) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range frags {
		var frag map[string]any
		if err := json.Unmarshal(registry.StripJSONC(f.Src), &frag); err != nil {
			return nil, fmt.Errorf("env fragment %s: %w", f.Name, err)
		}
		replaceServerLists(out, frag)
		MergeInto(out, frag)
	}

	return out, nil
}

// replacedLists are the bud.mcp.<name> lists a later fragment replaces
// rather than appends to: a server's tiers, so an overlay can narrow them,
// and its command, so an overlay can swap the executable
var replacedLists = []string{"privacy", "command", "approval"}

// replaceServerLists drops from out each list in replacedLists that frag
// sets for the same server, so the merge that follows replaces it. An
// approval block that sets only when rules (Suite D116) keeps the earlier
// tools and adds its rules after the earlier ones, so an overlay can add a
// rule, such as mail to Jason's own address, without copying the tools
func replaceServerLists(out, frag map[string]any) {
	next := serverMap(frag)
	cur := serverMap(out)
	for name, v := range next {
		ns, ok := v.(map[string]any)
		if !ok {
			continue
		}
		cs, ok := cur[name].(map[string]any)
		if !ok {
			continue
		}
		for _, k := range replacedLists {
			if _, ok := ns[k]; ok && !(k == "approval" && onlyWhen(ns[k])) {
				delete(cs, k)
			}
		}
	}
}

// onlyWhen says whether an approval block sets when rules and nothing else
func onlyWhen(v any) bool {
	a, ok := v.(map[string]any)
	if !ok || len(a) != 1 {
		return false
	}
	_, ok = a["when"]

	return ok
}

func serverMap(m map[string]any) map[string]any {
	b, _ := m["bud"].(map[string]any)
	servers, _ := b["mcp"].(map[string]any)

	return servers
}

// MergeInto lays src over dst, the way Merge lays fragments
func MergeInto(dst, src map[string]any) {
	for k, v := range src {
		switch sv := v.(type) {
		case map[string]any:
			if dv, ok := dst[k].(map[string]any); ok {
				MergeInto(dv, sv)
				continue
			}
		case []any:
			if dv, ok := dst[k].([]any); ok {
				dst[k] = append(slices.Clone(dv), sv...)
				continue
			}
		}
		dst[k] = v
	}
}

// Tiers are the privacy tiers (Suite D23)
var Tiers = []string{"P0", "P1", "P2"}

// PrivacyHeader carries a session's tier to the MCP gateway, which treats a
// request without it as P0 (internal/mcpgateway)
const PrivacyHeader = "X-Bud-Privacy"

// Bud is the bud block of the merged fragments
type Bud struct {
	// Gateway is the MCP gateway's base URL, such as http://<tailnet IP>:8790.
	// It lives in the overlay, since the address is personal. Empty means
	// none, and servers reached through it are left out
	Gateway string `json:"gateway,omitempty"`
	// Router is the model router's base URL, such as http://<tailnet IP>:8795
	// (Suite D70, D104). It lives in the overlay, since the address is
	// personal. When set, Hermes's private profile asks the router for
	// bud/manager instead of naming providers itself, so the router's health
	// fallback covers it. Empty means Hermes talks to the providers directly
	Router string `json:"router,omitempty"`
	// Privacy is the tier this device's sessions send the gateway. Empty
	// sends no header, which the gateway takes as P0
	Privacy string `json:"privacy,omitempty"`
	// MCP holds the device's MCP servers by name
	MCP map[string]Server `json:"mcp,omitempty"`
	// Hermes is laid over Hermes's config.yaml as it is (Suite D48)
	Hermes map[string]any `json:"hermes,omitempty"`
	// Tiers gives Hermes's own tools their action tier (Suite D10, D48):
	// allow runs alone, ask waits for Jason's approval and deny is refused.
	// Keys are Hermes's tool names or patterns (MCP tools are
	// mcp__<server>__<tool>): an exact name wins, then the longest pattern,
	// then "*". A later fragment sets a key over an earlier one's. Terminal
	// commands are tiered by the shell permission rules instead
	Tiers map[string]string `json:"tiers,omitempty"`
	// TiersWhen are when rules over Hermes's own tools (Suite D116): one
	// whose argument conditions hold decides over Tiers, the strictest
	// winning, though nothing loosens a deny. Tool names as in Tiers. A
	// later fragment's rules come after an earlier one's
	TiersWhen []argrule.Rule `json:"tiers_when,omitempty"`
	// SandboxShell tiers the commands Hermes's terminal runs in its Docker
	// sandbox (Suite D114), in place of the permission rules' shell rules,
	// which stay the coding devices'. Same globs, and the last match wins; a
	// later fragment's rules come after an earlier one's. Empty keeps the
	// permission rules' shell rules for Hermes too
	SandboxShell []ShellRule `json:"sandbox_shell,omitempty"`
	// OpenCodeSecrets are environment variables op run fills for OpenCode,
	// each an op:// reference, written to opencode.env beside the registry's
	// provider keys: the fragments' own keys, such as EXA_API_KEY for
	// websearch or GITHUB_PERSONAL_ACCESS_TOKEN for GitHub's MCP server. They
	// name the vault, so they belong in the overlay. A later fragment sets a
	// variable over an earlier one's
	OpenCodeSecrets map[string]string `json:"opencode_secrets,omitempty"`
}

// ShellRule is one sandbox shell rule: a command glob and its effect
type ShellRule struct {
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// Server is one MCP server. Exactly one of Gateway, URL and Command says how
// it's reached
type Server struct {
	// Gateway reaches it through the MCP gateway at <gateway>/<name>/mcp,
	// with the device's tier in X-Bud-Privacy
	Gateway bool `json:"gateway,omitempty"`
	// URL is a remote server's Streamable HTTP endpoint. A server with
	// neither URL nor Command is left out until a later fragment (usually
	// the overlay's) sets one
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Command starts a local server over stdio, its arguments after it
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Privacy lists the tiers whose sessions may use it; a P0 session gets
	// none of a server that leaves P0 out (Suite D81)
	Privacy  []string `json:"privacy"`
	Disabled bool     `json:"disabled,omitempty"`
	// HermesOnly exposes a loopback server to the P0 Hermes profile only.
	// Leave URL unset until the worker is installed.
	HermesOnly bool `json:"hermes_only,omitempty"`
	// Approval tiers a gateway server's tools for the gateway's review
	// (Suite D97, D103). Keys are tool-name globs with the runtime's
	// "<name>_" prefix, as in the permission rules; bud sync strips it, since
	// the gateway matches the upstream's own names. A tool no glob names asks
	Approval *Approval `json:"approval,omitempty"`
	// ReadOnly says every tool on a gateway server only reads, so the
	// gateway needs no approval block for it
	ReadOnly bool `json:"read_only,omitempty"`
	// MarksP0 says the server tags each P0 record it returns with
	// registry.P0Marker, as Bud memory does (JGA-389). The router keeps a
	// chat that receives one local, so the private Hermes profile may reach
	// a gateway server like this at P0 and see every tier
	MarksP0 bool `json:"marks_p0,omitempty"`
}

// Approval is a server's tool tiers, in the shape of the gateway's
// approval block
type Approval struct {
	Tools map[string]string `json:"tools,omitempty"`
	// When are argument rules (Suite D116), their tool globs with the
	// runtime's <name>_ prefix like Tools' keys
	When []argrule.Rule `json:"when,omitempty"`
}

// Remote says whether the resolved server is reached over HTTP
func (s Server) Remote() bool { return s.URL != "" }

// P0 says whether P0 sessions may use the server
func (s Server) P0() bool { return slices.Contains(s.Privacy, "P0") }

// Resolved is the bud block with every server's transport worked out
type Resolved struct {
	// Servers are the servers to render, by name, each with its URL and
	// headers or its command
	Servers map[string]Server
	// Left are the servers left out, by name, each with why
	Left   map[string]string
	Hermes map[string]any
	Tiers  map[string]string
	// Privacy is the device's tier, "" when no fragment sets one
	Privacy string
	// TiersWhen are the when rules over Hermes's own tools
	TiersWhen []argrule.Rule
	// P0Only are the servers only P0 sessions may use, sorted, every one the
	// block declares, left out or not, since a runtime elsewhere may still
	// reach it (Suite D119)
	P0Only []string
	// SandboxShell are Hermes's terminal rules, nil when no fragment sets them
	SandboxShell []ShellRule
	// Router is the model router's base URL, empty when none is set
	Router string
	// OpenCodeSecrets are the op:// references OpenCode's env file adds
	OpenCodeSecrets map[string]string
	// Gateway holds each enabled gateway server's approval settings, by
	// name, including servers left out of this device's runtimes, since the
	// gateway still serves them
	Gateway map[string]GatewayServer
}

// GatewayServer is what the gateway reads about one server beside its own
// config: the file bud sync renders (see GatewayApprovals)
type GatewayServer struct {
	// Tools are upstream tool-name globs, allow or ask. An empty block is
	// kept as {} (every tool asks), so only a server with no block omits it
	Tools    *map[string]string `json:"tools,omitempty"`
	When     []argrule.Rule     `json:"when,omitempty"`
	ReadOnly bool               `json:"read_only,omitempty"`
}

// GatewayApprovals renders the gateway's approvals file, or nil when no
// gateway server is declared
func (r *Resolved) GatewayApprovals() ([]byte, error) {
	if len(r.Gateway) == 0 {
		return nil, nil
	}
	b, err := json.MarshalIndent(map[string]any{"servers": r.Gateway}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render the gateway approvals: %w", err)
	}

	return append(b, '\n'), nil
}

// RouterP0Servers renders the router's list of P0-only servers
func (r *Resolved) RouterP0Servers() ([]byte, error) {
	b, err := json.MarshalIndent(map[string]any{"servers": r.P0Only}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render the router's P0 servers: %w", err)
	}

	return append(b, '\n'), nil
}

// Names are the resolved servers' names, sorted
func (r *Resolved) Names() []string { return sortedKeys(r.Servers) }

// Notes say which servers were left out and why, sorted
func (r *Resolved) Notes() []string {
	var out []string
	for _, n := range sortedKeys(r.Left) {
		out = append(out, fmt.Sprintf("MCP server %s is left out: %s", n, r.Left[n]))
	}

	return out
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var toolRe = regexp.MustCompile(`^[a-z0-9_*?-]+$`)

var envVarRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// Take removes the bud block from merged fragments and resolves it
func Take(merged map[string]any) (*Resolved, error) {
	raw, ok := merged["bud"]
	delete(merged, "bud")
	var b Bud
	if ok {
		buf, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(strings.NewReader(string(buf)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&b); err != nil {
			return nil, fmt.Errorf("env fragments: the bud block: %w", err)
		}
	}

	return b.Resolve()
}

// Resolve checks the block and works out each server's transport, reporting
// every problem at once
func (b *Bud) Resolve() (*Resolved, error) {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if b.Privacy != "" && !slices.Contains(Tiers, b.Privacy) {
		add("bud.privacy: %q isn't a tier (P0, P1, P2)", b.Privacy)
	}
	if b.Gateway != "" {
		if err := checkGateway(b.Gateway); err != nil {
			add("bud.gateway: %v", err)
		}
	}
	if b.Router != "" {
		if err := checkService(b.Router, "router", "8795", "bud adds /v1"); err != nil {
			add("bud.router: %v", err)
		}
	}

	for _, tool := range sortedKeys(b.Tiers) {
		if !toolRe.MatchString(tool) {
			add("bud.tiers.%s: a tool name or pattern is lowercase letters, digits, '_', '-', and * or ? as wildcards", tool)
		}
		if e := b.Tiers[tool]; e != "allow" && e != "ask" && e != "deny" {
			add("bud.tiers.%s: %q isn't allow, ask or deny", tool, e)
		}
	}

	errs = append(errs, argrule.Check("bud.tiers_when", b.TiersWhen, []string{"allow", "ask", "deny"}, func(tool string) string {
		switch {
		case !toolRe.MatchString(tool):
			return "a tool name or pattern is lowercase letters, digits, '_', '-', and * or ? as wildcards"
		case tool == "terminal" || tool == "*":
			return "the terminal is tiered by sandbox_shell or the shell rules; name another tool"
		}
		return ""
	})...)

	for i, r := range b.SandboxShell {
		if strings.TrimSpace(r.Resource) == "" {
			add("bud.sandbox_shell[%d]: the resource is a command glob, such as \"git push *\"", i)
		}
		if r.Effect != "allow" && r.Effect != "ask" && r.Effect != "deny" {
			add("bud.sandbox_shell[%d]: %q isn't allow, ask or deny", i, r.Effect)
		}
	}

	for _, v := range sortedKeys(b.OpenCodeSecrets) {
		if !envVarRe.MatchString(v) {
			add("bud.opencode_secrets.%s: a variable name is uppercase letters, digits and '_', not starting with a digit", v)
		}
		if !registry.IsOpRef(b.OpenCodeSecrets[v]) {
			add("bud.opencode_secrets.%s: the value must be an op://vault/item/field reference, never the secret itself", v)
		}
	}

	r := &Resolved{Servers: map[string]Server{}, Left: map[string]string{}, Gateway: map[string]GatewayServer{}, Hermes: b.Hermes, Privacy: b.Privacy, Tiers: b.Tiers, TiersWhen: b.TiersWhen, SandboxShell: b.SandboxShell, Router: strings.TrimSuffix(b.Router, "/"), OpenCodeSecrets: b.OpenCodeSecrets}
	r.P0Only = []string{}
	for _, name := range sortedKeys(b.MCP) {
		s := b.MCP[name]
		at := "bud.mcp." + name
		if slices.Equal(s.Privacy, []string{"P0"}) {
			r.P0Only = append(r.P0Only, name)
		}
		if !nameRe.MatchString(name) {
			add("%s: the name must be lowercase letters, digits and '-'", at)
			continue
		}
		if len(s.Privacy) == 0 {
			add("%s.privacy: list the tiers whose sessions may use it (P0, P1, P2)", at)
		}
		for _, t := range s.Privacy {
			if !slices.Contains(Tiers, t) {
				add("%s.privacy: %q isn't a tier (P0, P1, P2)", at, t)
			}
		}
		ways := 0
		for _, set := range []bool{s.Gateway, s.URL != "", len(s.Command) > 0} {
			if set {
				ways++
			}
		}
		if ways > 1 {
			add("%s: set one of gateway, url and command", at)
			continue
		}
		if s.HermesOnly && (!slices.Equal(s.Privacy, []string{"P0"}) || s.Gateway || len(s.Command) > 0 || (s.URL != "" && !loopbackService(s.URL))) {
			add("%s: hermes_only requires P0 and a direct loopback URL, or no URL until configured", at)
			continue
		}
		if s.MarksP0 && (!s.P0() || s.URL != "" || len(s.Command) > 0) {
			// only the gateway checks a marked server's tier; reached
			// directly, a cloud session could get its P0 records
			add("%s: marks_p0 needs P0 in privacy and a gateway server (gateway true, no url or command)", at)
			continue
		}
		if !s.Gateway && (s.Approval != nil || s.ReadOnly) {
			add("%s: approval and read_only apply to gateway servers only", at)
			continue
		}
		if s.ReadOnly && s.Approval != nil {
			add("%s: set read_only or approval, not both", at)
			continue
		}
		if gs, bad := gatewayServer(name, s); len(bad) > 0 {
			for _, m := range bad {
				add("%s.approval.%s", at, m)
			}
			continue
		} else if s.Gateway && !s.Disabled {
			r.Gateway[name] = gs
		}
		if s.Gateway && (len(s.Headers) > 0 || len(s.Env) > 0) {
			add("%s: a gateway server takes no headers or env; the gateway adds its key", at)
			continue
		}
		if s.URL != "" && !validURL(s.URL) {
			add("%s.url: %q isn't an http(s) URL", at, s.URL)
			continue
		}
		switch {
		case s.Disabled:
			r.Left[name] = "disabled"
		case s.Gateway && b.Gateway == "":
			r.Left[name] = "it goes through the MCP gateway, and no fragment sets bud.gateway (your overlay's env sets it)"
		case s.Gateway:
			s.URL = strings.TrimSuffix(b.Gateway, "/") + "/" + name + "/mcp"
			if b.Privacy != "" {
				s.Headers = map[string]string{PrivacyHeader: b.Privacy}
			}
			r.Servers[name] = s
		case ways == 0:
			r.Left[name] = "no fragment sets its url or command (your overlay's env sets it)"
		default:
			r.Servers[name] = s
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("env fragments: %s", strings.Join(errs, "; "))
	}

	return r, nil
}

// gatewayServer checks a server's approval tiers and strips the "<name>_"
// prefix from their keys
func gatewayServer(name string, s Server) (GatewayServer, []string) {
	gs := GatewayServer{ReadOnly: s.ReadOnly}
	if s.Approval == nil {
		return gs, nil
	}
	var bad []string
	tools := map[string]string{}
	gs.Tools = &tools
	for _, k := range sortedKeys(s.Approval.Tools) {
		up, ok := strings.CutPrefix(k, name+"_")
		switch {
		case !ok || up == "":
			bad = append(bad, fmt.Sprintf("tools: %q must start with %s_, as the permission rules' tool names do", k, name))
			continue
		}
		if _, err := path.Match(up, ""); err != nil || !toolRe.MatchString(up) {
			bad = append(bad, fmt.Sprintf("tools: %q isn't a valid tool-name glob", k))
			continue
		}
		if v := s.Approval.Tools[k]; v != "allow" && v != "ask" {
			bad = append(bad, fmt.Sprintf("tools: %s: %q isn't allow or ask", k, v))
			continue
		}
		tools[up] = s.Approval.Tools[k]
	}
	prefix := func(tool string) string {
		if up, ok := strings.CutPrefix(tool, name+"_"); !ok || up == "" || !toolRe.MatchString(up) {
			return fmt.Sprintf("%q must start with %s_, as the permission rules' tool names do", tool, name)
		}
		return ""
	}
	bad = append(bad, argrule.Check("when", s.Approval.When, []string{"allow", "ask"}, prefix)...)
	if len(bad) == 0 {
		for _, r := range s.Approval.When {
			r.Tool = strings.TrimPrefix(r.Tool, name+"_")
			gs.When = append(gs.When, r)
		}
	}

	return gs, bad
}

// loopbackService keeps an internal-only server literal, so an environment
// substitution cannot send its Authorization header outside this host.
func loopbackService(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	ip, err := netip.ParseAddr(u.Hostname())
	return err == nil && ip.Unmap().IsLoopback()
}

// validURL accepts an http(s) URL. {env:VAR} references are filled at
// launch, so a URL that is one reference is taken as it is, and in one that
// holds some, each stands in for a plain word while the rest is checked
func validURL(raw string) bool {
	if m := EnvRef.FindString(raw); m == raw {
		return true
	}
	u, err := url.Parse(EnvRef.ReplaceAllString(raw, "env"))

	return err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http")
}

// Declared says whether name is one of the block's servers, rendered or left out
func (r *Resolved) Declared(name string) bool {
	_, rendered := r.Servers[name]
	_, left := r.Left[name]

	return rendered || left
}

// CheckExtras checks a runtime's own entries for servers (OpenCode's
// mcp.servers, Hermes's mcp_servers): each must name a server the bud block
// declares, so none goes without its tiers and the private agent's rules,
// and none may set a field in transport, which comes from the bud block. It
// drops the entries of servers that were left out, so none becomes an
// entry without a transport
func (r *Resolved) CheckExtras(where string, extras map[string]any, transport []string) error {
	var errs []string
	for _, name := range sortedKeys(extras) {
		if !r.Declared(name) {
			errs = append(errs, fmt.Sprintf("%s.%s isn't in bud.mcp; declare it there with its privacy tiers, and keep only extra fields here", where, name))
			continue
		}
		if _, ok := r.Servers[name]; !ok {
			delete(extras, name)
			continue
		}
		e, _ := extras[name].(map[string]any)
		for _, k := range transport {
			if _, ok := e[k]; ok {
				errs = append(errs, fmt.Sprintf("%s.%s.%s comes from bud.mcp.%s; set it there", where, name, k, name))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("env fragments: %s", strings.Join(errs, "; "))
	}

	return nil
}

// checkGateway accepts an http(s) URL whose host is a loopback or tailnet IP
// address, since the gateway refuses a Host that isn't one (it guards
// against DNS rebinding), so a MagicDNS name would never get through
func checkGateway(raw string) error {
	return checkService(raw, "gateway", "8790", "bud adds /<server>/mcp")
}

// checkService checks a home-server service's base URL: http(s), a loopback
// or tailnet IP with a port, and no path, since the service listens on the
// tailnet only (Suite D72) and bud adds the path itself
func checkService(raw, name, port, path string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%q isn't an http(s) URL such as http://<tailnet IP>:%s", raw, port)
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%q: give only the scheme, address and port; %s", raw, path)
	}
	ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]"))
	if err != nil {
		return fmt.Errorf("%q names a host, and the %s answers only its IP address; use the tailnet IP (tailscale ip -4)", raw, name)
	}
	if !ip.IsLoopback() && !tailnet(ip) {
		return fmt.Errorf("%q isn't a loopback or tailnet address, and the %s listens on the tailnet only", raw, name)
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return fmt.Errorf("%q: give the %s's port, such as :%s", raw, name, port)
	}

	return nil
}

var tailnetRanges = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

func tailnet(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range tailnetRanges {
		if p.Contains(ip) {
			return true
		}
	}

	return false
}

// EnvRef matches an {env:VAR} reference, the form fragments use for a value
// a runtime fills from its environment at launch
var EnvRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
