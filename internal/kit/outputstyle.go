package kit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"jgarner.dev/bud/internal/atomicfile"
)

// removeOutputStyle takes out the outputStyle an older bud set in the Claude
// dir's settings.json (D75), now that the kit sets none (D78): only while it's
// still the one bud set (Ledger.OutputStyle), so a style you picked since
// stays. The rest of settings.json is kept as it was. nil means there was
// nothing to do
func removeOutputStyle(env Env, ledger *Ledger, dryRun bool) (*Action, error) {
	if ledger.OutputStyle == "" {
		return nil, nil
	}
	p := filepath.Join(env.ClaudeDir, "settings.json")
	if real, err := filepath.EvalSymlinks(p); err == nil {
		// a dotfiles link stays a link
		p = real
	}
	var settings map[string]any
	perm := os.FileMode(0o644)
	data, err := os.ReadFile(p)
	switch {
	case err == nil && len(bytes.TrimSpace(data)) > 0:
		// UseNumber keeps numbers as written
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		if err := d.Decode(&settings); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if fi, err := os.Stat(p); err == nil {
			perm = fi.Mode().Perm()
		}
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	if cur, _ := settings["outputStyle"].(string); cur != ledger.OutputStyle {
		// gone, or changed since bud set it: it's yours now
		if !dryRun {
			ledger.OutputStyle = ""
		}
		return nil, nil
	}
	a := &Action{"output-style", "settings.json", "remove", "bud doesn't set one any more"}
	if dryRun {
		return a, nil
	}
	delete(settings, "outputStyle")

	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false) // hooks are full of && and >
	e.SetIndent("", "  ")
	if err := e.Encode(settings); err != nil {
		return nil, err
	}
	if err := atomicfile.WriteFile(p, out.Bytes(), perm); err != nil {
		return nil, err
	}
	ledger.OutputStyle = ""

	return a, nil
}
