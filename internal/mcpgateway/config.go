// Package mcpgateway is the home server's MCP gateway (Suite D72): it
// forwards MCP requests from devices on the tailnet to keyed upstream
// servers, adds each server's key from the environment op run fills, logs
// every tool call and refuses cloud tools to P0 sessions (Suite D81). See
// docs/internals/mcp-gateway.md
package mcpgateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	models "jgarner.dev/bud/internal/registry"
	"jgarner.dev/bud/internal/tailnet"
)

// Config is mcp-gateway.json
type Config struct {
	Listen   string            `json:"listen"`
	AuditLog string            `json:"audit_log,omitempty"`
	Servers  map[string]Server `json:"servers"`
}

// Server is one upstream MCP server
type Server struct {
	URL     string   `json:"url"`
	Privacy []string `json:"privacy"`
	Key     *Key     `json:"key,omitempty"`
	// Approval turns on review for this server's approval-tier tool calls.
	// Without it every call passes through as before
	Approval *Approval `json:"approval,omitempty"`
}

// Approval says which of a server's tools need a review before the call goes
// upstream. Tools maps upstream tool-name globs (path.Match syntax, e.g.
// "get_*") to "allow" or "ask"; a tool no glob names asks, so a new upstream
// tool fails closed. When several globs match, "ask" wins
type Approval struct {
	Tools map[string]string `json:"tools,omitempty"`
}

// Asks says whether a call to tool needs a review
func (a *Approval) Asks(tool string) bool {
	allowed := false
	for _, glob := range sortedKeys(a.Tools) {
		if ok, _ := path.Match(glob, tool); !ok {
			continue
		}
		if a.Tools[glob] != "allow" {
			return true
		}
		allowed = true
	}
	return !allowed
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Key says where an upstream reads its key from and which environment
// variable op run puts it in (Suite D7, D73)
type Key struct {
	Header string `json:"header"`
	Prefix string `json:"prefix,omitempty"`
	Env    string `json:"env"`
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	envRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Read loads and validates a config file
func Read(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no gateway config at %s: copy server/mcp-gateway/mcp-gateway.example.json there, or pass --config", path)
		}
		return nil, fmt.Errorf("read the gateway config: %w", err)
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Validate reports every problem at once, each naming the field to fix
func (c *Config) Validate() error {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	if host, _, err := net.SplitHostPort(c.Listen); err != nil {
		add("listen: %q isn't host:port", c.Listen)
	} else if !tailnet.Inside(host) || host == "localhost" {
		add("listen: %q must be a loopback or tailnet IP address, so the gateway is never reachable from outside the tailnet (Suite D72)", host)
	}
	if len(c.Servers) == 0 {
		add("servers: none configured")
	}
	for _, name := range sortedNames(c.Servers) {
		s := c.Servers[name]
		at := "servers." + name
		if !nameRe.MatchString(name) {
			add("%s: names are lowercase letters, digits and -", at)
		}
		u, err := url.Parse(s.URL)
		inside := false
		switch {
		case err != nil || u.Host == "":
			add("%s.url: %q isn't a URL", at, s.URL)
		case u.Scheme == "https":
			inside = tailnet.Inside(u.Hostname())
		case u.Scheme == "http" && tailnet.Inside(u.Hostname()):
			inside = true
		default:
			add("%s.url: use https, or http only to a loopback or tailnet host", at)
		}
		if len(s.Privacy) == 0 {
			add("%s.privacy: list the session tiers that may use it (P1, P2)", at)
		}
		for _, t := range s.Privacy {
			if !slices.Contains(models.Tiers, t) {
				add("%s.privacy: %q isn't a tier (P0, P1, P2)", at, t)
			}
		}
		if slices.Contains(s.Privacy, "P0") && !inside {
			add("%s.privacy: allows P0, but %s is outside the house; P0 sessions may use only tools that never leave it (Suite D81)", at, s.URL)
		}
		if a := s.Approval; a != nil {
			for _, glob := range sortedKeys(a.Tools) {
				if _, err := path.Match(glob, ""); err != nil {
					add("%s.approval.tools: %q isn't a valid tool-name glob", at, glob)
				}
				if v := a.Tools[glob]; v != "allow" && v != "ask" {
					add("%s.approval.tools.%s: %q isn't allow or ask", at, glob, v)
				}
			}
		}
		if u != nil && u.RawQuery != "" && s.Approval != nil {
			add("%s.url: has a query string, which an approval review would show; put credentials in the key header (key.header, key.env) instead", at)
		}
		if k := s.Key; k != nil {
			if k.Header == "" {
				add("%s.key.header: name the header the server reads its key from", at)
			}
			if !envRe.MatchString(k.Env) {
				add("%s.key.env: %q isn't an environment variable name", at, k.Env)
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// keys reads each server's key from the environment, failing on the first
// server whose variable is missing or empty
func (c *Config) keys(getenv func(string) string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range sortedNames(c.Servers) {
		k := c.Servers[name].Key
		if k == nil {
			continue
		}
		v := getenv(k.Env)
		if v == "" {
			return nil, fmt.Errorf("servers.%s: %s is empty; start the gateway under op run with an env file that sets it (see server/mcp-gateway/README.md)", name, k.Env)
		}
		out[name] = k.Prefix + v
	}
	return out, nil
}

func sortedNames(m map[string]Server) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
