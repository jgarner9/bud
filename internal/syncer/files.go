package syncer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Actions a sync takes, or would take in a dry run
const (
	Write  = "write"
	Delete = "delete"
	Skip   = "skip"
	Switch = "switch"
)

// Change is one thing a sync did, or would do
type Change struct {
	Action string
	Path   string
	Why    string // for a skip, why and what to do; for a switch, from what to what
}

// want is a file the sync means to leave on disk
type want struct {
	path        string
	data        []byte
	perm        os.FileMode
	kind, layer string
}

// fileStep is a planned write or delete, with the ledger entry it leaves
type fileStep struct {
	Change
	want  *want
	entry File // what the ledger records once the step is done
}

// planFiles compares the files a sync wants with the disk and the ledger.
// It writes a file that's missing, or that is still what bud last wrote
// there; a file edited by hand, or that bud never wrote, is skipped and
// named, never overwritten. A file bud never wrote that already holds what
// bud would write is left alone and not taken over. A file bud wrote that the sync no longer wants
// is deleted if it's unchanged. keep is the ledger entries for files left
// as they are; each step's entry joins them once it's done
func planFiles(old []File, wants []want) (steps []fileStep, keep []File, skips []Change, err error) {
	tracked := map[string]File{}
	for _, f := range old {
		tracked[f.Path] = f
	}
	slices.SortFunc(wants, func(a, b want) int { return strings.Compare(a.path, b.path) })

	wanted := map[string]bool{}
	for i := range wants {
		w := &wants[i]
		wanted[w.path] = true
		sum := hash(w.data)
		entry := File{Path: w.path, SHA256: sum, Kind: w.kind, Layer: w.layer}
		data, exists, err := readRegularFile(w.path)
		have := fileHash(data, exists)
		if err != nil {
			return nil, nil, nil, err
		}
		prev, ours := tracked[w.path]
		if ours && data != nil && have != sum && have != prev.SHA256 && isHermesConfig(entry) {
			managed, state := splitHermesConfig(data)
			have = hash(managed)
			if have == prev.SHA256 && have != sum {
				w.data = state.carryInto(w.data)
			}
		}
		switch {
		case exists && have == sum && ours:
			keep = append(keep, entry)
		case exists && have == sum:
			// the same bytes, but not bud's: left as it is and never
			// recorded, so a later release can't overwrite or delete it
		case !exists || (ours && have == prev.SHA256):
			steps = append(steps, fileStep{Change: Change{Action: Write, Path: w.path}, want: w, entry: entry})
		case ours:
			// still bud's, so bud doctor keeps flagging the edit
			keep = append(keep, prev)
			skips = append(skips, Change{Skip, w.path, "changed by hand since bud wrote it, so bud left it; move it aside and sync again to take bud's version"})
		default:
			skips = append(skips, Change{Skip, w.path, "isn't a file bud wrote, so bud left it; move it aside and sync again to let bud write it"})
		}
	}

	for _, f := range old {
		if wanted[f.Path] {
			continue
		}
		have, exists, err := hashFile(f.Path)
		if err != nil {
			return nil, nil, nil, err
		}
		switch {
		case !exists:
		case have == f.SHA256:
			steps = append(steps, fileStep{Change: Change{Action: Delete, Path: f.Path}})
		default:
			skips = append(skips, Change{Skip, f.Path, "no longer synced, but changed by hand, so bud left it and stopped tracking it"})
		}
	}
	slices.SortStableFunc(steps, func(a, b fileStep) int { return strings.Compare(a.Path, b.Path) })

	return steps, keep, skips, nil
}

// apply runs one step
func (s fileStep) apply() error {
	if s.Action == Delete {
		err := os.Remove(s.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	return atomicfile.WriteFile(s.Path, s.want.data, s.want.perm)
}

func hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hashFile is path's sha256 and whether it exists. Anything there that
// isn't a regular file, such as a dir or a link, hashes as nothing a
// sync could have written, so it is never replaced
func hashFile(path string) (string, bool, error) {
	data, exists, err := readRegularFile(path)
	return fileHash(data, exists), exists, err
}

func fileHash(data []byte, exists bool) string {
	switch {
	case data != nil:
		return hash(data)
	case exists:
		return "not a regular file"
	default:
		return ""
	}
}

func readRegularFile(path string) ([]byte, bool, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !fi.Mode().IsRegular() {
		return nil, true, nil
	}
	b, err := os.ReadFile(path)
	return b, true, err
}
