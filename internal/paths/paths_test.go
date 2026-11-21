package paths

import (
	"path/filepath"
	"testing"
)

func TestResolveDefaults(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct{ name, got, want string }{
		{"config", p.Config, "/home/me/.config/bud"},
		{"data", p.Data, "/home/me/.local/share/bud"},
		{"state", p.State, "/home/me/.local/state/bud"},
		{"cache", p.Cache, "/home/me/.cache/bud"},
		{"install receipt", p.InstallReceipt(), "/home/me/.local/state/bud/install.json"},
		{"bin dir", p.BinDir, "/home/me/.local/bin"},
		{"machine config", p.MachineConfig(), "/home/me/.local/state/bud/config.json"},
		{"projects", p.Projects(), "/home/me/.local/state/bud/projects.json"},
		{"user tools", p.UserTools(), "/home/me/.local/share/bud/user-tools"},
		{"language specs", p.LanguageSpecs(), "/home/me/.config/bud/languages"},
		{"user language specs", p.UserLanguageSpecs(), "/home/me/.local/share/bud/user-tools/languages"},
		{"prev binary", p.PrevBinary(), "/home/me/.local/share/bud/bud.prev"},
	}
	for _, tt := range tests {
		if tt.got != filepath.FromSlash(tt.want) {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestResolveXDGOverrides(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	t.Setenv("XDG_CACHE_HOME", "/xdg/cache")

	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}

	if p.Config != "/xdg/config/bud" || p.Data != "/xdg/data/bud" || p.State != "/xdg/state/bud" || p.Cache != "/xdg/cache/bud" {
		t.Fatalf("got %+v", p)
	}
	// ~/.local/bin isn't an XDG base dir, so the overrides leave it alone
	if p.BinDir != "/home/me/.local/bin" {
		t.Fatalf("bin dir = %q", p.BinDir)
	}
}

func TestResolveIgnoresRelativeXDG(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Config != "/home/me/.config/bud" {
		t.Fatalf("config = %q, want the default", p.Config)
	}
}

func TestReviewPathsFailClosedWithoutRuntimeDir(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_RUNTIME_DIR", "")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.ReviewSocket() != "" {
		t.Errorf("socket = %q, want empty without XDG_RUNTIME_DIR", p.ReviewSocket())
	}
	if want := filepath.FromSlash("/home/me/.local/state/bud/approvals.json"); p.ApprovalStore() != want {
		t.Errorf("approvals = %q, want %q", p.ApprovalStore(), want)
	}

	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	p, _ = Resolve()
	if want := filepath.FromSlash("/run/user/1000/bud/review.sock"); p.ReviewSocket() != want {
		t.Errorf("socket = %q, want %q", p.ReviewSocket(), want)
	}
	if want := filepath.FromSlash("/home/me/.local/state/bud/review.secret"); p.ReviewSecret() != want {
		t.Errorf("secret = %q, want %q", p.ReviewSecret(), want)
	}
}
