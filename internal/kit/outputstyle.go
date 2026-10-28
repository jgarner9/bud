package kit

import (
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
// picked, and taken out again (want "") only while it's still bud's. It
// records what it set in the ledger; nil means there was nothing to do.
func syncOutputStyle(want string, env Env, ledger *Ledger, dryRun bool) (*Action, error) {
	if want == "" && ledger.OutputStyle == "" {
		return nil, nil
	}
	p := filepath.Join(env.ClaudeDir, "settings.json")
	settings := map[string]any{}
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
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

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(env.ClaudeDir, 0o755); err != nil {
		return nil, err
	}
	if err := atomicfile.WriteFile(p, append(out, '\n'), 0o644); err != nil {
		return nil, err
	}
	if a.Do == "remove" {
		ledger.OutputStyle = ""
	} else {
		ledger.OutputStyle = want
	}

	return &a, nil
}
