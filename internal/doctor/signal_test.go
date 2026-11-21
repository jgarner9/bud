package doctor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func swapSignal(t *testing.T, url string, bin func() (string, error), out func(context.Context, string) ([]byte, error)) {
	t.Helper()
	oldURL, oldBin, oldOut := signalURL, hermesBin, hermesVersionOut
	t.Cleanup(func() { signalURL, hermesBin, hermesVersionOut = oldURL, oldBin, oldOut })
	signalURL, hermesBin, hermesVersionOut = url, bin, out
}

func onPath(context.Context, string) ([]byte, error) { return nil, errors.New("unused") }

func TestSignalDaemonCheckWarnsWhenItDoesntAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/check":
		case "/away/api/v1/check":
			http.Redirect(w, r, "/api/v1/check", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	swapSignal(t, srv.URL, nil, nil)
	if r := checkSignalDaemon(); r.Status != OK || !strings.Contains(r.Detail, srv.URL) {
		t.Errorf("answering: %+v", r)
	}
	swapSignal(t, srv.URL+"/nope", nil, nil)
	if r := checkSignalDaemon(); r.Status != Warn || !strings.Contains(r.Detail, "404") || !strings.Contains(r.Fix, "bud-signal-cli") {
		t.Errorf("404: %+v", r)
	}
	// a redirect is never followed, even to a path that would answer
	swapSignal(t, srv.URL+"/away", nil, nil)
	if r := checkSignalDaemon(); r.Status != Warn || !strings.Contains(r.Detail, "302") {
		t.Errorf("redirect: %+v", r)
	}
	srv.Close()
	swapSignal(t, srv.URL, nil, nil)
	if r := checkSignalDaemon(); r.Status != Warn || !strings.Contains(r.Detail, "doesn't answer") {
		t.Errorf("down: %+v", r)
	}
}

func TestThePluginPinIsReadFromTheEmbeddedPluginYAML(t *testing.T) {
	pin, err := pluginHermesPin()
	if err != nil || pin != "0.23.2" {
		t.Fatalf("pin %q, %v", pin, err)
	}
}

func TestHermesVersionCheckComparesWithThePluginPin(t *testing.T) {
	pin, _ := pluginHermesPin()
	found := func() (string, error) { return "/x/hermes", nil }
	version := func(line string) func(context.Context, string) ([]byte, error) {
		return func(context.Context, string) ([]byte, error) { return []byte(line), nil }
	}
	swapSignal(t, "", found, version("Hermes Agent v"+pin+" (2026.9.24) · stable · local bf0203a794a0\n"))
	if r := checkHermesVersion(); r.Status != OK || !strings.HasPrefix(r.Detail, pin) {
		t.Errorf("pinned: %+v", r)
	}
	// the date looks like a version too, and must not be taken for it
	swapSignal(t, "", found, version("Hermes Agent v0.24.0 ("+pin+")\n"))
	if r := checkHermesVersion(); r.Status != Warn || !strings.Contains(r.Detail, "Hermes is 0.24.0") || !strings.Contains(r.Detail, "/approve") {
		t.Errorf("other release: %+v", r)
	}
	swapSignal(t, "", found, version("something odd\n"))
	if r := checkHermesVersion(); r.Status != Warn || !strings.Contains(r.Detail, "couldn't read") {
		t.Errorf("unreadable: %+v", r)
	}
	swapSignal(t, "", func() (string, error) { return "", errors.New("not found") }, onPath)
	if r := checkHermesVersion(); r.Status != Warn || !strings.Contains(r.Detail, "isn't on PATH") || !strings.Contains(r.Fix, pin) {
		t.Errorf("missing: %+v", r)
	}
}

// the update check after the first line is never waited for
func TestHermesFirstLineStopsAfterTheVersionLine(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hermes")
	script := "#!/bin/sh\necho 'Hermes Agent v0.23.2 (2026.9.24)'\nsleep 30\necho 'update available'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := hermesFirstLine(context.Background(), bin)
	if err != nil || string(out) != "Hermes Agent v0.23.2 (2026.9.24)\n" {
		t.Fatalf("%q, %v", out, err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("waited %s for the rest of the output", d)
	}
}

// a child that holds stdout open and keeps running is killed with it
func TestHermesFirstLineKillsChildrenThatHoldThePipe(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := filepath.Join(dir, "hermes"), filepath.Join(dir, "child.pid")
	script := "#!/bin/sh\nsleep 30 &\necho $! > " + pidFile + "\necho 'Hermes Agent v0.23.2'\nwait\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := hermesFirstLine(context.Background(), bin)
	if err != nil || !strings.Contains(string(out), "v0.23.2") {
		t.Fatalf("%q, %v", out, err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("waited %s", d)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// a version line that never ends is cut off at the deadline
func TestHermesFirstLineStopsAtTheDeadline(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'Hermes'\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, _ := hermesFirstLine(ctx, bin)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("waited %s", d)
	}
	if versionRe.Match(out) {
		t.Errorf("got a version from %q", out)
	}
}

func TestSignalChecksRunOnlyOnAServerAndHermesOnlyWhenNotQuick(t *testing.T) {
	t.Setenv("BUD_OVERLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	swapSignal(t, "http://127.0.0.1:1", func() (string, error) { return "", errors.New("no") }, onPath)
	p := tempPaths(t)
	if got := names(checkSync(p, device("dev-machine"), Options{Quick: true})); slices.Contains(got, "signal-cli") {
		t.Errorf("dev machine: %v", got)
	}
	got := names(checkSync(p, device("server"), Options{Quick: true}))
	if !slices.Contains(got, "signal-cli") || slices.Contains(got, "Hermes version") {
		t.Errorf("quick: %v", got)
	}
	got = names(checkSync(p, device("server"), Options{}))
	if !slices.Contains(got, "signal-cli") || !slices.Contains(got, "Hermes version") {
		t.Errorf("full: %v", got)
	}
	for _, r := range checkSync(p, device("server"), Options{}) {
		if (r.Name == "signal-cli" || r.Name == "Hermes version") && r.Status == Fail {
			t.Errorf("%s failed: %+v", r.Name, r)
		}
	}
}
