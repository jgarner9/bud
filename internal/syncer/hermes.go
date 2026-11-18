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
// bud's (see guardHermesDir); either way the note says so, and the skill
// gets no files and no lock
func hermesSkill(root, id, src, layer string, tracked []File) ([]want, string, error) {
	dst := filepath.Join(root, id)
	ws, err := skillFiles(src, dst, layer)
	if err != nil {
		return nil, "", err
	}
	note, err := guardHermesDir(root, dst, ws, tracked)
	if err != nil {
		return nil, "", fmt.Errorf("copy skill %s for Hermes: %w", id, err)
	}
	switch {
	case note == errLinked:
		return nil, fmt.Sprintf("%s is a link, so bud didn't copy or lock the skill %s through it; replace it with a plain dir and run bud sync again", dirOfLink(root, dst, ws), id), nil
	case note == errNotOurs:
		return nil, fmt.Sprintf("Hermes already has a skill named %s that bud didn't write (%s), so it was left alone and not locked; move it aside and run bud sync again to use bud's", id, dst), nil
	}

	return ws, "", nil
}

// HermesPluginName is the plugin bud installs in Hermes, and enables in the
// config it renders, so Signal reactions and codes answer approvals (Suite
// D30)
const HermesPluginName = "bud-signal"

// hermesPlugin is bud-signal's files (from plugin, the copy in the binary)
// under root, Hermes's plugins dir, decided for the whole plugin the way a
// skill is
func hermesPlugin(root string, plugin fs.FS, tracked []File) ([]want, string, error) {
	dst := filepath.Join(root, HermesPluginName)
	var ws []want
	err := fs.WalkDir(plugin, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(plugin, path)
		if err != nil {
			return err
		}
		ws = append(ws, want{filepath.Join(dst, filepath.FromSlash(path)), b, 0o644, "hermes-plugin", "binary"})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("install the %s plugin for Hermes: %w", HermesPluginName, err)
	}
	note, err := guardHermesDir(root, dst, ws, tracked)
	if err != nil {
		return nil, "", fmt.Errorf("install the %s plugin for Hermes: %w", HermesPluginName, err)
	}
	if note == "" {
		// a file bud would skip (edited by hand, or not a plain file) would
		// leave Hermes running code that isn't bud's, so the whole plugin
		// waits, and isn't enabled
		if bad, err := changedPluginFile(ws, tracked); err != nil {
			return nil, "", fmt.Errorf("install the %s plugin for Hermes: %w", HermesPluginName, err)
		} else if bad != "" {
			return nil, fmt.Sprintf("%s was changed by hand or isn't a plain file, so bud didn't install or enable the %s plugin, and Signal approvals take a typed /approve; move it aside and run bud sync again", bad, HermesPluginName), nil
		}
	}
	switch note {
	case errLinked:
		return nil, fmt.Sprintf("%s is a link, so bud didn't install the %s plugin through it, and Signal approvals take a typed /approve; replace it with a plain dir and run bud sync again", dirOfLink(root, dst, ws), HermesPluginName), nil
	case errNotOurs:
		return nil, fmt.Sprintf("Hermes already has a plugin dir %s that bud didn't write, so it was left alone and Signal approvals take a typed /approve; move it aside and run bud sync again", dst), nil
	}

	return ws, "", nil
}

// changedPluginFile is the first of ws that planFiles would skip: one that
// isn't a regular file, or holds bytes that are neither the new ones nor
// what bud last wrote there
func changedPluginFile(ws []want, tracked []File) (string, error) {
	for _, w := range ws {
		have, exists, err := hashFile(w.path)
		if err != nil {
			return "", err
		}
		if !exists || have == hash(w.data) {
			continue
		}
		i := slices.IndexFunc(tracked, func(f File) bool { return f.Path == w.path })
		if i < 0 || tracked[i].SHA256 != have {
			return w.path, nil
		}
	}

	return "", nil
}

const (
	errLinked  = "linked"
	errNotOurs = "not ours"
)

// guardHermesDir says whether bud may write ws, the files of dst under root:
// errLinked when a dir on their path is a link, which could take a write
// outside Hermes's dir, errNotOurs when dst holds files bud doesn't track
// (Hermes's own, or Jason's), else ""
func guardHermesDir(root, dst string, ws []want, tracked []File) (string, error) {
	if dirOfLink(root, dst, ws) != "" {
		return errLinked, nil
	}
	for _, d := range hermesDirs(root, dst, ws) {
		if _, err := os.Lstat(d); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	// an empty dir is what's left of one bud removed, so it's fair game
	if _, err := os.Lstat(dst); err == nil && !emptyTree(dst) {
		ours := slices.ContainsFunc(tracked, func(f File) bool { return strings.HasPrefix(f.Path, dst+string(filepath.Separator)) })
		if !ours {
			return errNotOurs, nil
		}
	}

	return "", nil
}

// hermesDirs is root, dst and every dir under dst that holds one of ws
func hermesDirs(root, dst string, ws []want) []string {
	dirs := []string{root, dst}
	for _, w := range ws {
		for d := filepath.Dir(w.path); len(d) > len(dst); d = filepath.Dir(d) {
			dirs = append(dirs, d)
		}
	}

	return dirs
}

// dirOfLink is the first of hermesDirs that is a link, or ""
func dirOfLink(root, dst string, ws []want) string {
	for _, d := range hermesDirs(root, dst, ws) {
		if info, err := os.Lstat(d); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return d
		}
	}

	return ""
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

// signalEnvName is the variable Hermes's Signal adapter reads signal-cli's
// URL from. Hermes turns the platform on only when the URL and the bot
// account are both in its environment, so the config's extra.http_url alone
// leaves Signal off
const signalEnvName = "SIGNAL_HTTP_URL"

// signalEnv is the private Hermes .env at path with SIGNAL_HTTP_URL set to
// url, and whether that changes the file. Only that variable's lines are
// touched: every other line, the numbers and tokens Jason keeps there among
// them, stays as it is. A missing .env becomes one holding just that line
func signalEnv(path, url string) (data []byte, perm fs.FileMode, changed bool, err error) {
	perm = 0o600
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, 0, false, fmt.Errorf("read Hermes's .env to set %s: %w", signalEnvName, err)
	}
	if err == nil {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, 0, false, fmt.Errorf("read Hermes's .env to set %s: %w", signalEnvName, err)
		}
		perm = fi.Mode().Perm()
	}
	text := string(b)
	want := signalEnvName + "=" + url
	lines := strings.SplitAfter(text, "\n")
	found := false
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		bare := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "\ufeff"))
		export := strings.HasPrefix(bare, "export ")
		bare = strings.TrimSpace(strings.TrimPrefix(bare, "export "))
		name, value, ok := strings.Cut(bare, "=")
		if !ok || strings.TrimSpace(name) != signalEnvName {
			continue
		}
		found = true
		if strings.Trim(strings.TrimSpace(value), `"'`) == url {
			continue
		}
		next := want
		if export {
			next = "export " + want
		}
		if i == 0 && strings.HasPrefix(body, "\ufeff") {
			next = "\ufeff" + next
		}
		lines[i] = next + line[len(body):]
		changed = true
	}
	out := strings.Join(lines, "")
	if !found {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += want + "\n"
		changed = true
	}

	return []byte(out), perm, changed, nil
}
