package syncer

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// Synced skills are locked in Hermes (Suite D48): `hermes curator lock`
// records the lock in Hermes's own state, and while it holds, every agent
// write path refuses to change the skill, in normal turns and background
// passes alike. Jason's own edits aren't gated

// hermesBin finds Hermes, and runHermes runs it; tests swap both
var (
	hermesBin = func() (string, error) { return exec.LookPath("hermes") }
	runHermes = func(bin string, args ...string) ([]byte, error) {
		return exec.Command(bin, args...).CombinedOutput()
	}
)

// skills are the set's skill IDs, sorted
func (s linkSet) skills() []string {
	var ids []string
	for k := range s {
		if id, ok := strings.CutPrefix(k, "skills/"); ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	return ids
}

// lockSkills locks each synced skill in Hermes and returns notes on what it
// couldn't lock. Locking is idempotent, so every sync locks them all again,
// which also covers a skill a new content version added
func lockSkills(ids []string, logf func(string, ...any)) []string {
	if len(ids) == 0 {
		return nil
	}
	bin, err := hermesBin()
	if err != nil {
		return []string{fmt.Sprintf("Hermes isn't on PATH, so the %d synced skills aren't locked yet; install Hermes and run bud sync again", len(ids))}
	}
	var failed []error
	for _, id := range ids {
		if out, err := runHermes(bin, "curator", "lock", id); err != nil {
			failed = append(failed, fmt.Errorf("%s: %v: %s", id, err, strings.TrimSpace(string(out))))
			continue
		}
		logf("locked skill %s in Hermes", id)
	}
	if len(failed) > 0 {
		return []string{fmt.Sprintf("couldn't lock every synced skill in Hermes, so the agent may change those; run hermes curator lock <skill> for each: %v", errors.Join(failed...))}
	}

	return nil
}
