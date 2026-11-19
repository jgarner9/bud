package profile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var examples = filepath.Join("..", "..", "profiles")

func TestTheExampleProfilesAreValid(t *testing.T) {
	names, err := List(OS(examples))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dev-machine", "model-box", "server"}; !slices.Equal(names, want) {
		t.Fatalf("examples = %v, want %v", names, want)
	}
	for _, name := range names {
		if _, _, err := Find(name, OS(examples)); err != nil {
			t.Error(err)
		}
	}
}

func TestTheDevMachineRunsTheIDEAndOpenCodeOnEdge(t *testing.T) {
	p, _, err := Find("dev-machine", OS(examples))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Has("ide") || !p.Has("harness") || p.Has("server") || p.Channel != "edge" {
		t.Fatalf("got %+v", p)
	}
}

func TestLeftOutKindsTakeEverythingButEnvTakesBase(t *testing.T) {
	p := &Profile{Name: "x", Components: []string{"harness"}, Channel: "stable"}
	for _, kind := range []string{"skills", "agents", "commands", "plugins"} {
		if !p.Selects(kind, "anything") {
			t.Errorf("%s left out should take everything", kind)
		}
	}
	if !p.Selects("env", "base") || p.Selects("env", "server") {
		t.Error("env left out should take only base")
	}
}

func TestAnEmptyEnvListTakesNoEnv(t *testing.T) {
	p, err := Parse([]byte(`{"name": "x", "components": ["harness"], "channel": "stable", "content": {"env": []}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Selects("env", "base") {
		t.Error(`"env": [] should take nothing, not fall back to base`)
	}
}

func TestGlobsSelectByID(t *testing.T) {
	p := &Profile{Content: Content{Skills: []string{"plan-*", "learn"}, Plugins: []string{}}}
	for id, want := range map[string]bool{"plan-to-linear": true, "learn": true, "learning": false, "code-style": false} {
		if got := p.Selects("skills", id); got != want {
			t.Errorf("Selects(skills, %q) = %v, want %v", id, got, want)
		}
	}
	if p.Selects("plugins", "approvals") {
		t.Error("an empty list should take nothing")
	}
}

func TestEachProblemSaysWhatToFix(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Profile
		want string
	}{
		{"bad name", Profile{Name: "My Laptop", Components: []string{"ide"}, Channel: "edge"}, `name: "My Laptop" must be lowercase`},
		{"no components", Profile{Name: "a", Channel: "edge"}, `components: empty`},
		{"unknown component", Profile{Name: "a", Components: []string{"toaster"}, Channel: "edge"}, `components: "toaster" isn't one of ide, harness, app, server, qwen`},
		{"repeat", Profile{Name: "a", Components: []string{"ide", "ide"}, Channel: "edge"}, `components: "ide" is listed twice`},
		{"bad channel", Profile{Name: "a", Components: []string{"ide"}, Channel: "nightly"}, `channel: "nightly" isn't one of stable, edge`},
		{"bad glob", Profile{Name: "a", Components: []string{"ide"}, Channel: "edge", Content: Content{Skills: []string{"[a"}}}, `content.skills: "[a" isn't a glob`},
		{"path glob", Profile{Name: "a", Components: []string{"ide"}, Channel: "edge", Content: Content{Env: []string{"env/base"}}}, `content.env: "env/base" isn't a glob`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing\n  %s\ngot\n  %v", tc.want, err)
			}
		})
	}
}

func TestTheOverlayProfileWins(t *testing.T) {
	overlay := t.TempDir()
	os.WriteFile(filepath.Join(overlay, "dev-machine.json"), []byte(`{"name": "dev-machine", "components": ["ide"], "channel": "stable"}`), 0o644)
	p, file, err := Find("dev-machine", OS(overlay), OS(examples))
	if err != nil {
		t.Fatal(err)
	}
	if file != filepath.Join(overlay, "dev-machine.json") || p.Has("harness") {
		t.Fatalf("got %s %+v", file, p)
	}
	names, _ := List(OS(overlay), OS(examples))
	if len(names) != 3 {
		t.Fatalf("a name in both dirs is listed once: %v", names)
	}
}

func TestFindExplainsWhatIsWrong(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "laptop.json"), []byte(`{"name": "desktop", "components": ["ide"], "channel": "edge"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "typo.json"), []byte(`{"name": "typo", "compnents": ["ide"]}`), 0o644)
	for name, want := range map[string]string{
		"laptop":  `its name is "desktop"`,
		"typo":    `unknown field "compnents"`,
		"nothere": `no profile called "nothere"`,
		"../etc":  `isn't a profile name`,
	} {
		if _, _, err := Find(name, OS(dir)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Find(%q): want %q, got %v", name, want, err)
		}
	}
	if _, _, err := Find("nothere", OS(dir)); !strings.Contains(err.Error(), "there are laptop, typo") {
		t.Errorf("the error should list what there is: %v", err)
	}
}

func TestProfileFormatVersion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		valid bool
	}{
		{"legacy", `{"name":"home","components":["server"],"channel":"edge"}`, true},
		{"current", `{"version":1,"name":"home","components":["server"],"channel":"edge"}`, true},
		{"zero", `{"version":0,"name":"home","components":["server"],"channel":"edge"}`, false},
		{"future", `{"version":2,"name":"home","components":["server"],"channel":"edge"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			err = p.Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("Validate() = %v, valid = %v", err, tc.valid)
			}
		})
	}
}
