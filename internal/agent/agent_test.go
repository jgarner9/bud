package agent

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeTools swaps the lookups: names on the fake PATH and a fixed version
func fakeTools(t *testing.T, installed []string, version string) {
	t.Helper()
	origLook, origVersion := lookPath, versionOf
	t.Cleanup(func() { lookPath, versionOf = origLook, origVersion })
	lookPath = func(name string) (string, error) {
		for _, n := range installed {
			if n == name {
				return "/fake/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	versionOf = func(bin string) string {
		if version != "" {
			return version
		}
		return filepath.Base(bin) + " 1.2.3"
	}
}

func TestArgvAndTUI(t *testing.T) {
	if got := Argv(); !reflect.DeepEqual(got, []string{"opencode", "acp"}) {
		t.Errorf("Argv = %q", got)
	}
	if got := TUI("/p"); !reflect.DeepEqual(got, []string{"opencode", "/p"}) {
		t.Errorf("TUI = %q", got)
	}
	if got := ChatTUI("/p", "chat"); !reflect.DeepEqual(got, []string{"opencode", "--agent=chat", "/p"}) {
		t.Errorf("ChatTUI = %q", got)
	}
}

func TestCheckNotInstalled(t *testing.T) {
	fakeTools(t, nil, "")
	st := Check(false)
	if st.Ready() || st.Bin != "" {
		t.Fatalf("ready with nothing installed: %+v", st)
	}
	if p := st.Problems[0]; !strings.Contains(p.What, "opencode isn't installed") || p.Fix != "npm install -g @opencode/cli@2.3.2" || !p.Hard {
		t.Errorf("problem: %+v", p)
	}
}

func TestCheckInstalled(t *testing.T) {
	fakeTools(t, []string{"opencode"}, "")
	st := Check(false)
	if !st.Ready() || len(st.Problems) != 0 || st.Bin != "/fake/bin/opencode" || st.Version != "opencode 1.2.3" {
		t.Errorf("installed: %+v", st)
	}
	if !reflect.DeepEqual(st.Argv, []string{"opencode", "acp"}) {
		t.Errorf("argv %q", st.Argv)
	}
	// quick doesn't run it for its version
	if st := Check(true); st.Version != "" || st.Bin == "" {
		t.Errorf("quick: %+v", st)
	}
}

func TestCheckOpenCodeHarness(t *testing.T) {
	for _, c := range []struct {
		name, version string
		ready         bool
		want          string
	}{
		{"pinned", "opencode v2.3.2", true, ""},
		{"bare pinned", "2.3.2", true, ""},
		{"other", "2.2.7", false, "reports version 2.2.7, want 2.3.2"},
		{"unrecognised", "", false, "reports version opencode 1.2.3"},
	} {
		fakeTools(t, []string{"opencode"}, c.version)
		st := CheckOpenCodeHarness()
		if st.Ready() != c.ready {
			t.Errorf("%s: ready %v, %+v", c.name, st.Ready(), st)
		}
		if c.want != "" && (len(st.Problems) == 0 || !strings.Contains(st.Problems[0].What, c.want)) {
			t.Errorf("%s: problems %+v, want %q", c.name, st.Problems, c.want)
		}
	}
}

func TestOpenCodeSetupUsesPinnedVersion(t *testing.T) {
	if InstallCommand != "npm install -g @opencode/cli@2.3.2" {
		t.Fatalf("OpenCode install = %q; want pinned version", InstallCommand)
	}
}
