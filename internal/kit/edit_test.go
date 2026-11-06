package kit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func repoKit(t *testing.T) Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kit")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "kit.json"), []byte(`{"plugins": [], "other": "kept"}`), 0o644)
	return Repo{Dir: dir}
}

func TestAddRemove(t *testing.T) {
	r := repoKit(t)
	if err := r.AddMarketplace("market", "https://example.com/market.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddPlugin("p@market"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []error{
		r.AddPlugin("p@market"),       // twice
		r.AddPlugin("no-marketplace"), // not name@marketplace
		r.AddPlugin("Bad@market"),
		r.AddMarketplace("market", "https://x"),
		r.AddMarketplace("other", ""),
	} {
		if bad == nil {
			t.Error("took a bad entry")
		}
	}
	k, err := Load(os.DirFS(r.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(k.Plugins, []string{"p@market"}) || k.Marketplaces["market"] != "https://example.com/market.git" {
		t.Fatalf("kit %+v", k)
	}

	for _, name := range []string{"p@market", "market"} {
		if _, err := r.Remove(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Remove("nothing"); err == nil {
		t.Fatal("removed nothing")
	}
	k, _ = Load(os.DirFS(r.Dir))
	if len(k.Plugins) != 0 || len(k.Marketplaces) != 0 {
		t.Fatalf("after rm %+v", k)
	}
	if idx, _ := r.index(); idx["other"] != "kept" {
		t.Fatalf("lost a key: %v", idx)
	}
}

func TestSyncSkipsDisabled(t *testing.T) {
	r := repoKit(t)
	r.AddPlugin("docs@market")
	k, _ := Load(os.DirFS(r.Dir))
	dir := t.TempDir()
	var ran []string
	env := Env{
		ClaudeDir: filepath.Join(dir, ".claude"), ClaudeJSON: filepath.Join(dir, ".claude.json"),
		Ledger: filepath.Join(dir, "l.json"), Disabled: []string{"docs@market"},
		Claude: func(a ...string) error { ran = append(ran, strings.Join(a, " ")); return nil },
	}
	if _, err := Sync(k, env, false); err != nil || len(ran) != 0 {
		t.Fatalf("synced a disabled entry: %q %v", ran, err)
	}

	// turned off under the marketplace the kit moved it away from: still off
	env.Disabled = []string{"docs@elsewhere"}
	if _, err := Sync(k, env, false); err != nil || len(ran) != 0 {
		t.Fatalf("synced a plugin disabled under its old id: %q %v", ran, err)
	}
}

func TestTurns(t *testing.T) {
	for _, c := range []struct {
		name, id string
		want     bool
	}{
		{"docs@market", "docs@market", true},
		{"docs@old", "docs@market", true},
		{"other@market", "docs@market", false},
		{"bud-plugins", "bud-plugins", true},
		{"docs", "docs@market", false},
	} {
		if got := Turns(c.name, c.id); got != c.want {
			t.Errorf("Turns(%q, %q) = %v, want %v", c.name, c.id, got, c.want)
		}
	}
}
