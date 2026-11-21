package doctor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"jgarner.dev/bud"
	"jgarner.dev/bud/internal/adapter/hermes"
)

// The Signal checks run on a server-profile device, where Hermes reaches
// Jason through signal-cli's daemon and the bud-signal plugin
// (docs/internals/signal.md). Both only warn: Signal being down never makes
// bud doctor fail, so it never rolls back a bud update

// signalURL, hermesBin and hermesVersionOut are what the checks reach;
// tests swap them
var (
	signalURL        = hermes.SignalURL
	hermesBin        = func() (string, error) { return exec.LookPath("hermes") }
	hermesVersionOut = hermesFirstLine
)

// hermesFirstLine runs hermes --version and returns its first line, then
// stops it: the version is on that line, and what follows includes an
// update check over the network, which bud doctor without --net never makes
func hermesFirstLine(ctx context.Context, bin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, "--version")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	line, readErr := bufio.NewReader(io.LimitReader(out, 4096)).ReadString('\n')
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if line == "" && readErr != nil {
		return nil, fmt.Errorf("no output: %w", readErr)
	}
	return []byte(line), nil
}

// signalTimeout bounds the daemon probe; it's on loopback, so a healthy
// daemon answers at once
const signalTimeout = 2 * time.Second

// checkSignalDaemon asks signal-cli's daemon for /api/v1/check on loopback.
// Nothing leaves the machine and nothing is sent on Signal
func checkSignalDaemon() Result {
	r := Result{Name: "signal-cli"}
	ctx, cancel := context.WithTimeout(context.Background(), signalTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signalURL+"/api/v1/check", nil)
	if err == nil {
		var resp *http.Response
		if resp, err = http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("it answered %s", resp.Status)
			}
		}
	}
	if err != nil {
		r.Status = Warn
		r.Detail = fmt.Sprintf("signal-cli's daemon doesn't answer at %s (%v), so Hermes can't reach Signal and nothing can be approved from the phone", signalURL, err)
		r.Fix = "systemctl --user status bud-signal-cli, and see server/signal-cli/README.md"
		return r
	}
	r.Detail = "the daemon answers at " + signalURL
	return r
}

// pinRe reads the plugin's requires_hermes pin, such as "==0.23.2"
var pinRe = regexp.MustCompile(`(?m)^requires_hermes:\s*"==([0-9][0-9A-Za-z.+-]*)"\s*$`)

// versionRe reads the release from hermes --version's first line, such as
// "Hermes Agent v0.23.2 (2026.9.24) · stable · local bf0203a794a0"; the
// date in parentheses also looks like a version, so the v is required
var versionRe = regexp.MustCompile(`\bv(\d+\.\d+\.\d+)\b`)

// pluginHermesPin is the one Hermes release bud-signal's plugin.yaml loads on
func pluginHermesPin() (string, error) {
	b, err := fs.ReadFile(bud.HermesPlugin(), "plugin.yaml")
	if err != nil {
		return "", err
	}
	m := pinRe.FindSubmatch(b)
	if m == nil {
		return "", errors.New(`bud-signal's plugin.yaml has no requires_hermes: "==X.Y.Z" line`)
	}
	return string(m[1]), nil
}

// checkHermesVersion compares hermes --version with bud-signal's pin. On any
// other release Hermes doesn't load the plugin: approvals then need a typed
// /approve, and the reviewer socket that the gateway, ops and the router ask
// through never starts
func checkHermesVersion() Result {
	r := Result{Name: "Hermes version"}
	pin, err := pluginHermesPin()
	if err != nil {
		r.Status, r.Detail = Warn, err.Error()
		return r
	}
	bin, err := hermesBin()
	if err != nil {
		r.Status = Warn
		r.Detail = "Hermes isn't on PATH, so the bud-signal plugin (made for Hermes " + pin + ") isn't running"
		r.Fix = "install Hermes " + pin + " (server/hermes/README.md)"
		return r
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := hermesVersionOut(ctx, bin)
	var got string
	if m := versionRe.FindStringSubmatch(string(out)); m != nil {
		got = m[1]
	}
	if err != nil || got == "" {
		r.Status = Warn
		r.Detail = fmt.Sprintf("couldn't read Hermes's version from %s --version (%v: %s)", bin, err, strings.TrimSpace(string(out)))
		return r
	}
	if got != pin {
		r.Status = Warn
		r.Detail = fmt.Sprintf("Hermes is %s, but bud-signal loads only on %s, so Signal approvals need a typed /approve and the approval reviewer doesn't run", got, pin)
		r.Fix = "install Hermes " + pin + ", or wait for a bud release that raises bud-signal's pin (docs/internals/signal.md)"
		return r
	}
	r.Detail = got + ", the release bud-signal is made for"
	return r
}
