package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	models "jgarner.dev/bud/internal/registry"
)

func TestRouterRefusesAListenAddressOutsideTheTailnet(t *testing.T) {
	for _, l := range []string{"0.0.0.0:8795", "192.168.1.5:8795", "localhost:8795", ":8795", "nonsense"} {
		err := runRouter(&Globals{}, nil, l, "", "", 10, 10*time.Minute, false)
		var ue *UsageError
		if !errors.As(err, &ue) {
			t.Errorf("%s: err = %v, want a usage error", l, err)
		}
	}
	err := runRouter(&Globals{}, nil, "0.0.0.0:1", "", "", 10, 10*time.Minute, false)
	if err == nil || !strings.Contains(err.Error(), "Suite D72") {
		t.Errorf("err = %v", err)
	}
}

func TestRouterServesTheRegistryAtThePathGiven(t *testing.T) {
	path := filepath.Join("..", "..", "content", "env", "registry.jsonc")
	reg, err := models.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for pid, p := range reg.Providers {
		if v := models.KeyVar(pid, p.Key); v != "" {
			t.Setenv(v, "test-key")
		}
	}
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	if err := os.MkdirAll(filepath.Join(state, "bud"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "bud", "router-p0-servers.json"), []byte(`{"servers": ["calendar", "mail"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var addr string
	orig := serveRouter
	serveRouter = func(_ context.Context, a string, _ http.Handler) error { addr = a; return nil }
	t.Cleanup(func() { serveRouter = orig })
	var stderr bytes.Buffer
	if err := runRouter(&Globals{stderr: &stderr}, nil, "127.0.0.1:0", path, filepath.Join(t.TempDir(), "spend.json"), 10, 10*time.Minute, false); err != nil {
		t.Fatal(err)
	}
	if addr != "127.0.0.1:0" {
		t.Errorf("served on %q", addr)
	}
	// without --verbose too, so the journal shows the rules were loaded
	if want := fmt.Sprintf("%d privacy rules, 2 P0-only servers", len(reg.PrivacyRules)); len(reg.PrivacyRules) == 0 || !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want a line saying %q", stderr.String(), want)
	}
}

func TestRouterWithoutSyncedContentSaysToSync(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := runRouter(&Globals{}, nil, "127.0.0.1:0", "", "", 10, 10*time.Minute, false)
	if err == nil || !strings.Contains(err.Error(), "bud sync") {
		t.Errorf("err = %v", err)
	}
}

func TestRouterRefusesACapThatIsntADollarAmount(t *testing.T) {
	path := filepath.Join("..", "..", "content", "env", "registry.jsonc")
	for _, c := range []float64{-1, math.NaN(), math.Inf(1)} {
		err := runRouter(&Globals{}, nil, "127.0.0.1:0", path, filepath.Join(t.TempDir(), "spend.json"), c, 10*time.Minute, false)
		var ue *UsageError
		if !errors.As(err, &ue) {
			t.Errorf("%v: err = %v, want a usage error", c, err)
		}
	}
}

func TestRouterRefusesAP0WaitOutsideZeroToAnHour(t *testing.T) {
	path := filepath.Join("..", "..", "content", "env", "registry.jsonc")
	for _, d := range []time.Duration{0, -time.Second, time.Hour + time.Second} {
		err := runRouter(&Globals{}, nil, "127.0.0.1:0", path, filepath.Join(t.TempDir(), "spend.json"), 10, d, false)
		var ue *UsageError
		if !errors.As(err, &ue) {
			t.Errorf("%v: err = %v, want a usage error", d, err)
		}
	}
}

// Suite D119: without the synced list of P0-only servers the router won't
// start, since a calendar result couldn't keep a chat local
func TestRouterWithoutTheP0ServersSaysToSync(t *testing.T) {
	path := filepath.Join("..", "..", "content", "env", "registry.jsonc")
	reg, err := models.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for pid, p := range reg.Providers {
		if v := models.KeyVar(pid, p.Key); v != "" {
			t.Setenv(v, "test-key")
		}
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	err = runRouter(&Globals{}, nil, "127.0.0.1:0", path, filepath.Join(t.TempDir(), "spend.json"), 10, 10*time.Minute, false)
	if err == nil || !strings.Contains(err.Error(), "run bud sync") || !strings.Contains(err.Error(), "P0-only") {
		t.Errorf("err = %v", err)
	}
}
