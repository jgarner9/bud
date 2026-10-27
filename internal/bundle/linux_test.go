package bundle

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/paths"
)

func TestExecArg(t *testing.T) {
	for in, want := range map[string]string{
		"launch":          "launch",
		"/home/a b/bud":   `"/home/a b/bud"`,
		`/odd"$x\y`:       `"/odd\"\$x\\y"`,
		"100%":            `"100%%"`,
		"PATH=/bin:/sbin": `"PATH=/bin:/sbin"`,
	} {
		if got := execArg(in); got != want {
			t.Errorf("execArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestLinuxInstall(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "applications", "bud.desktop")
	icon := func(size int) string { return filepath.Join(dir, "icons", fmt.Sprint(size), "bud.png") }
	l := Linux{Bin: "/home/me/.local/bin/bud", Path: `/usr/bin:/odd\dir`, Version: "1.2.3"}

	for range 2 { // the second install replaces the first
		if err := l.Install(entry, icon); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[Desktop Entry]\n",
		// the \ is quoted for Exec, then escaped again as a string value
		"\nExec=env \"PATH=/usr/bin:/odd\\\\\\\\dir\" /home/me/.local/bin/bud launch %F\n",
		"\nIcon=bud\n",
		"\nMimeType=inode/directory;text/plain;\n",
		"\nX-Bud-Version=1.2.3\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("entry lacks %q:\n%s", want, data)
		}
	}
	for _, size := range IconSizes {
		png, err := os.ReadFile(icon(size))
		if err != nil || !bytes.Equal(png, iconPNG(size)) {
			t.Errorf("icon %d: %v", size, err)
		}
	}

	os.WriteFile(entry, []byte("[Desktop Entry]\nName=Someone else's\n"), 0o644)
	if err := l.Install(entry, icon); err == nil || !strings.Contains(err.Error(), "didn't write it") {
		t.Fatalf("replaced someone else's entry: %v", err)
	}
}

func TestFindLinux(t *testing.T) {
	orig := goos
	goos = "linux"
	t.Cleanup(func() { goos = orig })
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if found := Find(p); len(found) != 0 {
		t.Fatalf("found %+v before installing", found)
	}

	if err := (Linux{Bin: "/bin/bud", Path: "/bin"}).Install(p.DesktopEntry(), p.DesktopIcon); err != nil {
		t.Fatal(err)
	}
	found := Find(p)
	if len(found) != 1 || found[0].Path != p.DesktopEntry() || len(found[0].Files) != 1+len(IconSizes) {
		t.Fatalf("found %+v", found)
	}
	if err := found[0].Remove(); err != nil {
		t.Fatal(err)
	}
	if found := Find(p); len(found) != 0 {
		t.Fatalf("found %+v after removing", found)
	}

	// someone else's bud.desktop isn't bud's to remove
	os.WriteFile(p.DesktopEntry(), []byte("[Desktop Entry]\n"), 0o644)
	if found := Find(p); len(found) != 0 {
		t.Fatalf("claimed %+v", found)
	}
}
