// Package atomicfile writes files so a crash never leaves one half-written:
// the data goes to a temp file in the same dir, which is then renamed over the
// target. For the JSON files bud keeps (config.json, projects.json, the sync manifest)
package atomicfile

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to path with perm, creating path's dir when needed
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	// a no-op once the rename succeeds
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}
