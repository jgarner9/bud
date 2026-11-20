package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
)

// recordComponents swaps every component's install for a recorder. A
// component named in fail returns an error instead
func recordComponents(t *testing.T, fail ...string) *[]string {
	t.Helper()
	var ran []string
	orig := installComponents
	installComponents = map[string]func(*Globals, bool) error{}
	for _, c := range profile.Components {
		installComponents[c] = func(*Globals, bool) error {
			ran = append(ran, c)
			if slices.Contains(fail, c) {
				return errors.New("broke")
			}
			return nil
		}
	}
	t.Cleanup(func() { installComponents = orig })

	return &ran
}

// deviceHome points bud's dirs at a temp dir and returns the overlay's
// profiles dir, which starts out missing
func deviceHome(t *testing.T) (overlayProfiles string, configPath string) {
	t.Helper()
	dir := t.TempDir()
	for _, env := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(env, filepath.Join(dir, env))
	}
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}

	return filepath.Join(p.Overlay(), "profiles"), p.MachineConfig()
}

func deviceSetting(t *testing.T, path string) string {
	t.Helper()
	f, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.Settings()
	if err != nil {
		t.Fatal(err)
	}

	return s.Device
}

func TestInstallWithNoDeviceIsTheIDEAlone(t *testing.T) {
	_, cfg := deviceHome(t)
	ran := recordComponents(t)
	if err := install(&Globals{}, nil, true, ""); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*ran, []string{"ide"}) {
		t.Fatalf("ran %v, want only ide", *ran)
	}
	if _, err := os.Stat(cfg); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an install with no device shouldn't write a device setting")
	}
}

func TestInstallRunsEachExampleProfilesComponents(t *testing.T) {
	for name, want := range map[string][]string{
		"dev-machine": {"ide", "harness"},
		"server":      {"harness", "server"}, // in profile.Components order, not the file's
		"model-box":   {"qwen"},
	} {
		t.Run(name, func(t *testing.T) {
			_, cfg := deviceHome(t)
			ran := recordComponents(t)
			if err := install(&Globals{}, nil, true, name); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(*ran, want) {
				t.Fatalf("ran %v, want %v", *ran, want)
			}
			if got := deviceSetting(t, cfg); got != name {
				t.Fatalf("device setting = %q, want %q", got, name)
			}
		})
	}
}

func TestInstallRemembersTheDevice(t *testing.T) {
	_, cfg := deviceHome(t)
	ran := recordComponents(t)
	if err := install(&Globals{}, nil, true, "model-box"); err != nil {
		t.Fatal(err)
	}
	*ran = nil
	if err := install(&Globals{}, nil, true, ""); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*ran, []string{"qwen"}) {
		t.Fatalf("a later install without --device ran %v, want the remembered model-box's qwen", *ran)
	}
	if err := install(&Globals{}, nil, true, "dev-machine"); err != nil {
		t.Fatal(err)
	}
	if got := deviceSetting(t, cfg); got != "dev-machine" {
		t.Fatalf("--device should replace the setting, got %q", got)
	}
}

func TestTheOverlaysProfileWins(t *testing.T) {
	overlay, _ := deviceHome(t)
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := `{"name": "dev-machine", "components": ["ide"], "channel": "stable"}`
	if err := os.WriteFile(filepath.Join(overlay, "dev-machine.json"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := recordComponents(t)
	if err := install(&Globals{}, nil, true, "dev-machine"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*ran, []string{"ide"}) {
		t.Fatalf("ran %v, want the overlay's ide-only dev-machine", *ran)
	}
}

func TestAnUnknownDeviceChangesNothing(t *testing.T) {
	_, cfg := deviceHome(t)
	ran := recordComponents(t)
	err := install(&Globals{}, nil, true, "toaster")
	if err == nil || !strings.Contains(err.Error(), `no profile called "toaster"`) || !strings.Contains(err.Error(), "dev-machine, model-box, server") {
		t.Fatalf("want the missing name and the ones there are, got %v", err)
	}
	if len(*ran) > 0 {
		t.Fatalf("ran %v for a profile that doesn't exist", *ran)
	}
	if _, err := os.Stat(cfg); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an unknown device shouldn't be saved")
	}
}

func TestAFailedComponentDoesntStopTheRest(t *testing.T) {
	deviceHome(t)
	ran := recordComponents(t, "ide")
	err := install(&Globals{}, nil, true, "dev-machine")
	if err == nil || !strings.Contains(err.Error(), "ide: broke") {
		t.Fatalf("want ide's error named, got %v", err)
	}
	if !slices.Equal(*ran, []string{"ide", "harness"}) {
		t.Fatalf("ran %v, want harness to run after ide failed", *ran)
	}
}

func TestComponentsNotBuiltSayWhichPhase(t *testing.T) {
	for c, phase := range map[string]string{"server": "phase 2", "qwen": "phase 3", "app": "phase 7"} {
		err := installComponents[c](&Globals{}, true)
		if err == nil || !strings.Contains(err.Error(), "not built yet") || !strings.Contains(err.Error(), phase) {
			t.Errorf("%s: want not built yet and %s, got %v", c, phase, err)
		}
	}
	for _, c := range profile.Components {
		if installComponents[c] == nil {
			t.Errorf("component %s has no install", c)
		}
	}
}

func TestInstallDeviceOpenCodeHarness(t *testing.T) {
	for _, tc := range []struct {
		name, installed, after, wantError string
		wantInstall                       bool
	}{
		{name: "already pinned", installed: "2.3.2"},
		{name: "different version", installed: "2.3.1", wantError: "want 2.3.2"},
		{name: "unreadable version", installed: "unknown", wantError: "reports version unknown"},
		{name: "install the pin", after: "2.3.2", wantInstall: true},
		{name: "verify after npm", after: "2.3.1", wantInstall: true, wantError: "want 2.3.2"},
		{name: "missing after npm", wantInstall: true, wantError: "isn't on PATH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overlay, _ := deviceHome(t)
			if err := os.MkdirAll(overlay, 0o755); err != nil {
				t.Fatal(err)
			}
			prof := `{"name":"coding","components":["harness"],"channel":"stable"}`
			if err := os.WriteFile(filepath.Join(overlay, "coding.json"), []byte(prof), 0o600); err != nil {
				t.Fatal(err)
			}

			bin := t.TempDir()
			t.Setenv("PATH", bin)
			writeOpenCode := func(version string) {
				if version == "" {
					return
				}
				script := "#!/bin/sh\nprintf '%s\\n' '" + version + "'\n"
				if version == "unknown" {
					script = "#!/bin/sh\nexit 1\n"
				}
				if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeOpenCode(tc.installed)

			orig := npmInstall
			t.Cleanup(func() { npmInstall = orig })
			var packages []string
			npmInstall = func(pkg string) error {
				packages = append(packages, pkg)
				writeOpenCode(tc.after)
				return nil
			}

			err := install(&Globals{}, nil, true, "coding")
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("got %v, want %q", err, tc.wantError)
			}
			var want []string
			if tc.wantInstall {
				want = []string{"@opencode/cli@2.3.2"}
			}
			if !slices.Equal(packages, want) {
				t.Fatalf("npm installed %v, want %v", packages, want)
			}
		})
	}
}
