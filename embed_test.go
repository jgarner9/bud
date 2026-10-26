package bud

import (
	"io/fs"
	"testing"
)

func TestNvimConfig(t *testing.T) {
	cfg := NvimConfig()

	for _, path := range []string{"init.lua", "lazy-lock.json", "lua/bud/lazy.lua"} {
		if _, err := fs.Stat(cfg, path); err != nil {
			t.Errorf("%s missing from embedded config: %v", path, err)
		}
	}

	// rooted at nvim/, so the prefix shouldn't appear
	if _, err := fs.Stat(cfg, "nvim"); err == nil {
		t.Error("embedded config should be rooted inside nvim/")
	}
}
