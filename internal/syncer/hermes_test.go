package syncer

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestSyncedSkillsAreLockedInHermes(t *testing.T) {
	origBin, origRun := hermesBin, runHermes
	t.Cleanup(func() { hermesBin, runHermes = origBin, origRun })
	hermesBin = func() (string, error) { return "/usr/bin/hermes", nil }
	var ran []string
	runHermes = func(bin string, args ...string) ([]byte, error) {
		ran = append(ran, bin+" "+strings.Join(args, " "))
		if args[2] == "broken" {
			return []byte("curator: no skill named 'broken'"), errors.New("exit status 1")
		}
		return nil, nil
	}
	set := linkSet{"skills/b": "/x/b", "skills/a": "/x/a", "plugins/p": "/x/p"}
	if notes := lockSkills(set.skills(), func(string, ...any) {}); notes != nil {
		t.Errorf("notes: %q", notes)
	}
	if want := []string{"/usr/bin/hermes curator lock a", "/usr/bin/hermes curator lock b"}; !slices.Equal(ran, want) {
		t.Errorf("ran %q, want %q", ran, want)
	}

	notes := lockSkills([]string{"broken"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "no skill named 'broken'") {
		t.Errorf("a failed lock is named: %q", notes)
	}

	hermesBin = func() (string, error) { return "", errors.New("not found") }
	notes = lockSkills([]string{"a"}, func(string, ...any) {})
	if len(notes) != 1 || !strings.Contains(notes[0], "Hermes isn't on PATH") {
		t.Errorf("without Hermes: %q", notes)
	}
	if lockSkills(nil, nil) != nil {
		t.Error("no skills, no notes")
	}
}
