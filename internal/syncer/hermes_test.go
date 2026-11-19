package syncer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/profile"
)

func TestSyncedSkillsAreLockedInHermes(t *testing.T) {
	origBin, origRun := hermesBin, runHermes
	t.Cleanup(func() { hermesBin, runHermes = origBin, origRun })
	hermesBin = func() (string, error) { return "/usr/bin/hermes", nil }
	var ran []string
	runHermes = func(bin string, args ...string) ([]byte, error) {
		ran = append(ran, bin+" "+strings.Join(args, " "))
		if args[len(args)-1] == "broken" {
			return []byte("curator: no skill named 'broken'"), errors.New("exit status 1")
		}
		return nil, nil
	}
	if notes := lockSkills("", []string{"a", "b"}, func(string, ...any) {}); notes != nil {
		t.Errorf("notes: %q", notes)
	}
	if notes := lockSkills("cloud", []string{"a"}, func(string, ...any) {}); notes != nil {
		t.Errorf("notes: %q", notes)
	}
	if want := []string{"/usr/bin/hermes curator lock a", "/usr/bin/hermes curator lock b", "/usr/bin/hermes -p cloud curator lock a"}; !slices.Equal(ran, want) {
		t.Errorf("ran %q, want %q", ran, want)
	}

	notes := lockSkills("cloud", []string{"broken"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "no skill named 'broken'") || !strings.Contains(notes[0], "hermes -p cloud curator lock") {
		t.Errorf("a failed lock is named, with the profile: %q", notes)
	}
	notes = lockSkills("", []string{"broken"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "no skill named 'broken'") {
		t.Errorf("a failed lock is named: %q", notes)
	}

	hermesBin = func() (string, error) { return "", errors.New("not found") }
	notes = lockSkills("", []string{"a"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "Hermes isn't on PATH") {
		t.Errorf("without Hermes: %q", notes)
	}
	if lockSkills("", nil, nil) != nil {
		t.Error("no skills, no notes")
	}
}

func TestSkillFilesCopyASkillsRegularFilesForHermes(t *testing.T) {
	src := t.TempDir()
	write := func(rel string, perm os.FileMode) {
		path := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(rel), perm); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", 0o644)
	write("private.md", 0o600)
	write("scripts/run.sh", 0o755)
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	got, err := skillFiles(src, "/h/.hermes/skills/demo", "public")
	if err != nil {
		t.Fatal(err)
	}
	want := []want{
		{"/h/.hermes/skills/demo/SKILL.md", []byte("SKILL.md"), 0o644, "hermes-skill", "public"},
		{"/h/.hermes/skills/demo/private.md", []byte("private.md"), 0o600, "hermes-skill", "public"},
		{"/h/.hermes/skills/demo/scripts/run.sh", []byte("scripts/run.sh"), 0o755, "hermes-skill", "public"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d (the link isn't copied): %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.path != w.path || string(g.data) != string(w.data) || g.perm != w.perm || g.kind != w.kind || g.layer != w.layer {
			t.Errorf("file %d: got %+v, want %+v", i, g, w)
		}
	}

	if _, err := skillFiles(filepath.Join(src, "missing"), "/h/x", "public"); err == nil {
		t.Error("a missing skill dir is an error")
	}
}

// Suite D48: on a server, each synced skill is copied into Hermes's own
// skills dir under the ledger and locked there, and a skill the profile
// drops is removed from it
func TestAServerSyncCopiesItsSkillsIntoHermesAndLocksThem(t *testing.T) {
	origBin, origRun := hermesBin, runHermes
	t.Cleanup(func() { hermesBin, runHermes = origBin, origRun })
	hermesBin = func() (string, error) { return "/usr/bin/hermes", nil }
	var locked []string
	runHermes = func(bin string, args ...string) ([]byte, error) {
		locked = append(locked, strings.Join(args[:len(args)-3], " ")+":"+args[len(args)-1])
		return nil, nil
	}

	s := newServer(t)
	content := tree(t)
	content["env/server.jsonc"] = &fstest.MapFile{Data: repoFile(t, "server.jsonc")}
	s.publish("2026.11.17.1", "edge", content)
	o := options(t, s)
	o.Device = "home"
	examples := o.Profiles
	skills := `["learn", "plan"]`
	o.Profiles = func() []profile.Dir {
		return append(examples(), profile.Dir{Name: "home", FS: fstest.MapFS{
			"home.json": {Data: []byte(`{"name": "home", "components": ["server"], "channel": "edge", "content": {"skills": ` + skills + `, "env": ["base", "server"]}}`)},
		}})
	}
	run(t, o)

	dir := o.Paths.HermesSkills()
	for _, d := range []string{dir, filepath.Join(o.Paths.HermesCloud(), "skills")} {
		for _, id := range []string{"learn", "plan"} {
			if got := read(t, filepath.Join(d, id, "SKILL.md")); !strings.Contains(got, "name: "+id) {
				t.Errorf("Hermes's copy of %s in %s = %q", id, d, got)
			}
		}
	}
	if !slices.Equal(locked, []string{":learn", ":plan", "-p cloud:learn", "-p cloud:plan"}) {
		t.Errorf("locked %q, want both skills in both profiles", locked)
	}
	if cfg := read(t, o.Paths.HermesConfig()); !strings.Contains(cfg, "external_dirs: []") {
		t.Errorf("config.yaml should set no external skills dir:\n%s", cfg)
	}
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	tracked := false
	for _, f := range led.Files {
		if f.Path == filepath.Join(dir, "learn", "SKILL.md") {
			tracked = true
		}
	}
	if !tracked {
		t.Error("the ledger doesn't track Hermes's copy of learn")
	}

	skills = `["learn"]`
	locked = nil
	run(t, o)
	if _, err := os.Stat(filepath.Join(dir, "plan", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("a dropped skill's copy should be removed; stat: %v", err)
	}
	if !slices.Equal(locked, []string{":learn", "-p cloud:learn"}) {
		t.Errorf("locked %q, want learn", locked)
	}

	// adding it back finds no leftover dir in the way
	skills = `["learn", "plan"]`
	rep := run(t, o)
	if got := read(t, filepath.Join(dir, "plan", "SKILL.md")); !strings.Contains(got, "name: plan") {
		t.Errorf("plan wasn't copied back: %q (notes %q)", got, rep.Notes)
	}
}

// A skill bud didn't write, or one whose path goes through a link, gets no
// files and no lock, and the notes say why
func TestHermesSkillLeavesAloneWhatIsntBuds(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"SKILL.md", "scripts/run.sh"} {
		if err := os.WriteFile(filepath.Join(src, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	ws, note, err := hermesSkill(root, "demo", src, "public", nil)
	if err != nil || note != "" || len(ws) != 2 {
		t.Fatalf("a new skill: %d files, note %q, err %v", len(ws), note, err)
	}

	// an empty dir is what's left of a skill bud removed
	if err := os.MkdirAll(filepath.Join(root, "demo", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesSkill(root, "demo", src, "public", nil); note != "" || len(ws) != 2 {
		t.Errorf("an empty leftover dir: %d files, note %q", len(ws), note)
	}

	// Hermes's own skill of that name
	if err := os.WriteFile(filepath.Join(root, "demo", "SKILL.md"), []byte("Hermes's"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, note, err = hermesSkill(root, "demo", src, "public", nil)
	if err != nil || ws != nil || !strings.Contains(note, "didn't write") {
		t.Errorf("an untracked skill dir: %d files, note %q, err %v", len(ws), note, err)
	}
	tracked := []File{{Path: filepath.Join(root, "demo", "SKILL.md")}}
	if ws, note, _ = hermesSkill(root, "demo", src, "public", tracked); note != "" || len(ws) != 2 {
		t.Errorf("bud's own skill dir: %d files, note %q", len(ws), note)
	}

	// a link anywhere on the way, even in bud's own skill
	if err := os.Remove(filepath.Join(root, "demo", "scripts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "demo", "scripts")); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesSkill(root, "demo", src, "public", tracked); ws != nil || !strings.Contains(note, "is a link") {
		t.Errorf("a linked subdir: %d files, note %q", len(ws), note)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "other")); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesSkill(root, "other", src, "public", nil); ws != nil || !strings.Contains(note, "is a link") {
		t.Errorf("a linked skill dir: %d files, note %q", len(ws), note)
	}
}

// Suite D95: the cloud profile is rendered beside the private one, and a
// private .env holding a cloud provider's key stops the sync
func TestAServerSyncRendersBothHermesProfilesAndRefusesACloudKey(t *testing.T) {
	s := newServer(t)
	content := tree(t)
	content["env/server.jsonc"] = &fstest.MapFile{Data: repoFile(t, "server.jsonc")}
	content["env/registry.jsonc"] = &fstest.MapFile{Data: repoFile(t, "registry.jsonc")}
	s.publish("2026.11.17.1", "edge", content)
	o := options(t, s)
	o.Device = "home"
	examples := o.Profiles
	o.Profiles = func() []profile.Dir {
		return append(examples(), profile.Dir{Name: "home", FS: fstest.MapFS{
			"home.json": {Data: []byte(`{"name": "home", "components": ["server"], "channel": "edge", "content": {"env": ["base", "server"]}}`)},
		}})
	}
	if err := os.MkdirAll(filepath.Dir(o.Paths.HermesEnv()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.Paths.HermesEnv(), []byte("OP_SERVICE_ACCOUNT_TOKEN=x\nexport OPENCODE_GO_API_KEY=sk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), o); err == nil || !strings.Contains(err.Error(), "OPENCODE_GO_API_KEY") || !strings.Contains(err.Error(), "profiles/cloud/.env") {
		t.Fatalf("a private .env with the Go key: got %v", err)
	}
	if _, err := os.Stat(o.Paths.HermesConfig()); !os.IsNotExist(err) {
		t.Errorf("nothing should be written when the key is found; stat: %v", err)
	}

	// a byte order mark before the first key doesn't hide it
	if err := os.WriteFile(o.Paths.HermesEnv(), []byte("\ufeffOPENROUTER_API_KEY=sk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), o); err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("a BOM-prefixed built-in provider key: got %v", err)
	}

	if err := os.WriteFile(o.Paths.HermesEnv(), []byte("OP_SERVICE_ACCOUNT_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, o)
	private := read(t, o.Paths.HermesConfig())
	cloud := read(t, filepath.Join(o.Paths.HermesCloud(), "config.yaml"))
	if strings.Contains(private, "opencode.ai") || strings.Contains(private, "OPENCODE_GO_API_KEY") {
		t.Errorf("the private profile reaches the Go provider:\n%s", private)
	}
	if !strings.Contains(cloud, "opencode.ai") || !strings.Contains(cloud, "standalone: true") {
		t.Errorf("the cloud profile should list Go and stay standalone:\n%s", cloud)
	}
	// Hermes reads signal-cli's URL from the private .env, which keeps
	// Jason's own lines; the cloud profile's .env isn't touched
	if got := read(t, o.Paths.HermesEnv()); got != "OP_SERVICE_ACCOUNT_TOKEN=x\nSIGNAL_HTTP_URL=http://127.0.0.1:8793\n" {
		t.Errorf("the private .env: got %q", got)
	}
	if _, err := os.Stat(filepath.Join(o.Paths.HermesCloud(), ".env")); !os.IsNotExist(err) {
		t.Errorf("the cloud profile's .env should be left alone; stat: %v", err)
	}
	// set once, it's left alone
	rep := run(t, o)
	for _, c := range rep.Changes {
		if c.Path == o.Paths.HermesEnv() {
			t.Errorf("a second sync changes the .env again: %+v", c)
		}
	}
	// Signal and bud-signal are the private profile's alone
	if strings.Contains(cloud, "platforms:") || strings.Contains(cloud, "bud-signal") || !strings.Contains(private, "bud-signal") {
		t.Errorf("Signal belongs to the private profile only:\n%s", cloud)
	}
}

func TestTheSignalPluginIsInstalledForHermesWhenItsDirIsBuds(t *testing.T) {
	plugin := fstest.MapFS{
		"plugin.yaml": {Data: []byte("name: bud-signal\n")},
		"__init__.py": {Data: []byte("def register(ctx): pass\n")},
	}
	root := filepath.Join(t.TempDir(), "plugins")
	dst := filepath.Join(root, HermesPluginName)

	ws, note, err := hermesPlugin(root, plugin, nil)
	if err != nil || note != "" || len(ws) != 2 {
		t.Fatalf("a new plugin: %d files, note %q, err %v", len(ws), note, err)
	}
	for _, w := range ws {
		if filepath.Dir(w.path) != dst || w.perm != 0o644 || w.kind != "hermes-plugin" || w.layer != "binary" {
			t.Errorf("file %+v", w)
		}
	}
	if got := pluginNames(note); !slices.Equal(got, []string{"bud-signal"}) {
		t.Errorf("enabled %q", got)
	}

	// a plugin dir bud didn't write is left alone, and not enabled
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "plugin.yaml"), []byte("Jason's"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, note, err = hermesPlugin(root, plugin, nil)
	if err != nil || ws != nil || !strings.Contains(note, "didn't write") || !strings.Contains(note, "/approve") {
		t.Errorf("an untracked plugin dir: %d files, note %q, err %v", len(ws), note, err)
	}
	if pluginNames(note) != nil {
		t.Error("a plugin bud couldn't install is enabled")
	}
	tracked := []File{{Path: filepath.Join(dst, "plugin.yaml"), SHA256: hash([]byte("Jason's"))}}
	if ws, note, _ = hermesPlugin(root, plugin, tracked); note != "" || len(ws) != 2 {
		t.Errorf("bud's own plugin dir: %d files, note %q", len(ws), note)
	}

	// a file of bud's edited by hand, or swapped for a link, holds the whole plugin back
	if err := os.WriteFile(filepath.Join(dst, "plugin.yaml"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesPlugin(root, plugin, tracked); ws != nil || !strings.Contains(note, "changed by hand") || pluginNames(note) != nil {
		t.Errorf("an edited plugin file: %d files, note %q", len(ws), note)
	}
	if err := os.Remove(filepath.Join(dst, "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "x"), filepath.Join(dst, "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesPlugin(root, plugin, tracked); ws != nil || !strings.Contains(note, "isn't a plain file") {
		t.Errorf("a linked plugin file: %d files, note %q", len(ws), note)
	}

	// through a link
	linked := filepath.Join(t.TempDir(), "plugins")
	if err := os.Symlink(t.TempDir(), linked); err != nil {
		t.Fatal(err)
	}
	if ws, note, _ = hermesPlugin(linked, plugin, nil); ws != nil || !strings.Contains(note, "is a link") {
		t.Errorf("a linked plugins dir: %d files, note %q", len(ws), note)
	}
}

func TestTheShippedSignalPluginIsInTheBinary(t *testing.T) {
	for _, f := range []string{"plugin.yaml", "__init__.py", "adapter.py", "approvals.py"} {
		if _, err := fs.Stat(hermesPluginFS(), f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	err := fs.WalkDir(hermesPluginFS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.Contains(path, "__pycache__") {
			t.Errorf("%s is embedded", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAHeldDirsTrackedFilesAreNeitherPlannedNorDropped(t *testing.T) {
	files := []File{{Path: "/h/plugins/bud-signal/a.py"}, {Path: "/h/plugins/bud-signal-x/b.py"}, {Path: "/h/config.yaml"}}
	plan, kept := holdBack(files, []string{"/h/plugins/bud-signal"})
	if len(kept) != 1 || kept[0].Path != "/h/plugins/bud-signal/a.py" || len(plan) != 2 {
		t.Errorf("plan %v, kept %v", plan, kept)
	}
}

func TestSignalEnvSetsOnlyTheURL(t *testing.T) {
	const url = "http://127.0.0.1:8793"
	for _, tc := range []struct {
		name, have, want string
		changed          bool
	}{
		{"missing", "", "SIGNAL_HTTP_URL=" + url + "\n", true},
		{"appended", "SIGNAL_ACCOUNT=+1\n# note\nOTHER=x", "SIGNAL_ACCOUNT=+1\n# note\nOTHER=x\nSIGNAL_HTTP_URL=" + url + "\n", true},
		{"replaced", "A=1\nexport SIGNAL_HTTP_URL=http://old:1\r\nB=2\n", "A=1\nexport SIGNAL_HTTP_URL=" + url + "\r\nB=2\n", true},
		{"appended with CRLF", "A=1\r\nB=2", "A=1\r\nB=2\r\nSIGNAL_HTTP_URL=" + url + "\r\n", true},
		{"behind a BOM", "\ufeffSIGNAL_HTTP_URL=x\n", "\ufeffSIGNAL_HTTP_URL=" + url + "\n", true},
		{"already set", "SIGNAL_HTTP_URL=\"" + url + "\"\nA=1\n", "SIGNAL_HTTP_URL=\"" + url + "\"\nA=1\n", false},
		{"a longer name", "SIGNAL_HTTP_URL_OLD=x\n", "SIGNAL_HTTP_URL_OLD=x\nSIGNAL_HTTP_URL=" + url + "\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if tc.have != "" {
				if err := os.WriteFile(path, []byte(tc.have), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			got, perm, changed, err := signalEnv(path, url)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want || changed != tc.changed {
				t.Errorf("got %q (changed %v), want %q (changed %v)", got, changed, tc.want, tc.changed)
			}
			wantPerm := os.FileMode(0o640)
			if tc.have == "" {
				wantPerm = 0o600
			}
			if perm != wantPerm {
				t.Errorf("perm %o, want %o", perm, wantPerm)
			}
		})
	}
}

func TestHermesConfigKeepsRuntimeStateAcrossSync(t *testing.T) {
	const onboarding = "onboarding:\n  seen:\n    busy_input_prompt: true\n"
	const choice = "  service_install_choice: decline\n"
	for _, tc := range []struct {
		name, gateway, gatewayWithState string
	}{
		{"private", "", "gateway:\n" + choice},
		{"cloud", "gateway:\n  standalone: true\n", "gateway:\n  standalone: true\n" + choice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := "# config\n" + tc.gateway + "web:\n  keyless_fallback: false\n"
			runtimeConfig := "# config\n" + tc.gatewayWithState + "web:\n  keyless_fallback: false\n" + onboarding
			if err := os.WriteFile(path, []byte(runtimeConfig), 0o600); err != nil {
				t.Fatal(err)
			}
			// An existing ledger hashes the exact bytes Bud originally wrote.
			ledger := Ledger{Files: []File{{Path: path, SHA256: hash([]byte(original)), Kind: "config", Layer: "rendered"}}}
			assertClean := func() {
				t.Helper()
				if drift, err := ledger.Drift(); err != nil || len(drift) != 0 {
					t.Fatalf("Drift() = %+v, %v", drift, err)
				}
			}
			assertClean()
			plan := func(rendered string) ([]fileStep, []Change) {
				t.Helper()
				steps, _, skips, err := planFiles(ledger.Files, []want{{path, []byte(rendered), 0o600, "config", "rendered"}})
				if err != nil {
					t.Fatal(err)
				}
				return steps, skips
			}
			if steps, skips := plan(original); len(steps) != 0 || len(skips) != 0 {
				t.Fatalf("unchanged render planned steps %v, skips %v", steps, skips)
			}
			updated := original + "updates:\n  check: false\n"
			steps, skips := plan(updated)
			if len(steps) != 1 || steps[0].Action != Write || len(skips) != 0 {
				t.Fatalf("changed render planned steps %v, skips %v", steps, skips)
			}
			if err := steps[0].apply(); err != nil {
				t.Fatal(err)
			}
			ledger.Files[0] = steps[0].entry
			got := read(t, path)
			if !strings.Contains(got, onboarding) || !strings.Contains(got, choice) {
				t.Fatalf("Hermes state missing after rewrite:\n%s", got)
			}
			assertClean()
			if steps, skips := plan(updated); len(steps) != 0 || len(skips) != 0 {
				t.Fatalf("repeat sync planned steps %v, skips %v", steps, skips)
			}
			edited := strings.Replace(got, "keyless_fallback: false", "keyless_fallback: true", 1)
			if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}
			if drift, err := ledger.Drift(); err != nil || len(drift) != 1 || drift[0].Path != path {
				t.Fatalf("Drift() with manual edit = %+v, %v", drift, err)
			}
			if steps, skips := plan(updated); len(steps) != 0 || len(skips) != 1 {
				t.Fatalf("manually edited render planned steps %v, skips %v", steps, skips)
			}
		})
	}
}

func TestContactsRenderOnlyIntoPrivateHermes(t *testing.T) {
	s := newServer(t)
	content := tree(t)
	content["env/server.jsonc"] = &fstest.MapFile{Data: repoFile(t, "server.jsonc")}
	content["env/contacts.jsonc"] = &fstest.MapFile{Data: []byte(`{
		"mcp": {"servers": {"contacts": {"enabled": true}}},
		"bud": {
			"mcp": {"contacts": {
				"hermes_only": true, "url": "http://127.0.0.1:8794/mcp", "privacy": ["P0"],
				"headers": {"Authorization": "Bearer {env:BUD_CONTACTS_MCP_TOKEN}"}
			}},
			"hermes": {"mcp_servers": {"contacts": {"trust": "untrusted"}}}
		}
	}`)}
	s.publish("2026.11.17.1", "edge", content)
	o := options(t, s)
	o.Device = "home"
	examples := o.Profiles
	o.Profiles = func() []profile.Dir {
		return append(examples(), profile.Dir{Name: "home", FS: fstest.MapFS{
			"home.json": {Data: []byte(`{"name": "home", "components": ["server"], "channel": "edge", "content": {"env": ["base", "server", "contacts"]}}`)},
		}})
	}
	run(t, o)
	private := read(t, o.Paths.HermesConfig())
	if !strings.Contains(private, "  contacts:\n") || !strings.Contains(private, "Bearer ${BUD_CONTACTS_MCP_TOKEN}") {
		t.Errorf("private config is missing the contacts server or token reference:\n%s", private)
	}
	for _, path := range []string{
		filepath.Join(o.Paths.HermesCloud(), "config.yaml"),
		filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"),
	} {
		if output := read(t, path); strings.Contains(output, "contacts") || strings.Contains(output, "BUD_CONTACTS_MCP_TOKEN") {
			t.Errorf("contacts reached %s:\n%s", path, output)
		}
	}
	if tiers := read(t, o.Paths.HermesTiers()); !strings.Contains(tiers, "mcp__contacts__*") {
		t.Errorf("the tier gate must deny contacts to other profiles:\n%s", tiers)
	}
}
