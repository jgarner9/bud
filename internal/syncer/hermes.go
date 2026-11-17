package syncer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Synced skills are locked in Hermes (Suite D48): bud copies each into
// Hermes's own skills dir, since `hermes curator lock` takes only a skill
// there, then locks it. Hermes records the lock in its own state, and while
// it holds, every agent write path refuses to change the skill, in normal
// turns and background passes alike. Jason's own edits aren't gated

// hermesBin finds Hermes, and runHermes runs it; tests swap both
var (
	hermesBin = func() (string, error) { return exec.LookPath("hermes") }
	runHermes = func(bin string, args ...string) ([]byte, error) {
		return exec.Command(bin, args...).CombinedOutput()
	}
)

// hermesCloud is the name of Hermes's cloud profile (Suite D95)
const hermesCloud = "cloud"

// lockSkills locks each synced skill in a Hermes profile ("" for the
// private, default one) and returns notes on what it couldn't lock. Locking
// is idempotent, so every sync locks them all again, which also covers a
// skill a new content version added
func lockSkills(profile string, ids []string, logf func(string, ...any)) []string {
	if len(ids) == 0 {
		return nil
	}
	bin, err := hermesBin()
	if err != nil {
		return []string{fmt.Sprintf("Hermes isn't on PATH, so the %d synced skills aren't locked yet; install Hermes and run bud sync again", len(ids))}
	}
	var failed []error
	for _, id := range ids {
		args := []string{"curator", "lock", id}
		if profile != "" {
			args = append([]string{"-p", profile}, args...)
		}
		if out, err := runHermes(bin, args...); err != nil {
			failed = append(failed, fmt.Errorf("%s: %v: %s", id, err, strings.TrimSpace(string(out))))
			continue
		}
		logf("locked skill %s in Hermes%s", id, profileSuffix(profile))
	}
	if len(failed) > 0 {
		cmd := "hermes"
		if profile != "" {
			cmd += " -p " + profile
		}
		return []string{fmt.Sprintf("couldn't lock every synced skill in Hermes%s, so the agent may change those; run %s curator lock <skill> for each: %v", profileSuffix(profile), cmd, errors.Join(failed...))}
	}

	return nil
}

func profileSuffix(profile string) string {
	if profile == "" {
		return ""
	}
	return "'s " + profile + " profile"
}

// privateEnv refuses a private Hermes .env that sets the key of a provider
// the private profile leaves out: Hermes's built-in providers read keys such
// as OPENCODE_GO_API_KEY from its environment, so /model could reach that
// cloud provider again (Suite D95). A missing .env is fine
func privateEnv(path string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check Hermes's .env for cloud keys: %w", err)
	}
	var set []string
	// Hermes's loader takes a first line behind a UTF-8 byte order mark
	text := strings.TrimPrefix(string(b), "\ufeff")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, _, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if ok && slices.Contains(keys, name) && !slices.Contains(set, name) {
			set = append(set, name)
		}
	}
	if len(set) > 0 {
		return fmt.Errorf("%s sets %s, the key of a cloud provider, and Hermes's private profile would reach that provider through /model (Suite D95); move the line to %s and run bud sync again (nothing was changed)", path, strings.Join(set, ", "), filepath.Join(filepath.Dir(path), "profiles", hermesCloud, ".env"))
	}

	return nil
}

// skillFiles are the files of the skill in src, to be written under dst.
// A skill's links and special files aren't copied: a link could point
// Hermes outside the skill
func skillFiles(src, dst, layer string) ([]want, error) {
	var out []want
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, want{filepath.Join(dst, rel), b, info.Mode().Perm(), "hermes-skill", layer})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("copy skill %s for Hermes: %w", filepath.Base(dst), err)
	}

	return out, nil
}

// hermesSkill is Hermes's copy of the skill id (its files in src) under
// root, Hermes's skills dir. Whether bud may write it is decided for the
// whole skill, so a skill is never half written or locked when it isn't
// bud's: a skill dir bud doesn't track (Hermes's own, or Jason's) is left
// alone, and so is one whose path goes through a link, which could take a
// write outside Hermes's skills dir. Either way the note says so, and the
// skill gets no files and no lock
func hermesSkill(root, id, src, layer string, tracked []File) ([]want, string, error) {
	dst := filepath.Join(root, id)
	ws, err := skillFiles(src, dst, layer)
	if err != nil {
		return nil, "", err
	}
	dirs := []string{root, dst}
	for _, w := range ws {
		for d := filepath.Dir(w.path); len(d) > len(dst); d = filepath.Dir(d) {
			dirs = append(dirs, d)
		}
	}
	for _, d := range dirs {
		info, err := os.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("copy skill %s for Hermes: %w", id, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Sprintf("%s is a link, so bud didn't copy or lock the skill %s through it; replace it with a plain dir and run bud sync again", d, id), nil
		}
	}
	// an empty dir is what's left of a skill bud removed, so it's fair game
	if _, err := os.Lstat(dst); err == nil && !emptyTree(dst) {
		ours := slices.ContainsFunc(tracked, func(f File) bool { return strings.HasPrefix(f.Path, dst+string(filepath.Separator)) })
		if !ours {
			return nil, fmt.Sprintf("Hermes already has a skill named %s that bud didn't write (%s), so it was left alone and not locked; move it aside and run bud sync again to use bud's", id, dst), nil
		}
	}

	return ws, "", nil
}

// emptyTree says dir holds only dirs, none of them links
func emptyTree(dir string) bool {
	empty := true
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			empty = false
			return filepath.SkipAll
		}
		return nil
	})

	return empty
}
