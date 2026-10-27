package launcher

import "errors"

// ghostty runs what follows -e. On macOS its CLI can't open windows, so it
// has to go through the app bundle
func init() {
	commands["ghostty"] = func(w Window, f Found) ([]string, error) {
		args := append([]string{"--working-directory=" + w.Dir, "--title=" + w.Title, "-e"}, w.Argv...)
		if goos == "darwin" {
			if f.App == "" {
				return nil, errors.New("ghostty on macOS opens windows through Ghostty.app, which isn't in /Applications or ~/Applications")
			}
			return openApp(f.App, args...), nil
		}

		return append([]string{f.Bin}, args...), nil
	}
}
