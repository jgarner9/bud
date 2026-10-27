package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Receipt is what install.sh records about the binary it installed. Its absence
// means bud came from somewhere else (go install, a package manager), so bud
// must leave that binary alone (D20)
type Receipt struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// ReadReceipt returns nil, nil when there's no receipt
func ReadReceipt(path string) (*Receipt, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// the path decides what uninstall deletes, so it has to name a bud binary
	if !filepath.IsAbs(r.Path) || filepath.Base(r.Path) != "bud" {
		return nil, fmt.Errorf("%s: %q is not an absolute path to a bud binary", path, r.Path)
	}

	return &r, nil
}
