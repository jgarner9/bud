package cli

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/curator"
	models "jgarner.dev/bud/internal/registry"
)

func TestCuratorRunChecksItsInputs(t *testing.T) {
	var got *curator.Curator
	origRun, origReg := runCurator, curatorLoadRegistry
	runCurator = func(_ context.Context, c *curator.Curator) (curator.Summary, error) {
		got = c
		return curator.Summary{Read: 2}, nil
	}
	reg := &models.Registry{
		Providers: map[string]models.Provider{"qwen": {API: "openai", Endpoint: "http://qwen.invalid:8080/v1", Local: true}},
		Models:    map[string]models.Model{"q": {Provider: "qwen", Name: "qwen-35b"}},
		Roles:     map[string]models.Role{"curator": {Pool: []string{"q"}}},
	}
	curatorLoadRegistry = func(string) (*models.Registry, error) { return reg, nil }
	t.Cleanup(func() { runCurator, curatorLoadRegistry = origRun, origReg })
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	t.Setenv(curatorToken, "")
	if err := curatorRun(&Globals{}, nil, "", "", "", "P1,P2"); err == nil || !strings.Contains(err.Error(), curatorToken) {
		t.Errorf("no token: %v", err)
	}
	t.Setenv(curatorToken, "tok")
	if err := curatorRun(&Globals{}, nil, "", "", "", "P1,P3"); err == nil {
		t.Error("a bad tier was accepted")
	}
	if err := curatorRun(&Globals{}, []string{"x"}, "", "", "", "P1"); err == nil {
		t.Error("an argument was accepted")
	}
	var out, errOut bytes.Buffer
	if err := curatorRun(&Globals{stdout: &out, stderr: &errOut}, nil, "", "", "", "P1, P2"); err != nil {
		t.Fatal(err)
	}
	if got == nil || strings.Join(got.Tiers, ",") != "P1,P2" || !strings.HasSuffix(got.Repo.Dir, "bud/kb") || !strings.HasSuffix(got.State, "bud/curator.json") {
		t.Fatalf("curator = %+v", got)
	}
	if !strings.Contains(out.String(), "read 2 records") || !strings.Contains(errOut.String(), "tiers P1,P2") {
		t.Errorf("stdout %q, stderr %q", out.String(), errOut.String())
	}

	reg.Providers["qwen"] = models.Provider{API: "openai", Endpoint: "https://cloud.example/v1"}
	if err := curatorRun(&Globals{}, nil, "", "", "", "P1"); err == nil || !strings.Contains(err.Error(), "local") {
		t.Errorf("a cloud curator model: %v", err)
	}
}

func TestCuratorPromoteAndResolveArgs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, args := range [][]string{nil, {"x"}, {"0"}, {"1", "2"}} {
		if err := curatorPromote(&Globals{}, args, ""); err == nil {
			t.Errorf("promote %v was accepted", args)
		}
	}
	if err := curatorPromote(&Globals{}, []string{"12"}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "isn't a git clone") {
		t.Errorf("promote without a clone: %v", err)
	}
	kbDir := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", kbDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	var out bytes.Buffer
	if err := curatorPromote(&Globals{stdout: &out}, []string{"12"}, kbDir); err != nil || !strings.Contains(out.String(), "record 12") {
		t.Errorf("promote 12: %v %q", err, out.String())
	}
	for _, args := range [][]string{nil, {"ab12cd34"}, {"ab12cd34", "maybe"}} {
		if err := curatorResolve(&Globals{}, args, ""); err == nil {
			t.Errorf("resolve %v was accepted", args)
		}
	}
	if err := curatorRestore(&Globals{}, nil, ""); err == nil {
		t.Error("restore with nothing was accepted")
	}
}
