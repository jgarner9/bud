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

// syncOutputStyle makes outputStyle in the Claude dir's settings.json want:
// set when it's unset or still what bud set, never over a style the user
// picked, and taken out again (want "") only while it's still bud's. haveFile
// says the style's file is bud's and in place; without it want isn't turned
// on. It records what it set in the ledger; nil means there was nothing to do.
func syncOutputStyle(want string, haveFile bool, env Env, ledger *Ledger, dryRun bool) (*Action, error) {
	if want == "" && ledger.OutputStyle == "" {
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
	if settings == nil {
		settings = map[string]any{}
	}
	cur, _ := settings["outputStyle"].(string)

	var a Action
	switch {
	case want == "":
		if cur != ledger.OutputStyle {
			// changed since bud set it: it's the user's now
			if !dryRun {
				ledger.OutputStyle = ""
			}
			return nil, nil
		}
		a = Action{"output-style", "settings.json", "remove", "no longer in the kit"}
		delete(settings, "outputStyle")
	case cur == want:
		// set by bud, or by the user before bud did, which stays theirs
		return &Action{"output-style", "settings.json", "ok", ""}, nil
	case !haveFile:
		return &Action{"output-style", "settings.json", "skip", "you have your own output-styles/" + want + ".md"}, nil
	case cur != "" && cur != ledger.OutputStyle:
		if !dryRun {
			ledger.OutputStyle = ""
		}
		return &Action{"output-style", "settings.json", "skip", "you picked " + cur}, nil
	default:
		a = Action{"output-style", "settings.json", "add", ""}
		if cur != "" {
			a.Do = "update"
		}
		settings["outputStyle"] = want
	}
	if dryRun {
		return &a, nil
	}

	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false) // hooks are full of && and >
	e.SetIndent("", "  ")
	if err := e.Encode(settings); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := atomicfile.WriteFile(p, out.Bytes(), perm); err != nil {
		return nil, err
	}
	if a.Do == "remove" {
		ledger.OutputStyle = ""
	} else {
		ledger.OutputStyle = want
	}

	return &a, nil
}
