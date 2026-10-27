package lang

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// needNvim skips tests that run Neovim when it isn't installed (CI's go job);
// the smoke job, which has nvim, runs them
func needNvim(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not on PATH")
	}
}

func writeSpec(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".lua"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadShipped(t *testing.T) {
	needNvim(t)
	set, err := Load(true)
	if err != nil {
		t.Fatal(err)
	}

	// D10, and the minimal preset is what the specs mark (D30)
	want := []string{"bash", "c", "css", "go", "html", "python", "typescript"}
	if got := set.Names(); !slices.Equal(got, want) {
		t.Errorf("shipped specs = %v, want %v", got, want)
	}
	if len(set.Errors) > 0 {
		t.Errorf("shipped specs failed: %v", set.Errors)
	}
	if got := set.Preset("minimal"); !slices.Equal(got, []string{"bash", "python"}) {
		t.Errorf("minimal preset = %v", got)
	}

	goSpec, ok := set.Get("go")
	if !ok || !slices.Contains(goSpec.Mason, "gopls") || !slices.Equal(goSpec.System, []string{"go"}) {
		t.Errorf("go = %+v", goSpec)
	}
}

func TestLoadDirs(t *testing.T) {
	needNvim(t)
	user := t.TempDir()
	writeSpec(t, user, "zig", `return { filetypes = { "zig" }, system = { "zig" }, minimal = true }`)
	// a user spec named like a shipped one loses to the shipped one
	writeSpec(t, user, "go", `return { filetypes = { "nope" } }`)
	writeSpec(t, user, "broken", `return { filetypes = { "x" }, formatter = {} }`)
	writeSpec(t, user, "syntax", `return {`)
	writeSpec(t, user, "_template", `return {`)

	set, err := Load(true, user, filepath.Join(user, "missing"))
	if err != nil {
		t.Fatal(err)
	}

	zig, ok := set.Get("zig")
	if !ok || !zig.Minimal || !slices.Equal(zig.System, []string{"zig"}) {
		t.Errorf("zig = %+v, %v", zig, ok)
	}
	if goSpec, _ := set.Get("go"); slices.Contains(goSpec.Filetypes, "nope") {
		t.Error("the user's go.lua won over the shipped one")
	}
	if !strings.Contains(set.Errors["broken"], `unknown field "formatter"`) {
		t.Errorf("broken: %q", set.Errors["broken"])
	}
	if set.Errors["syntax"] == "" {
		t.Error("a syntax error should be reported")
	}
	if _, ok := set.Errors["_template"]; ok {
		t.Error("files starting with _ aren't specs")
	}

	// without shipped, only the dirs count
	set, err = Load(false, user)
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Names(); !slices.Equal(got, []string{"go", "zig"}) {
		t.Errorf("names = %v", got)
	}
}

func TestLoadEmpty(t *testing.T) {
	needNvim(t)
	set, err := Load(false, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Specs) != 0 || len(set.Errors) != 0 {
		t.Fatalf("got %+v", set)
	}
}

func TestLoadNoNvim(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Load(true); err == nil || !strings.Contains(err.Error(), "needs Neovim") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnabled(t *testing.T) {
	set := &Set{
		Specs: []Spec{
			{Name: "bash", Minimal: true, System: []string{"bash"}},
			{Name: "go", System: []string{"go", "cc"}},
			{Name: "python", Minimal: true, System: []string{"python3", "cc"}},
		},
		Errors: map[string]string{"broken": "oops"},
	}

	tests := []struct {
		name      string
		languages []string
		profile   string
		want      []string
		unknown   []string
	}{
		{"full preset", nil, "full", []string{"bash", "go", "python"}, nil},
		{"minimal preset", nil, "minimal", []string{"bash", "python"}, nil},
		{"configured order", []string{"python", "go"}, "minimal", []string{"python", "go"}, nil},
		{"empty list", []string{}, "full", nil, nil},
		{"unknown and broken", []string{"zig", "broken", "go"}, "full", []string{"go"}, []string{"zig"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs, unknown := set.Enabled(tt.languages, tt.profile)
			var names []string
			for _, s := range specs {
				names = append(names, s.Name)
			}
			if !slices.Equal(names, tt.want) || !slices.Equal(unknown, tt.unknown) {
				t.Fatalf("got %v %v, want %v %v", names, unknown, tt.want, tt.unknown)
			}
		})
	}

	specs, _ := set.Enabled(nil, "full")
	if got := System(specs); !slices.Equal(got, []string{"bash", "go", "cc", "python3"}) {
		t.Errorf("system = %v", got)
	}
}

func TestFile(t *testing.T) {
	if got := File("go"); got != "languages/go.lua" {
		t.Fatalf("File = %q", got)
	}
}
