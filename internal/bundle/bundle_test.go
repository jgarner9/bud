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
