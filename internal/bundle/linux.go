package bundle

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/atomicfile"
)

// Linux is what goes into bud.desktop: the same as Bud.app, an entry that
// runs bud launch with what was opened (%F)
type Linux struct {
	Bin     string // the bud it runs
	Path    string // the PATH it runs bud with
	Version string
}

// linuxMarker is in every entry bud writes, which is how it knows it may
// replace one
const linuxMarker = "X-Bud-Version="

// IconSizes are the hicolor sizes the entry's icon is written at
var IconSizes = []int{64, 128, 256, 512}

// Install writes the entry at entry and its icon at icon(size) for each of
// IconSizes. It replaces an entry bud wrote, and refuses any other
func (l Linux) Install(entry string, icon func(size int) string) error {
	if data, err := os.ReadFile(entry); err == nil && !bytes.Contains(data, []byte(linuxMarker)) {
		return fmt.Errorf("%s is already there and bud didn't write it; move it away first", entry)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	src, err := l.entry()
	if err != nil {
		return err
	}
	for _, size := range IconSizes {
		if err := os.MkdirAll(filepath.Dir(icon(size)), 0o755); err != nil {
			return err
		}
		if err := atomicfile.WriteFile(icon(size), iconPNG(size), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		return err
	}
	if err := atomicfile.WriteFile(entry, src, 0o644); err != nil {
		return err
	}
	// menus and file managers notice a new entry on their own; these only
	// make it quicker, where they're installed
	if bin, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(bin, filepath.Dir(entry)).Run()
	}

	return nil
}

func (l Linux) entry() ([]byte, error) {
	src, err := fs.ReadFile(bud.Bundles(), "linux/bud.desktop")
	if err != nil {
		return nil, err
	}
	t, err := template.New("bud.desktop").Parse(string(src))
	if err != nil {
		return nil, err
	}
	exec := []string{"env", "PATH=" + l.Path, l.Bin, "launch"}
	for i, arg := range exec {
		exec[i] = desktopValue(execArg(arg))
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]string{"Exec": strings.Join(exec, " "), "Version": desktopValue(l.Version)})

	return buf.Bytes(), err
}

// execArg quotes one Exec argument by the desktop entry spec: in double
// quotes, with ", `, $ and \ escaped, and % doubled since it starts a field code
func execArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\><~|&;$*?#()`%=") {
		return s
	}
	s = strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`, `\`, `\\`, `%`, `%%`).Replace(s)
	return `"` + s + `"`
}

// desktopValue escapes a string value, which the spec unescapes before the
// Exec quoting is read, so a backslash has to be doubled again
func desktopValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`).Replace(s)
}
