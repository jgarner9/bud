package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
	"jgarner.dev/bud/internal/registry"
	"jgarner.dev/bud/internal/syncer"
)

const synced = "2026.11.15.1"

// syncedDevice lays out what a sync of content synced leaves: the version
// dir with the shipped registry, the current link, one rendered file and
// a ledger naming it. It returns the paths and that file
func syncedDevice(t *testing.T) (*paths.Paths, string) {
	t.Helper()
	p := tempPaths(t)
	reg, err := os.ReadFile(filepath.Join("..", "..", "content", "env", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(p.Content(), synced, "env", "registry.jsonc"), reg)
	if err := os.Symlink(synced, p.ContentCurrent()); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(p.OpenCodeConfig(), "opencode.jsonc")
	writeFile(t, cfg, []byte("{}\n"))
	sum := sha256.Sum256([]byte("{}\n"))
	writeLedger(t, p, syncer.Ledger{
		Format: syncer.LedgerFormat, Device: "dev-machine", Channel: "edge", Content: synced,
		Files: []syncer.File{{Path: cfg, SHA256: hex.EncodeToString(sum[:]), Kind: "config", Layer: "rendered"}},
	})

	return p, cfg
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLedger(t *testing.T, p *paths.Paths, led syncer.Ledger) {
	t.Helper()
	b, err := json.Marshal(led)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.SyncLedger(), b)
}

func withSyncCheck(t *testing.T, rep *syncer.Report, err error) {
	t.Helper()
	orig := syncCheck
	syncCheck = func(syncer.Options) (*syncer.Report, error) { return rep, err }
	t.Cleanup(func() { syncCheck = orig })
}

func withChannel(t *testing.T, edge string, err error) {
	t.Helper()
	orig := fetchChannel
	fetchChannel = func(context.Context, string) (*content.Channel, error) {
		if err != nil {
			return nil, err
		}
		return &content.Channel{Channels: map[string]content.Versions{"edge": {Content: edge}}}, nil
	}
	t.Cleanup(func() { fetchChannel = orig })
}

func device(name string) config.Settings {
	s := config.Defaults()
	s.Device = name
	return s
}

// byName finds the result called name, failing the test without one
func byName(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no %q result in %+v", name, results)
	return Result{}
}

func names(results []Result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Name)
	}
	return out
}

func TestSyncChecksSkipADeviceWithNoProfile(t *testing.T) {
	p, _ := syncedDevice(t)
	if got := checkSync(p, device(""), Options{Quick: true, Net: true}); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestSyncChecksOpenCodeHarnessBeforeFirstSync(t *testing.T) {
	t.Setenv("BUD_OVERLAY", "")
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	p := tempPaths(t)
	r := byName(t, checkSync(p, device("dev-machine"), Options{}), "OpenCode harness")
	if r.Status != Warn || !strings.Contains(r.Fix, "@opencode/cli@2.3.2") {
		t.Fatalf("missing harness: %+v", r)
	}
	for _, version := range []string{"2.3.1", "2.3.2"} {
		script := "#!/bin/sh\nprintf '%s\\n' '" + version + "'\n"
		if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		r = byName(t, checkSync(p, device("dev-machine"), Options{}), "OpenCode harness")
		if version == "2.3.2" && (r.Status != OK || r.Detail != version) {
			t.Errorf("pinned harness: %+v", r)
		}
		if version != "2.3.2" && (r.Status != Warn || !strings.Contains(r.Detail, "want 2.3.2")) {
			t.Errorf("mismatched harness: %+v", r)
		}
	}
	for _, results := range [][]Result{
		checkSync(p, device("dev-machine"), Options{Quick: true}),
		checkSync(p, device("model-box"), Options{}),
	} {
		for _, r := range results {
			if r.Name == "OpenCode harness" {
				t.Fatalf("unexpected harness check in %+v", results)
			}
		}
	}
}

func TestSyncChecksPassRightAfterASync(t *testing.T) {
	t.Setenv("BUD_OVERLAY", "")
	p, _ := syncedDevice(t)
	withSyncCheck(t, &syncer.Report{Content: synced}, nil)
	withChannel(t, synced, nil)
	// nothing here reaches the network: every endpoint is down
	orig := netClient
	netClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	t.Cleanup(func() { netClient = orig })

	results := checkSync(p, device("dev-machine"), Options{Quick: true})
	if got := strings.Join(names(results), ","); got != "device profile,sync ledger,registry,content layers,synced config" {
		t.Fatalf("offline checks = %s", got)
	}
	for _, r := range results {
		if r.Status != OK {
			t.Errorf("%s = %v: %s", r.Name, r.Status, r.Detail)
		}
	}
	if r := byName(t, results, "content layers"); r.Detail != "no overlay set, so public content only; public: nothing; rendered: opencode.jsonc" {
		t.Errorf("layers detail = %q", r.Detail)
	}
	if r := byName(t, results, "sync ledger"); r.Detail != "content "+synced+", 1 file as bud sync wrote them" {
		t.Errorf("ledger detail = %q", r.Detail)
	}

	results = checkSync(p, device("dev-machine"), Options{Quick: true, Net: true})
	if r := byName(t, results, "content channel"); r.Status != OK {
		t.Errorf("channel = %v: %s", r.Status, r.Detail)
	}
	if r := byName(t, results, "model endpoints"); r.Status != Warn || !strings.Contains(r.Detail, "qwen (qwen.invalid:8080) didn't answer") {
		t.Errorf("endpoints = %v: %s", r.Status, r.Detail)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestContentLayersNameEachItemsLayer(t *testing.T) {
	p, _ := syncedDevice(t)
	dir := filepath.Join(p.Content(), synced)
	writeFile(t, filepath.Join(dir, "AGENTS.md"), []byte("public\n"))
	writeFile(t, filepath.Join(dir, "skills", "learn", "SKILL.md"), []byte("public\n"))
	writeFile(t, filepath.Join(dir, "skills", "plan", "SKILL.md"), []byte("public\n"))
	writeFile(t, filepath.Join(dir, "env", "base.jsonc"), []byte("{}\n"))
	writeFile(t, filepath.Join(dir, "env", "server.jsonc"), []byte("{}\n")) // not selected by dev-machine
	writeFile(t, filepath.Join(p.Overlay(), "overlay.json"), []byte(`{"format": 1}`))
	writeFile(t, filepath.Join(p.Overlay(), "content", "AGENTS.md"), []byte("persona\n"))
	writeFile(t, filepath.Join(p.Overlay(), "content", "skills", "learn", "SKILL.md"), []byte("mine\n"))
	writeFile(t, filepath.Join(p.Overlay(), "env", "registry.jsonc"), []byte(`{"version": 1}`))
	writeFile(t, filepath.Join(p.Overlay(), "env", "coding.jsonc"), []byte("{}\n"))
	led, err := syncer.LoadLedger(p.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	led.Overlay = &syncer.OverlayRef{Repo: "git@example.invalid:me/overlay.git", Commit: "0123456789abcdef"}
	prof := &profile.Profile{Name: "dev-machine", Components: []string{"harness"}, Channel: "edge", Content: profile.Content{Env: []string{"base", "coding"}}}

	r := checkLayers(dir, led.Overlay.Repo, p.Overlay(), prof, led)
	want := "overlay on disk (last sync layered 0123456): env/coding, instructions/AGENTS, skills/learn (replaces public); " +
		"public: env/base, instructions/AGENTS, skills/plan; rendered: opencode.jsonc"
	if r.Status != OK || r.Detail != want {
		t.Errorf("layers = %v: %q\nwant %q", r.Status, r.Detail, want)
	}

	// a device that runs no OpenCode takes only skills and plugins
	box := &profile.Profile{Name: "model-box", Components: []string{"qwen"}, Channel: "stable"}
	r = checkLayers(dir, led.Overlay.Repo, p.Overlay(), box, led)
	if want := "overlay on disk (last sync layered 0123456): skills/learn (replaces public); public: skills/plan; rendered: opencode.jsonc"; r.Detail != want {
		t.Errorf("model-box layers = %q\nwant %q", r.Detail, want)
	}

	// set but never cloned: public content only, and a sync fixes it
	r = checkLayers(dir, led.Overlay.Repo, "", prof, led)
	if r.Status != Warn || !strings.HasPrefix(r.Detail, "the overlay git@example.invalid:me/overlay.git isn't cloned yet") || r.Fix == "" {
		t.Errorf("uncloned = %v: %q fix %q", r.Status, r.Detail, r.Fix)
	}
}

func TestDeviceProfileFailsWhenItCantBeFound(t *testing.T) {
	p, _ := syncedDevice(t)
	withSyncCheck(t, &syncer.Report{}, nil)
	r := byName(t, checkSync(p, device("laptop"), Options{Quick: true}), "device profile")
	if r.Status != Fail || !strings.Contains(r.Detail, "laptop") || r.Fix == "" {
		t.Fatalf("got %+v", r)
	}
}

func TestSyncLedger(t *testing.T) {
	tests := []struct {
		name   string
		mess   func(t *testing.T, p *paths.Paths, cfg string)
		status Status
		detail string
		fix    string
	}{
		{"never synced", func(t *testing.T, p *paths.Paths, _ string) { os.Remove(p.SyncLedger()) }, Warn, "hasn't synced yet", "bud sync"},
		{"unreadable", func(t *testing.T, p *paths.Paths, _ string) { writeFile(t, p.SyncLedger(), []byte("{")) }, Fail, "move it aside", ""},
		{"current gone", func(t *testing.T, p *paths.Paths, _ string) { os.Remove(p.ContentCurrent()) }, Fail, "points at nothing", "bud sync"},
		{"content gone", func(t *testing.T, p *paths.Paths, _ string) { os.RemoveAll(filepath.Join(p.Content(), synced)) }, Fail, "is gone", "bud sync"},
		{"file missing", func(t *testing.T, p *paths.Paths, cfg string) { os.Remove(cfg) }, Warn, "missing: ", "writes missing files again"},
		{"hand edit", func(t *testing.T, p *paths.Paths, cfg string) { writeFile(t, cfg, []byte(`{"mine": 1}`)) }, Warn, "changed by hand: ", "put the change in the overlay"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, cfg := syncedDevice(t)
			withSyncCheck(t, &syncer.Report{}, nil)
			tt.mess(t, p, cfg)
			results := checkSync(p, device("dev-machine"), Options{Quick: true})
			r := byName(t, results, "sync ledger")
			if r.Status != tt.status || !strings.Contains(r.Detail, tt.detail) || !strings.Contains(r.Fix, tt.fix) {
				t.Fatalf("got %v %q (fix %q), want %v containing %q and fix %q", r.Status, r.Detail, r.Fix, tt.status, tt.detail, tt.fix)
			}
			if tt.status == Fail || tt.detail == "hasn't synced yet" {
				if len(results) != 2 {
					t.Errorf("checks past the ledger ran: %v", names(results))
				}
			}
		})
	}
}

func TestRegistryFailsWhenItDoesntValidate(t *testing.T) {
	p, _ := syncedDevice(t)
	withSyncCheck(t, &syncer.Report{}, nil)
	writeFile(t, filepath.Join(p.Content(), synced, "env", "registry.jsonc"), []byte(`{"version": 1, "roles": {"coding": {"model": "nope"}}}`))

	results := checkSync(p, device("dev-machine"), Options{Quick: true})
	r := byName(t, results, "registry")
	if r.Status != Fail || !strings.Contains(r.Detail, "nope") {
		t.Fatalf("got %+v", r)
	}
	if strings.Contains(strings.Join(names(results), ","), "synced config") {
		t.Error("the render check ran on a registry that doesn't validate")
	}
}

func TestSyncedConfig(t *testing.T) {
	tests := []struct {
		name   string
		rep    func(cfg string) *syncer.Report
		err    error
		mess   bool // hand-edit opencode.jsonc, which the ledger check names
		status Status
		detail string
	}{
		{"matches", func(string) *syncer.Report { return &syncer.Report{} }, nil, false, OK, "matches what this bud renders"},
		{"would change", func(string) *syncer.Report {
			return &syncer.Report{Changes: []syncer.Change{{Action: syncer.Write, Path: "/x/agents/review.md"}}}
		}, nil, false, Warn, "bud sync would write /x/agents/review.md"},
		{"not bud's", func(string) *syncer.Report {
			return &syncer.Report{Skipped: []syncer.Change{{Action: syncer.Skip, Path: "/x/commands/check.md"}}}
		}, nil, false, Warn, "not bud's, so bud's version isn't in use: /x/commands/check.md"},
		{"hand edit named once", func(cfg string) *syncer.Report {
			return &syncer.Report{Skipped: []syncer.Change{{Action: syncer.Skip, Path: cfg}}}
		}, nil, true, OK, "matches"},
		{"can't render", func(string) *syncer.Report { return nil }, errors.New("env fragment coding sets update"), false, Fail, "sets update"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, cfg := syncedDevice(t)
			withSyncCheck(t, tt.rep(cfg), tt.err)
			if tt.mess {
				writeFile(t, cfg, []byte(`{"mine": 1}`))
			}
			r := byName(t, checkSync(p, device("dev-machine"), Options{Quick: true}), "synced config")
			if r.Status != tt.status || !strings.Contains(r.Detail, tt.detail) {
				t.Fatalf("got %v %q, want %v containing %q", r.Status, r.Detail, tt.status, tt.detail)
			}
			if r.Status != OK && r.Fix == "" {
				t.Error("no fix")
			}
		})
	}
}

func TestContentChannel(t *testing.T) {
	tests := []struct {
		name       string
		edge       string
		err        error
		rolledBack []string
		status     Status
		detail     string
	}{
		{"newest", synced, nil, nil, OK, "is the newest on edge"},
		{"behind", "2026.11.15.2", nil, nil, Warn, "content 2026.11.15.2 is out on edge"},
		{"held after a rollback", "2026.11.15.2", nil, []string{"2026.11.15.2"}, OK, "held on " + synced},
		{"nothing offered", "", nil, nil, Warn, "offers no content"},
		{"older offered", "2026.11.14.1", nil, nil, Warn, "older than"},
		{"offline", "", errors.New("download: no route to host"), nil, Warn, "couldn't check: download"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withChannel(t, tt.edge, tt.err)
			prof := &profile.Profile{Name: "dev-machine", Channel: "edge"}
			led := &syncer.Ledger{Content: synced, RolledBack: tt.rolledBack}
			r := checkChannel(context.Background(), "", prof, led)
			if r.Status != tt.status || !strings.Contains(r.Detail, tt.detail) {
				t.Fatalf("got %v %q, want %v containing %q", r.Status, r.Detail, tt.status, tt.detail)
			}
		})
	}
}

func TestModelEndpoints(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/facts/models" {
			w.Write([]byte(`{"data": [{"id": "glm-5.3", "limit": {"context": 2000000, "output": 0}}]}`))
			return
		}
		if r.URL.Path == "/empty/models" {
			w.Write([]byte(`{"data": []}`))
			return
		}
		if r.URL.Path != "/v1/models" || r.URL.Query().Get("v") != "1" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data": [{"id": "glm-5.3"}, {"id": "qwen-35b"}]}`))
	}))
	t.Cleanup(srv.Close)
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()

	reg := func(endpoint, model string) *registry.Registry {
		return &registry.Registry{
			Providers: map[string]registry.Provider{"go": {Endpoint: endpoint, Key: "{env:TEST_GO_KEY}"}},
			Models:    map[string]registry.Model{"m": {Provider: "go", Name: model}},
			Roles:     map[string]registry.Role{"coding": {Model: "m"}},
		}
	}
	tests := []struct {
		name     string
		endpoint string
		model    string
		status   Status
		detail   string
	}{
		{"listed", srv.URL + "/v1?v=1", "glm-5.3", OK, "1 provider answered"},
		{"not listed", srv.URL + "/v1/?v=1", "kimi-k3", Warn, "go doesn't list kimi-k3"},
		{"empty list", srv.URL + "/empty", "glm-5.3", Warn, "go doesn't list glm-5.3"},
		{"facts drifted", srv.URL + "/facts", "glm-5.3", Warn, "go reports different facts for m.context"},
		{"down", downURL + "/v1", "glm-5.3", Warn, "go (" + strings.TrimPrefix(downURL, "http://") + ") didn't answer"},
	}
	t.Setenv("TEST_GO_KEY", "sekrit")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := checkEndpoints(context.Background(), reg(tt.endpoint, tt.model))
			if r.Status != tt.status || !strings.Contains(r.Detail, tt.detail) {
				t.Fatalf("got %v %q, want %v containing %q", r.Status, r.Detail, tt.status, tt.detail)
			}
		})
	}
	if gotAuth != "Bearer sekrit" {
		t.Errorf("an {env:VAR} key that's set should be sent; got %q", gotAuth)
	}

	// without its key, a provider that refuses still answered
	t.Setenv("TEST_GO_KEY", "")
	locked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(locked.Close)
	if r := checkEndpoints(context.Background(), reg(locked.URL, "anything")); r.Status != OK {
		t.Errorf("a 401 = %v %q, want ok", r.Status, r.Detail)
	}
}

func TestSyncChecksWaitOutARunningSync(t *testing.T) {
	p, _ := syncedDevice(t)
	withSyncCheck(t, &syncer.Report{}, nil)
	unlock, err := syncer.TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	// another holder of the lock, as a running sync is: flock is per open file
	results := checkSync(p, device("dev-machine"), Options{Quick: true})
	if len(results) != 1 || results[0].Status != Warn || !strings.Contains(results[0].Detail, "a bud sync is running") {
		t.Fatalf("got %+v", results)
	}
}

func TestReviewChannelWarnsOnlyOnAServerDeviceAndNeverFails(t *testing.T) {
	t.Setenv("BUD_OVERLAY", "")
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	p := tempPaths(t)
	if got := names(checkSync(p, device("dev-machine"), Options{Quick: true})); slices.Contains(got, "approval reviewer") {
		t.Fatalf("a device without the server component was checked: %v", got)
	}

	if got := names(checkSync(p, device("server"), Options{Quick: true})); !slices.Contains(got, "approval reviewer") {
		t.Fatalf("a server device wasn't checked: %v", got)
	}
	r := checkReviewChannel(p)
	if r.Status != Warn || !strings.Contains(r.Detail, "no socket") || !strings.Contains(r.Detail, "secret isn't usable") {
		t.Fatalf("missing: %+v", r)
	}
	if err := os.MkdirAll(filepath.Dir(p.ReviewSecret()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ReviewSecret(), []byte("s3cret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := checkReviewChannel(p); r.Status != Warn || strings.Contains(r.Detail, "secret isn't usable") || !strings.Contains(r.Detail, "no socket") {
		t.Errorf("secret only: %+v", r)
	}
	if err := os.MkdirAll(filepath.Dir(p.ReviewSocket()), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", p.ReviewSocket())
	if err != nil {
		t.Skip("no unix sockets here:", err)
	}
	defer l.Close()
	if r := checkReviewChannel(p); r.Status != OK {
		t.Errorf("in place: %+v", r)
	}
	// a secret the reviewer would refuse warns too
	if err := os.Chmod(p.ReviewSecret(), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := checkReviewChannel(p); r.Status != Warn || !strings.Contains(r.Detail, "mode 0600") {
		t.Errorf("loose secret: %+v", r)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	p2, _ := paths.Resolve()
	if r := checkReviewChannel(p2); r.Status != Warn || !strings.Contains(r.Detail, "XDG_RUNTIME_DIR") {
		t.Errorf("no runtime dir: %+v", r)
	}
}
