package agent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
)

// fakeTools swaps the lookups: names on the fake PATH, a fixed version, and
// a dial that answers for the addresses in up
func fakeTools(t *testing.T, installed []string, up ...string) *[]string {
	t.Helper()
	dialed := &[]string{}
	origLook, origVersion, origDial := lookPath, versionOf, dial
	t.Cleanup(func() { lookPath, versionOf, dial = origLook, origVersion, origDial })
	lookPath = func(name string) (string, error) {
		for _, n := range installed {
			if n == name {
				return "/fake/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	versionOf = func(bin string) string { return filepath.Base(bin) + " 1.2.3" }
	dial = func(addr string) error {
		*dialed = append(*dialed, addr)
		for _, a := range up {
			if a == addr {
				return nil
			}
		}
		return errors.New("connection refused")
	}
	return dialed
}

func settings(mod func(*config.Settings)) config.Settings {
	s := config.Defaults()
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestArgv(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, c := range []struct {
		name string
		mod  func(*config.Settings)
		want []string
		err  string
	}{
		{"openclaw default", nil, []string{"openclaw", "acp"}, ""},
		{"openclaw gateway", func(s *config.Settings) {
			s.AI.OpenClaw.URL = "wss://home.example.ts.net:18789"
			s.AI.OpenClaw.TokenFile = "~/.config/openclaw/gateway-token"
		}, []string{"openclaw", "acp", "--url", "wss://home.example.ts.net:18789", "--token-file", filepath.Join(home, ".config/openclaw/gateway-token")}, ""},
		{"claude", func(s *config.Settings) { s.AI.Backend = "claude" }, []string{"claude-agent-acp"}, ""},
		{"codex", func(s *config.Settings) { s.AI.Backend = "codex" }, []string{"codex-acp"}, ""},
		{"opencode", func(s *config.Settings) { s.AI.Backend = "opencode" }, []string{"opencode", "acp"}, ""},
		{"custom", func(s *config.Settings) {
			s.AI.Backend = "custom"
			s.AI.Custom.Command = []string{"gemini", "--experimental-acp"}
		}, []string{"gemini", "--experimental-acp"}, ""},
		{"custom empty", func(s *config.Settings) { s.AI.Backend = "custom" }, nil, "ai.custom.command is empty"},
		{"unknown", func(s *config.Settings) { s.AI.Backend = "gpt" }, nil, `"gpt" isn't a backend`},
	} {
		got, err := Argv(settings(c.mod))
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: error %v, want %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestCheckNotInstalled(t *testing.T) {
	fakeTools(t, nil)
	st := Check(settings(nil), false)
	if st.Ready() || st.Bin != "" {
		t.Fatalf("ready with nothing installed: %+v", st)
	}
	if p := st.Problems[0]; !strings.Contains(p.What, "openclaw isn't installed") || p.Fix != "npm install -g openclaw" {
		t.Errorf("problem: %+v", p)
	}
}

func TestCheckOpenClawLocal(t *testing.T) {
	dialed := fakeTools(t, []string{"openclaw"})
	st := Check(settings(nil), false)
	if !st.Ready() || len(st.Problems) != 0 {
		t.Fatalf("not ready: %+v", st.Problems)
	}
	if st.Version != "openclaw 1.2.3" || st.Bin != "/fake/bin/openclaw" {
		t.Errorf("version %q, bin %q", st.Version, st.Bin)
	}
	if len(*dialed) != 0 {
		t.Errorf("dialed %v with no URL set", *dialed)
	}
}

func TestCheckOpenClawGateway(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "gateway-token")
	os.WriteFile(token, []byte("secret\n"), 0o600)
	dialed := fakeTools(t, []string{"openclaw"}, "home.example.ts.net:18789")
	st := Check(settings(func(s *config.Settings) {
		s.AI.OpenClaw.URL = "wss://home.example.ts.net:18789"
		s.AI.OpenClaw.TokenFile = token
	}), false)
	if !st.Ready() || len(st.Problems) != 0 || !st.Reachable {
		t.Fatalf("gateway up: %+v", st)
	}
	if !reflect.DeepEqual(*dialed, []string{"home.example.ts.net:18789"}) {
		t.Errorf("dialed %v", *dialed)
	}

	// quick doesn't touch the network
	*dialed = nil
	if st := Check(settings(func(s *config.Settings) { s.AI.OpenClaw.URL = "wss://down.example.com" }), true); len(*dialed) != 0 || len(st.Problems) != 0 {
		t.Errorf("quick: dialed %v, problems %+v", *dialed, st.Problems)
	}
}

func TestCheckOpenClawProblems(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose")
	os.WriteFile(loose, []byte("secret\n"), 0o644)
	fakeTools(t, []string{"openclaw"})
	for _, c := range []struct {
		name      string
		url, file string
		want      string
		hard      bool
	}{
		{"missing token file", "", filepath.Join(dir, "nope"), "isn't there", true},
		{"readable token file", "", loose, "can be read by others", false},
		{"not a websocket", "https://home:18789", "", "isn't a ws:// or wss:// URL", true},
		{"plain ws on the internet", "ws://gateway.example.com:18789", "", "plain ws://", true},
		{"gateway down", "wss://down.example.com:18789", "", "didn't answer", false},
	} {
		st := Check(settings(func(s *config.Settings) {
			s.AI.OpenClaw.URL, s.AI.OpenClaw.TokenFile = c.url, c.file
		}), false)
		if len(st.Problems) == 0 {
			t.Errorf("%s: no problem found", c.name)
			continue
		}
		p := st.Problems[0]
		if !strings.Contains(p.What, c.want) || p.Hard != c.hard || p.Fix == "" {
			t.Errorf("%s: %+v, want %q hard=%v", c.name, p, c.want, c.hard)
		}
	}
}

func TestPlainOK(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "192.168.1.20": true, "10.0.0.5": true,
		"100.101.102.103": true, "home.tailnet.ts.net": true, "nas.local": true,
		"gateway.example.com": false, "8.8.8.8": false,
	} {
		if got := plainOK(host); got != want {
			t.Errorf("plainOK(%s) = %v, want %v", host, got, want)
		}
	}
}
