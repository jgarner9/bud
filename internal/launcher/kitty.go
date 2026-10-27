package launcher

// kitty takes the program to run as its trailing arguments
func init() {
	commands["kitty"] = func(w Window, f Found) ([]string, error) {
		args := append([]string{"--directory=" + w.Dir, "--title=" + w.Title}, w.Argv...)
		if goos == "darwin" && f.App != "" {
			return openApp(f.App, args...), nil
		}

		return append([]string{f.Bin}, args...), nil
	}
}
