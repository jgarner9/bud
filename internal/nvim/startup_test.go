package nvim

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRPCRequestIsMsgpack(t *testing.T) {
	got := rpcRequest(1, "nvim_ui_attach", 80, 24, map[string]any{"ext_linegrid": true})
	want := []byte{0x94, 0x00, 0x01, 0xae}
	want = append(want, "nvim_ui_attach"...)
	want = append(want, 0x93, 0x50, 0x18, 0x81, 0xac)
	want = append(want, "ext_linegrid"...)
	want = append(want, 0xc3)
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x\nwant % x", got, want)
	}

	// the wide forms: a string past 31 bytes, an int outside 0..127, false
	long := strings.Repeat("x", 40)
	got = rpcRequest(2, "m", long, -1, false)
	want = []byte{0x94, 0x00, 0x02, 0xa1, 'm', 0x93, 0xda, 0x00, 40}
	want = append(want, long...)
	want = append(want, 0xd3, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xc2)
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x\nwant % x", got, want)
	}
}

func TestFirstScreenLeavesOutTheWaitForAScreen(t *testing.T) {
	log := `times in msec
 clock   self+sourced   self:  sourced script
000.001  000.001: --- NVIM STARTING ---
001.497  000.001: waiting for UI
004.620  003.123: done waiting for UI
020.000  000.300  000.200: sourcing /x/init.lua
034.123  000.615: first screen update
034.124  000.001: --- NVIM STARTED ---
`
	d, ok, err := firstScreen([]byte(log))
	if err != nil || !ok {
		t.Fatalf("got %v, %v, %v", d, ok, err)
	}
	if want := 31 * time.Millisecond; d != want {
		t.Errorf("got %v, want %v (34.123 less 3.123 waiting)", d, want)
	}

	// not drawn yet
	if _, ok, err := firstScreen([]byte(log[:strings.Index(log, "034.123")])); ok || err != nil {
		t.Errorf("before the first screen: got %v, %v", ok, err)
	}
	if _, _, err := firstScreen([]byte("x.y  000.6: first screen update\n")); err == nil {
		t.Error("a mangled line: expected an error")
	}
}

func TestLaunchEnvTimesFromBudsStart(t *testing.T) {
	vars := launchEnv("focus", true)
	got := map[string]string{}
	for _, kv := range vars {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	if got["NVIM_APPNAME"] != AppName || got["BUD_LAYOUT"] != "focus" || got["BUD_OPEN"] != "project" {
		t.Errorf("got %v", got)
	}
	us, err := strconv.ParseInt(got["BUD_LAUNCHED"], 10, 64)
	if err != nil || us != started.UnixMicro() {
		t.Errorf("BUD_LAUNCHED %q, want %d", got["BUD_LAUNCHED"], started.UnixMicro())
	}
}

// bud's config dirs in a temp dir, with init.lua as given
func probeConfig(t *testing.T, init string) string {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("needs nvim")
	}
	root := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, filepath.Join(root, v))
	}
	dir := filepath.Join(root, "XDG_CONFIG_HOME", AppName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(init), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStartupTimeDrawsAFirstScreen(t *testing.T) {
	// the probe is told, and a filetype gets a buffer of its own
	root := probeConfig(t, `
assert(vim.env.BUD_STARTUP_PROBE == "1", "BUD_STARTUP_PROBE unset")
vim.api.nvim_create_autocmd("FileType", { pattern = "lua", callback = function() vim.g.lua_seen = true end })
vim.o.laststatus = 3`)

	for _, ft := range []string{"", "lua"} {
		d, err := StartupTime(ft)
		if err != nil {
			t.Fatalf("%q: %v", ft, err)
		}
		if d <= 0 || d > startupTimeout {
			t.Errorf("%q: implausible %v", ft, d)
		}
	}
	// ShaDa isn't written, so the scratch file never shows in recent files
	if _, err := os.Stat(filepath.Join(root, "XDG_STATE_HOME", AppName, "shada")); !os.IsNotExist(err) {
		t.Errorf("ShaDa written: %v", err)
	}
}

func TestStartupTimeReportsAnEarlyExit(t *testing.T) {
	probeConfig(t, `io.stderr:write("broken config\n") vim.cmd("cquit 3")`)
	if _, err := StartupTime(""); err == nil || !strings.Contains(err.Error(), "exited before its first screen") {
		t.Fatalf("got %v", err)
	}
}
