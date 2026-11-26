package cli

import (
	"jgarner.dev/bud/internal/installer"
	"jgarner.dev/bud/internal/paths"
)

// UseManagedBin puts the tools bud downloaded (D107) ahead of the system's on
// PATH, for bud and what it starts. main calls it, so tests never do
func UseManagedBin() {
	if p, err := paths.Resolve(); err == nil {
		installer.UseManagedBin(p.Data)
	}
}
