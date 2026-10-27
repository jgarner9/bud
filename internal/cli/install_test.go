package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/lang"
)

// allLanguages stands in for the shipped specs' names
var allLanguages = []string{"bash", "c", "css", "go", "html", "python", "typescript"}

// testSpecs is a spec set like the shipped one, with bash and python minimal
func testSpecs() *lang.Set {
	set := &lang.Set{Errors: map[string]string{}}
	for _, name := range allLanguages {
		set.Specs = append(set.Specs, lang.Spec{Name: name, Minimal: name == "bash" || name == "python"})
	}
	return set
}

// answers is a prompter fed the given lines, with its questions discarded
func answers(lines ...string) *prompter {
	return newPrompter(strings.NewReader(strings.Join(lines, "\n")), io.Discard)
}

func withTerminals(t *testing.T, names ...string) {
	t.Helper()
	orig := detectTerminals
	detectTerminals = func() []string { return names }
	t.Cleanup(func() { detectTerminals = orig })
}

// loadConfig writes data (when not empty) to a fresh config.json and loads it
func loadConfig(t *testing.T, data string) (*config.File, config.Settings) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if data != "" {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.Settings()
	if err != nil {
		t.Fatal(err)
	}

	return f, s
}

func TestPrompterAsk(t *testing.T) {
	pr := answers("  hello  ", "")
	if got, _ := pr.ask("q", "def"); got != "hello" {
		t.Errorf("answer = %q", got)
	}
	if got, _ := pr.ask("q", "def"); got != "def" {
		t.Errorf("empty answer = %q, want the default", got)
	}
	if got, _ := pr.ask("q", "def"); got != "def" {
		t.Errorf("end of input = %q, want the default", got)
	}
}

func TestPrompterChoose(t *testing.T) {
	if got, err := answers("nope", "ghostty").choose("Terminal", config.Terminals, "kitty"); err != nil || got != "ghostty" {
		t.Errorf("retry: got %q, %v", got, err)
	}
	if _, err := answers("a", "b", "c").choose("Terminal", config.Terminals, "kitty"); err == nil {
		t.Error("expected an error after three bad answers")
	}
}

func TestPrompterChooseMany(t *testing.T) {
	got, err := answers("python, go go").chooseMany("Languages", allLanguages, nil)
	if err != nil || !slices.Equal(got, []string{"go", "python"}) {
		t.Errorf("got %q, %v; want the options' order without repeats", got, err)
	}
	got, err = answers("rust", "").chooseMany("Languages", allLanguages, []string{"bash"})
	if err != nil || !slices.Equal(got, []string{"bash"}) {
		t.Errorf("retry then default: got %q, %v", got, err)
	}

	for _, in := range []string{"all", "ALL", "go, all"} {
		got, err = answers(in).chooseMany("Languages", allLanguages, []string{"bash"})
		if err != nil || !slices.Equal(got, allLanguages) {
			t.Errorf("%q: got %q, %v; want every language", in, got, err)
		}
	}
	// the result is a copy, so changing it can't change allLanguages
	got[0] = "x"
	if allLanguages[0] == "x" {
		t.Error("chooseMany returned allLanguages itself")
	}
}

func TestPrompterChooseManyShowsAllAsDefault(t *testing.T) {
	var out strings.Builder
	pr := newPrompter(strings.NewReader("\n"), &out)
	got, err := pr.chooseMany("Languages", allLanguages, allLanguages)
	if err != nil || !slices.Equal(got, allLanguages) {
		t.Fatalf("empty answer: got %q, %v", got, err)
	}
	if want := "or all) [all]: "; !strings.HasSuffix(out.String(), want) {
		t.Fatalf("prompt = %q, want it to end with %q", out.String(), want)
	}

	out.Reset()
	pr = newPrompter(strings.NewReader("\n"), &out)
	if _, err := pr.chooseMany("Languages", allLanguages, []string{"python", "bash"}); err != nil {
		t.Fatal(err)
	}
	if want := "[python bash]: "; !strings.HasSuffix(out.String(), want) {
		t.Fatalf("prompt = %q, want it to end with %q", out.String(), want)
	}
}

func TestPrompterConfirm(t *testing.T) {
	for in, want := range map[string]bool{"y": true, "YES": true, "": false, "n": false, "sure": false} {
		if got, _ := answers(in).confirm("Continue?"); got != want {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}

func TestAskSetupDefaults(t *testing.T) {
	t.Run("fresh machine takes detected terminal and the profile's preset", func(t *testing.T) {
		withTerminals(t, "ghostty")
		f, s := loadConfig(t, "")
		s, err := askSetup(nil, f, s, "minimal")
		if err != nil {
			t.Fatal(err)
		}
		if s, err = chooseLanguages(nil, testSpecs(), s); err != nil {
			t.Fatal(err)
		}
		if s.Profile != "minimal" || s.Terminal != "ghostty" || !slices.Equal(s.Languages, []string{"bash", "python"}) {
			t.Fatalf("got %+v", s)
		}
	})

	t.Run("configured values win over detection and presets", func(t *testing.T) {
		withTerminals(t, "ghostty")
		f, s := loadConfig(t, `{"profile": "minimal", "terminal": "kitty", "languages": ["go"]}`)
		s, err := askSetup(nil, f, s, "")
		if err != nil {
			t.Fatal(err)
		}
		if s, err = chooseLanguages(nil, testSpecs(), s); err != nil {
			t.Fatal(err)
		}
		if s.Profile != "minimal" || s.Terminal != "kitty" || !slices.Equal(s.Languages, []string{"go"}) {
			t.Fatalf("got %+v", s)
		}
	})

	t.Run("nothing detected keeps kitty", func(t *testing.T) {
		withTerminals(t)
		f, s := loadConfig(t, "")
		if s, _ = askSetup(nil, f, s, ""); s.Terminal != "kitty" {
			t.Fatalf("terminal = %q", s.Terminal)
		}
	})
}

func TestAskSetupInteractive(t *testing.T) {
	withTerminals(t, "kitty", "ghostty")
	f, s := loadConfig(t, "")

	// profile, terminal, then languages
	pr := answers("minimal", "ghostty", "")
	s, err := askSetup(pr, f, s, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Profile != "minimal" || s.Terminal != "ghostty" {
		t.Fatalf("got %+v", s)
	}
	// the languages default follows the profile picked a moment earlier
	if s, err = chooseLanguages(pr, testSpecs(), s); err != nil || !slices.Equal(s.Languages, []string{"bash", "python"}) {
		t.Fatalf("languages = %v, %v", s.Languages, err)
	}

	t.Run("--profile skips the profile question", func(t *testing.T) {
		f, s := loadConfig(t, "")
		pr := answers("ghostty", "go")
		s, err := askSetup(pr, f, s, "full")
		if err != nil {
			t.Fatal(err)
		}
		if s, err = chooseLanguages(pr, testSpecs(), s); err != nil {
			t.Fatal(err)
		}
		if s.Profile != "full" || s.Terminal != "ghostty" || !slices.Equal(s.Languages, []string{"go"}) {
			t.Fatalf("got %+v", s)
		}
	})
}

func TestChooseLanguages(t *testing.T) {
	t.Run("a configured name with no spec leaves the default", func(t *testing.T) {
		_, s := loadConfig(t, `{"languages": ["zig", "go"]}`)
		out := &strings.Builder{}
		pr := newPrompter(strings.NewReader("\n"), out)
		s, err := chooseLanguages(pr, testSpecs(), s)
		if err != nil || !slices.Equal(s.Languages, []string{"go"}) {
			t.Fatalf("got %v, %v", s.Languages, err)
		}
	})

	t.Run("nobody to ask keeps a configured list as is", func(t *testing.T) {
		_, s := loadConfig(t, `{"languages": ["zig", "go"]}`)
		s, err := chooseLanguages(nil, testSpecs(), s)
		if err != nil || !slices.Equal(s.Languages, []string{"zig", "go"}) {
			t.Fatalf("got %v, %v", s.Languages, err)
		}
	})

	t.Run("an empty list stays empty", func(t *testing.T) {
		_, s := loadConfig(t, `{"languages": []}`)
		s, err := chooseLanguages(nil, testSpecs(), s)
		if err != nil || s.Languages == nil || len(s.Languages) != 0 {
			t.Fatalf("got %#v, %v", s.Languages, err)
		}
	})
}

func TestLanguageDeps(t *testing.T) {
	specs := &lang.Set{Specs: []lang.Spec{
		{Name: "go", System: []string{"go"}},
		{Name: "python", Minimal: true, System: []string{"python3"}},
	}}
	_, s := loadConfig(t, `{"profile": "minimal"}`)
	deps := languageDeps(specs, s)
	if len(deps) != 1 || deps[0].Name != "python3" {
		t.Fatalf("minimal preset deps = %+v", deps)
	}
}

func TestSaveSetup(t *testing.T) {
	preset := testSpecs().Preset("full")

	t.Run("all defaults writes nothing", func(t *testing.T) {
		f, s := loadConfig(t, "")
		if err := saveSetup(f, s, preset); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(f.Path); !os.IsNotExist(err) {
			t.Fatalf("config.json was written, stat err = %v", err)
		}
	})

	t.Run("writes changes and keeps other keys", func(t *testing.T) {
		f, s := loadConfig(t, `{"dashboard": {"style": "minimal"}}`)
		s.Profile = "minimal"
		s.Languages = []string{"bash"}
		if err := saveSetup(f, s, preset); err != nil {
			t.Fatal(err)
		}

		f, s = loadConfig(t, mustRead(t, f.Path))
		if s.Profile != "minimal" || !slices.Equal(s.Languages, []string{"bash"}) || s.Dashboard.Style != "minimal" {
			t.Fatalf("got %+v", s)
		}
		if _, set := f.Get("terminal"); set {
			t.Error("terminal kept its default, so it shouldn't be written")
		}
	})

	t.Run("the profile's preset isn't written", func(t *testing.T) {
		f, s := loadConfig(t, "")
		s.Profile = "minimal"
		s.Languages = []string{"bash", "python"}
		if err := saveSetup(f, s, testSpecs().Preset("minimal")); err != nil {
			t.Fatal(err)
		}
		if f, _ = loadConfig(t, mustRead(t, f.Path)); f != nil {
			if _, set := f.Get("languages"); set {
				t.Error("languages matched the minimal preset, so it shouldn't be written")
			}
		}
	})

	t.Run("a key the file already sets is kept even at its default", func(t *testing.T) {
		f, s := loadConfig(t, `{"terminal": "ghostty"}`)
		s.Terminal = "kitty"
		if err := saveSetup(f, s, preset); err != nil {
			t.Fatal(err)
		}
		if _, s = loadConfig(t, mustRead(t, f.Path)); s.Terminal != "kitty" {
			t.Fatalf("terminal = %q", s.Terminal)
		}
	})
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestInstallDepsWithoutConsent(t *testing.T) {
	plan := &installer.Plan{
		Missing: []installer.Dep{{Name: "ripgrep"}},
		Manager: installer.Manager{Name: "brew", Bin: "brew", Install: []string{"brew", "install"}},
		Found:   true,
	}

	// declining, or having nobody to ask without --yes, must not run anything
	for name, pr := range map[string]*prompter{"declined": answers("n"), "no terminal": nil} {
		var missing *installer.MissingError
		if err := installDeps(plan, pr, false); !errors.As(err, &missing) {
			t.Errorf("%s: err = %v, want *MissingError", name, err)
		}
	}

	if err := installDeps(&installer.Plan{}, nil, false); err != nil {
		t.Errorf("nothing missing: err = %v", err)
	}
}

func TestIsTerminal(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	if isTerminal(devnull) {
		t.Error("/dev/null is a character device, not a terminal")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminal(r) {
		t.Error("a pipe is not a terminal")
	}
}
