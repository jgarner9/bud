package syncer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// activeDirs is the dir beside the active link that holds its targets
const activeDirs = "active.d"

// linkSet names the skill and plugin dirs a device takes: skills/<id> and
// plugins/<id>, each mapped to the dir it links to
type linkSet map[string]string

// name is the set's dir name in active.d, a hash of its links, so the same
// selection always lands in the same dir and a sync that changes nothing
// leaves the active link where it was
func (s linkSet) name() string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\x00%s\x00", k, s[k])
	}

	return hash([]byte(b.String()))[:16]
}

// build makes active.d/<name> unless it's there, in a temp dir renamed into
// place, so it is either whole or absent
func (s linkSet) build(active string) error {
	dirs := filepath.Join(filepath.Dir(active), activeDirs)
	dst := filepath.Join(dirs, s.name())
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(dirs, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dirs, ".tmp-")
	if err != nil {
		return err
	}
	for _, kind := range []string{"skills", "plugins"} {
		if err := os.Mkdir(filepath.Join(tmp, kind), 0o755); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	for rel, target := range s {
		if err := os.Symlink(target, filepath.Join(tmp, rel)); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.RemoveAll(tmp)
		return err
	}

	return nil
}

// prune removes every dir in active.d but keep's
func prune(active, keep string) error {
	dirs := filepath.Join(filepath.Dir(active), activeDirs)
	entries, err := os.ReadDir(dirs)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if e.Name() != keep {
			errs = append(errs, os.RemoveAll(filepath.Join(dirs, e.Name())))
		}
	}

	return errors.Join(errs...)
}

// switchLink points link at target in one step: a new link beside it,
// renamed over it, so anything reading link sees the old target or the new
// one, never neither. A link already pointing there is left alone. It
// refuses to replace anything that isn't a link
func switchLink(link, target string) error {
	fi, err := os.Lstat(link)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink == 0:
		return fmt.Errorf("%s isn't a link, so bud won't replace it; move it aside and sync again", link)
	default:
		if t, _ := os.Readlink(link); t == target {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	tmp := link + ".tmp"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		os.Remove(tmp)
		return err
	}

	return nil
}

// readLink is link's target, or empty when there's no link
func readLink(link string) string {
	t, err := os.Readlink(link)
	if err != nil {
		return ""
	}

	return t
}
