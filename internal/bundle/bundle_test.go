package bundle

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIcon(t *testing.T) {
	icns := Icon()
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:8])) != len(icns) {
		t.Fatalf("bad header %q", icns[:8])
	}
	seen := 0
	for rest := icns[8:]; len(rest) > 0; seen++ {
		kind, n := string(rest[:4]), int(binary.BigEndian.Uint32(rest[4:8]))
		img, err := png.Decode(bytes.NewReader(rest[8:n]))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if want := icnsTypes[seen].size; img.Bounds().Dx() != want || img.Bounds().Dy() != want {
			t.Errorf("%s is %v, want %d square", kind, img.Bounds(), want)
		}
		rest = rest[n:]
	}
	if seen != len(icnsTypes) {
		t.Fatalf("%d entries, want %d", seen, len(icnsTypes))
	}
}

func TestRobotIsRectangular(t *testing.T) {
	for i, line := range robot {
		if len(line) != len(robot[0]) {
			t.Errorf("row %d is %d wide, want %d", i, len(line), len(robot[0]))
		}
	}
}

func TestScript(t *testing.T) {
	src, err := Mac{Bin: `/Users/a "b"/bud`, Path: `/bin:/odd\dir`}.script()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`property budBin : "/Users/a \"b\"/bud"`,
		`property budPath : "/bin:/odd\\dir"`,
		"on open droppedItems",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("script lacks %q", want)
		}
	}
}

// TestBuild builds a real Bud.app. It needs macOS's osacompile, which some
// sandboxes can't run, so it skips when a trivial script won't compile
func TestBuild(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Bud.app is built on macOS")
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.applescript")
	os.WriteFile(probe, []byte("do shell script \"true\"\n"), 0o644)
	if err := exec.Command("osacompile", "-o", filepath.Join(dir, "probe.scpt"), probe).Run(); err != nil {
		t.Skipf("osacompile can't run here: %v", err)
	}

	dest := filepath.Join(dir, "Apps", "Bud.app")
	m := Mac{Bin: "/usr/local/bin/bud", Path: "/usr/bin:/bin", Version: "1.2.3"}
	for range 2 { // the second build replaces the first
		if err := m.Build(dest); err != nil {
			t.Fatal(err)
		}
	}
	if id, err := MacBundleID(dest); err != nil || id != MacID {
		t.Fatalf("bundle id %q, %v", id, err)
	}
	icon, err := os.ReadFile(filepath.Join(dest, "Contents", "Resources", "droplet.icns"))
	if err != nil || !bytes.Equal(icon, Icon()) {
		t.Fatalf("icon not written: %v", err)
	}
	if err := exec.Command("codesign", "--verify", dest).Run(); err != nil {
		t.Fatalf("signature: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(dest)); len(entries) != 1 {
		t.Fatalf("left behind %v", entries)
	}

	other := filepath.Join(dir, "Apps", "Other.app")
	os.MkdirAll(filepath.Join(other, "Contents"), 0o755)
	if err := m.Build(other); err == nil || !strings.Contains(err.Error(), "didn't build it") {
		t.Fatalf("replaced someone else's app: %v", err)
	}
}

func TestKittyFiles(t *testing.T) {
	m := Mac{
		Launch:    []string{"/usr/bin/env", "PATH=/bin:/my dir", "/Users/j/.local/bin/bud", "open"},
		Home:      "/Users/j",
		KittyConf: "/Users/j/.config/kitty/kitty.conf",
	}
	files := m.kittyFiles()
	want := map[string][]string{
		"kitty.conf": {"include /Users/j/.config/kitty/kitty.conf\n", "macos_quit_when_last_window_closed yes\n"},
		"macos-launch-services-cmdline": {
			"--directory=/Users/j --title=bud /usr/bin/env 'PATH=/bin:/my dir' /Users/j/.local/bin/bud open\n",
		},
		"launch-actions.conf": {
			"protocol file\n",
			"action launch --type=os-window --cwd=/Users/j /usr/bin/env 'PATH=/bin:/my dir' /Users/j/.local/bin/bud open ${FILE_PATH}\n",
		},
	}
	if len(files) != len(want) {
		t.Fatalf("files %v", files)
	}
	for name, parts := range want {
		for _, part := range parts {
			if !strings.Contains(files[name], part) {
				t.Errorf("%s lacks %q:\n%s", name, part, files[name])
			}
		}
	}
	// with no kitty.conf of yours, nothing to include
	m.KittyConf = ""
	if strings.Contains(m.kittyFiles()["kitty.conf"], "include") {
		t.Error("included a kitty.conf that isn't there")
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/bud": "/usr/bin/bud",
		"PATH=/a:/b":   "PATH=/a:/b",
		"/my dir":      "'/my dir'",
		"it's":         `'it'\''s'`,
		"":             "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeKitty is a kitty.app with what Build reads and keeps: its version, its
// own LSEnvironment and an asset-catalog icon name
func fakeKitty(t *testing.T, dir, version string) string {
	t.Helper()
	app := filepath.Join(dir, "kitty.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.MkdirAll(filepath.Join(app, "Contents", "Resources"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "kitty"), []byte("#!/bin/sh\n"), 0o755)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>kitty</string>
<key>CFBundleIdentifier</key><string>net.kovidgoyal.kitty</string>
<key>CFBundleName</key><string>kitty</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>` + version + `</string>
<key>CFBundleIconFile</key><string>kitty.icns</string>
<key>CFBundleIconName</key><string>kitty</string>
<key>LSEnvironment</key><dict><key>KITTY_LAUNCHED_BY_LAUNCH_SERVICES</key><string>1</string></dict>
</dict></plist>`
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644)
	return app
}

// TestBuildKittyCopy builds a real Bud.app from a fake kitty.app. It needs
// macOS's plutil and codesign, and skips without them
func TestBuildKittyCopy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Bud.app is built on macOS")
	}
	dir := t.TempDir()
	kitty := fakeKitty(t, dir, "0.50.0")
	if err := exec.Command("codesign", "--force", "--sign", "-", kitty).Run(); err != nil {
		t.Skipf("codesign can't run here: %v", err)
	}
	dest := filepath.Join(dir, "Apps", "Bud.app")
	m := Mac{
		Version: "1.2.3", Kitty: kitty, Home: "/Users/j",
		Launch: []string{"/usr/bin/env", "PATH=/bin", "/usr/local/bin/bud", "open"},
	}
	for range 2 { // the second build replaces the first
		if err := m.Build(dest); err != nil {
			t.Fatal(err)
		}
	}
	read := func(key string) string {
		v, err := plistString(dest, key)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return v
	}
	conf := filepath.Join(dest, "Contents", "Resources", KittyDir)
	for key, want := range map[string]string{
		"CFBundleIdentifier":                              MacID,
		"CFBundleName":                                    "Bud",
		"CFBundleDisplayName":                             "Bud",
		"CFBundleShortVersionString":                      "1.2.3",
		"CFBundleIconFile":                                "bud.icns",
		"LSEnvironment.KITTY_CONFIG_DIRECTORY":            conf,
		"LSEnvironment.KITTY_LAUNCHED_BY_LAUNCH_SERVICES": "1",
		kittyKey:        kitty,
		kittyVersionKey: "0.50.0",
	} {
		if got := read(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if _, err := plistString(dest, "CFBundleIconName"); err == nil {
		t.Error("kitty's asset-catalog icon name kept: it would win over bud.icns")
	}
	for name, text := range m.kittyFiles() {
		if got, err := os.ReadFile(filepath.Join(conf, name)); err != nil || string(got) != text {
			t.Errorf("%s: %q (%v)", name, got, err)
		}
	}
	if icon, err := os.ReadFile(filepath.Join(dest, "Contents", "Resources", "bud.icns")); err != nil || !bytes.Equal(icon, Icon()) {
		t.Errorf("icon not written: %v", err)
	}
	if err := exec.Command("codesign", "--verify", "--deep", dest).Run(); err != nil {
		t.Errorf("signature: %v", err)
	}
	if app, version := MacKitty(dest); app != kitty || version != "0.50.0" {
		t.Errorf("MacKitty = %q, %q", app, version)
	}
	if entries, _ := os.ReadDir(filepath.Dir(dest)); len(entries) != 1 {
		t.Errorf("left behind %v", entries)
	}
	// kitty itself is untouched
	if id, _ := MacBundleID(kitty); id != "net.kovidgoyal.kitty" {
		t.Errorf("kitty.app's bundle id is now %q", id)
	}
}
