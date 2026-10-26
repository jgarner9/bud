// Package bud holds the repo files that ship inside the bud binary
package bud

import (
	"embed"
	"io/fs"
)

//go:embed nvim
var nvimFS embed.FS

// NvimConfig returns the repo's nvim/ directory, rooted so paths read like "init.lua"
func NvimConfig() fs.FS {
	sub, err := fs.Sub(nvimFS, "nvim")
	if err != nil {
		// only fails on a bad path, which the embed directive above rules out
		panic(err)
	}

	return sub
}
