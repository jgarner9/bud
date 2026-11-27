package syncer

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	bud "jgarner.dev/bud"
	"jgarner.dev/bud/internal/profile"
)

// repoContent is the repo's content/ as a tree to publish
func repoContent(t *testing.T) fstest.MapFS {
	t.Helper()
	root := filepath.Join("..", "..", "content")
	tree := fstest.MapFS{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		tree[filepath.ToSlash(rel)] = &fstest.MapFile{Data: b}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tree
}

// Build plan task 1.11: the content this repo ships, synced with the
// dev-machine example profile, gives OpenCode its skills, agents and the
// coding fragment
func TestTheShippedContentSyncsOnADevMachine(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", repoContent(t))
	o := options(t, s)
	o.Device = "dev-machine"
	o.Profiles = func() []profile.Dir { return []profile.Dir{{Name: "bud's examples", FS: bud.Profiles()}} }

	run(t, o)

	skills, err := os.ReadDir(filepath.Join(o.Paths.Active(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range skills {
		ids = append(ids, e.Name())
		b, err := os.ReadFile(filepath.Join(o.Paths.Active(), "skills", e.Name(), "SKILL.md"))
		if err != nil {
			t.Errorf("skill %s: %v", e.Name(), err)
			continue
		}
		// OpenCode lists a skill only when it has a description
		if !strings.HasPrefix(string(b), "---\nname: "+e.Name()+"\ndescription: ") {
			t.Errorf("skill %s's front matter doesn't start with its name and a description:\n%.200s", e.Name(), b)
		}
	}
	for _, want := range []string{"bud-dev", "file-issue"} {
		if !strings.Contains(strings.Join(ids, " "), want) {
			t.Errorf("active skills %v lack %s", ids, want)
		}
	}

	if got := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "agents", "chat.md")); !strings.Contains(got, "mode: primary") || !strings.Contains(got, "action: subagent") {
		t.Errorf("agents/chat.md = %q", got)
	}
	cfg := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"))
	// the data dir here is a temp dir, not ~/.local/share/bud: the exceptions
	// for synced content follow it, and come after the coding fragment's ask
	ask := strings.Index(cfg, `"action": "external_directory",
      "effect": "ask",
      "resource": "*"`)
	allow := strings.Index(cfg, `"resource": "`+filepath.Join(o.Paths.Content(), "*")+`"`)
	if ask < 0 || allow < ask {
		t.Errorf("the synced content's external_directory allow (at %d) should follow the coding fragment's ask (at %d):\n%s", allow, ask, cfg)
	}
	if !strings.Contains(cfg, filepath.Join(o.Paths.Overlay(), "*")) {
		t.Errorf("opencode.jsonc lacks the overlay's exception:\n%s", cfg)
	}
	if strings.Contains(cfg, ".local/share/bud") {
		t.Errorf("opencode.jsonc names the default data dir:\n%s", cfg)
	}
	for _, want := range []string{
		filepath.Join(o.Paths.ContentCurrent(), "AGENTS.md"),
		`"formatter": true`,
		`"action": "external_directory"`,
		`"resource": "go test *"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("opencode.jsonc lacks %s:\n%s", want, cfg)
		}
	}
}

var update = flag.Bool("update", false, "rewrite the golden files")

// personalEnv stands in for an overlay's env/personal.jsonc: the gateway by
// tailnet IP (Suite D72, D74) and Bud memory through it with its approval
// tiers (JGA-313). Example address only
const personalEnv = `{"bud": {"gateway": "http://100.64.0.1:8790", "mcp": {"memory": {"gateway": true, "approval": {"tools": {"memory_memory_search": "allow", "memory_memory_stage": "allow"}}}}}}`

// JGA-358: each example profile, synced with the shipped content and an
// overlay that adds a personal fragment, renders the golden configs: the MCP
// servers and built-in tool settings of the tool map
func TestEachExampleProfileRendersItsGoldenConfigs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	for _, name := range []string{"dev-machine", "server", "server-planning", "model-box"} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			o := options(t, s)
			o.Device = name
			o.Profiles = func() []profile.Dir { return ProfileDirs(o.Paths) }

			// the overlay's profile is the example's, plus the personal fragment
			ex, _, err := profile.Find(name, profile.Dir{Name: "examples", FS: bud.Profiles()})
			if err != nil {
				t.Fatal(err)
			}
			s.publish("2026.11.16.1", ex.Channel, repoContent(t))
			prof := string(repoFileAt(t, "profiles", name+".json"))
			if ex.Content.Env != nil {
				prof = strings.Replace(prof, `"env": [`, `"env": ["personal", `, 1)
			}
			repo := t.TempDir()
			for path, data := range map[string]string{
				"overlay.json":               `{"format": 1}`,
				"profiles/" + name + ".json": prof,
				"env/personal.jsonc":         personalEnv,
			} {
				full := filepath.Join(repo, path)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			git(t, repo, "init", "-q")
			git(t, repo, "add", ".")
			git(t, repo, "commit", "-q", "-m", "overlay")
			o.Overlay = repo

			rep := run(t, o)
			for _, n := range rep.Notes {
				if strings.Contains(n, "MCP server") {
					t.Errorf("with the overlay's personal fragment nothing should be left out: %s", n)
				}
			}
			root := filepath.Dir(filepath.Dir(o.Paths.Config))
			for _, f := range []struct{ path, golden string }{
				{filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"), "opencode.jsonc"},
				{o.Paths.HermesConfig(), "hermes.yaml"},
				{filepath.Join(o.Paths.HermesCloud(), "config.yaml"), "hermes-cloud.yaml"},
				{o.Paths.MCPGatewayApprovals(), "mcp-gateway-approvals.json"},
			} {
				golden := filepath.Join("testdata", "profiles", name, f.golden)
				got, err := os.ReadFile(f.path)
				if os.IsNotExist(err) {
					if _, err := os.Stat(golden); err == nil {
						t.Errorf("%s wasn't rendered, but %s expects it", f.path, golden)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				// the temp dirs stand in for a home dir
				got = bytes.ReplaceAll(got, []byte(root), []byte("/home/you"))
				if *update {
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, got, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run go test ./internal/syncer -run Golden -update to create it)", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s differs from %s; check the diff and run go test ./internal/syncer -run Golden -update\ngot:\n%s", f.path, golden, got)
				}
			}
		})
	}
}

func repoFileAt(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// Suite D142 to D146: a session runs a command with a vault secret only
// through bud secrets run. OpenCode applies the last rule that matches, so
// the allow has to follow the catch-all ask, and the denies for op, the
// Keychain tool and the token file have to follow every ask and allow they
// could otherwise lose to
func TestTheShippedRulesLetSessionsRunBudSecretsRunAndNothingElseAboutSecrets(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", repoContent(t))
	o := options(t, s)
	o.Device = "dev-machine"
	o.Profiles = func() []profile.Dir { return []profile.Dir{{Name: "bud's examples", FS: bud.Profiles()}} }
	run(t, o)
	cfg := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"))

	rule := func(action, effect, resource string) int {
		t.Helper()
		i := strings.Index(cfg, `"action": "`+action+`",
      "effect": "`+effect+`",
      "resource": "`+resource+`"`)
		if i < 0 {
			t.Fatalf("opencode.jsonc has no %s rule that %ss %q", action, effect, resource)
		}

		return i
	}
	catchAll := rule("shell", "ask", "*")
	if allow := rule("shell", "allow", "bud secrets run *"); allow < catchAll {
		t.Errorf("bud secrets run's allow (at %d) comes before the catch-all ask (at %d), which would win", allow, catchAll)
	}
	for _, res := range []string{"op", "op *", "security", "security *", "*/security", "*/security *", "*op-service-account*"} {
		if deny := rule("shell", "deny", res); deny < catchAll {
			t.Errorf("the deny for %q (at %d) comes before the catch-all ask (at %d)", res, deny, catchAll)
		}
	}
	if deny, ask := rule("read", "deny", "*op-service-account*"), rule("read", "ask", "*.env"); deny < ask {
		t.Errorf("the token file's read deny (at %d) comes before the *.env ask (at %d), which would win", deny, ask)
	}
	if strings.Contains(cfg, `"resource": "bud secrets *"`) || strings.Contains(cfg, `"resource": "bud *"`) {
		t.Error("a rule wider than bud secrets run * would let a session run other bud commands alone")
	}
}
