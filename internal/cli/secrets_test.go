package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/audit"
	"jgarner.dev/bud/internal/optoken"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/secretrun"
)

const secretsTestToken = "ops_secrets-run-test-token"

// secretsDevice is a throwaway device: an opencode.env with one reference, a
// fake op that resolves references to "resolved-for-NAME" and records the
// token it was started with, a fake gh that records what it can see, and a
// token that no real Keychain is asked for
type secretsDevice struct {
	p       *paths.Paths
	scratch string // TMPDIR: where the fakes write what they saw
}

func newSecretsDevice(t *testing.T, ghBody string) *secretsDevice {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.OpenCodeEnv(), []byte("# bud sync\nGITHUB_PERSONAL_ACCESS_TOKEN=op://vault/github/pat\nEXA_API_KEY=op://vault/exa/key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin, scratch := t.TempDir(), t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("op", `echo "$OP_SERVICE_ACCOUNT_TOKEN" > "$TMPDIR/op-saw-token"
[ "$1" = run ] && [ "$2" = -- ] || exit 9
shift 2
for v in $(env | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=op:\/\/.*/\1/p'); do export "$v=resolved-for-$v"; done
exec "$@"`)
	write("gh", ghBody)
	// the fakes are the trusted tools; the session's PATH is only a decoy
	origDirs := secretrun.TrustedDirs
	t.Cleanup(func() { secretrun.TrustedDirs = origDirs })
	secretrun.TrustedDirs = []string{bin, "/usr/bin", "/bin"}
	t.Setenv("PATH", t.TempDir()+":/usr/bin:/bin")
	t.Setenv("TMPDIR", scratch)
	t.Setenv("GH_HOST", "evil.example")
	t.Setenv(optoken.EnvVar, "")

	orig := secretsTokenFinder
	t.Cleanup(func() { secretsTokenFinder = orig })
	secretsTokenFinder = func(*paths.Paths) optoken.Finder {
		return optoken.Finder{GOOS: "linux", Getenv: func(string) string { return secretsTestToken }}
	}

	return &secretsDevice{p: p, scratch: scratch}
}

func (d *secretsDevice) saw(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(d.scratch, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	return strings.TrimSpace(string(b))
}

func (d *secretsDevice) events(t *testing.T) []audit.Event {
	t.Helper()
	evs, err := audit.Read(d.p.Audit())
	if err != nil {
		t.Fatal(err)
	}

	return evs
}

func TestSecretsRunGivesTheCommandTheSecretAndOpTheToken(t *testing.T) {
	d := newSecretsDevice(t, `echo "gh=$GH_TOKEN token=${OP_SERVICE_ACCOUNT_TOKEN-unset} host=${GH_HOST-unset} exa=${EXA_API_KEY-unset} args=$*" > "$TMPDIR/gh-saw"`)
	err := secretsRun(&Globals{}, []string{"GITHUB_PERSONAL_ACCESS_TOKEN=GH_TOKEN"}, []string{"gh", "pr", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d.saw(t, "gh-saw"), "gh=resolved-for-GH_TOKEN token=unset host=unset exa=unset args=pr list"; got != want {
		t.Errorf("gh saw %q, want %q: only the secret it named, no token, nothing it could be steered by", got, want)
	}
	if got := d.saw(t, "op-saw-token"); got != secretsTestToken {
		t.Errorf("op was started with %q, want the token", got)
	}
	evs := d.events(t)
	if len(evs) != 2 || evs[0].Action != "run" || evs[1].Action != "exit" {
		t.Fatalf("audit events: %+v, want the run recorded before the command and its exit after", evs)
	}
	for _, e := range evs {
		if e.Source != audit.SourceSecrets || e.Kind != audit.KindSecretRun || e.Decision != "allowed" || e.Target != "gh pr" {
			t.Errorf("audit event %+v", e)
		}
	}
	raw, _ := os.ReadFile(d.p.Audit())
	for _, leak := range []string{secretsTestToken, "resolved-for", "op://"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the audit log holds %q: %s", leak, raw)
		}
	}
}

func TestSecretsRunPassesTheCommandsExitCodeThrough(t *testing.T) {
	d := newSecretsDevice(t, `exit 7`)
	err := secretsRun(&Globals{}, []string{"EXA_API_KEY"}, []string{"gh", "run", "list"})
	var ce *commandExitError
	if !errors.As(err, &ce) || ce.code != 7 {
		t.Fatalf("err = %v, want exit code 7", err)
	}
	if evs := d.events(t); len(evs) != 2 || evs[1].Detail["exit"] != float64(7) {
		t.Errorf("audit events: %+v", evs)
	}
}

func TestSecretsRunRefusesAndRecordsWithoutStartingOp(t *testing.T) {
	d := newSecretsDevice(t, `echo ran > "$TMPDIR/gh-saw"`)
	for name, tc := range map[string]struct {
		specs, argv []string
		want        string
	}{
		"a shell":         {[]string{"EXA_API_KEY"}, []string{"sh", "-c", "echo $EXA_API_KEY"}, "can't run with secrets"},
		"printenv":        {[]string{"EXA_API_KEY"}, []string{"printenv", "EXA_API_KEY"}, "can't run with secrets"},
		"a merge":         {[]string{"EXA_API_KEY"}, []string{"gh", "pr", "merge", "5"}, "Jason approves"},
		"a write":         {[]string{"EXA_API_KEY"}, []string{"gh", "api", "-X", "POST", "repos/x/y/issues"}, "reads only"},
		"an unknown name": {[]string{"NOPE"}, []string{"gh", "pr", "list"}, "EXA_API_KEY, GITHUB_PERSONAL_ACCESS_TOKEN"},
		"a reserved name": {[]string{"EXA_API_KEY=OP_SERVICE_ACCOUNT_TOKEN"}, []string{"gh", "pr", "list"}, "can't be put in"},
	} {
		err := secretsRun(&Globals{}, tc.specs, tc.argv)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, tc.want)
		}
	}
	if _, err := os.Stat(filepath.Join(d.scratch, "op-saw-token")); err == nil {
		t.Error("op was started for a refused request")
	}
	evs := d.events(t)
	if len(evs) != 6 {
		t.Fatalf("%d audit events, want one per refusal: %+v", len(evs), evs)
	}
	for _, e := range evs {
		if e.Decision != "refused" {
			t.Errorf("event %+v isn't a refusal", e)
		}
	}
}

func TestSecretsRunNeedsASecretAndACommand(t *testing.T) {
	newSecretsDevice(t, "true")
	for _, tc := range []struct{ specs, argv []string }{
		{nil, []string{"gh", "pr", "list"}},
		{[]string{"EXA_API_KEY"}, nil},
	} {
		var ue *UsageError
		if err := secretsRun(&Globals{}, tc.specs, tc.argv); !errors.As(err, &ue) {
			t.Errorf("secretsRun(%v, %v) = %v, want a usage error", tc.specs, tc.argv, err)
		}
	}
}

func TestSecretsRunWithoutATokenSaysHowToAddOne(t *testing.T) {
	d := newSecretsDevice(t, "true")
	secretsTokenFinder = func(p *paths.Paths) optoken.Finder {
		return optoken.Finder{GOOS: "linux", Getenv: func(string) string { return "" }, File: p.OpTokenFile()}
	}
	err := secretsRun(&Globals{}, []string{"EXA_API_KEY"}, []string{"gh", "pr", "list"})
	if err == nil || !errors.Is(err, optoken.ErrNone) || !strings.Contains(err.Error(), d.p.OpTokenFile()) {
		t.Errorf("err = %v, want ErrNone naming the token file", err)
	}
}

func TestSecretsRunStopsWhenTheAuditLogWontOpen(t *testing.T) {
	d := newSecretsDevice(t, `echo ran > "$TMPDIR/gh-saw"`)
	if err := os.Mkdir(d.p.Audit(), 0o700); err != nil { // a directory where the log should be
		t.Fatal(err)
	}
	err := secretsRun(&Globals{}, []string{"EXA_API_KEY"}, []string{"gh", "pr", "list"})
	if err == nil || !strings.Contains(err.Error(), "audit log") {
		t.Fatalf("err = %v, want the audit log named", err)
	}
	if _, err := os.Stat(filepath.Join(d.scratch, "gh-saw")); err == nil {
		t.Error("the command ran with no record of it")
	}
}

func TestSecretsRunNeverRecordsTheTextOfARequest(t *testing.T) {
	d := newSecretsDevice(t, `true`)
	// a session can type any value it holds into a spec or a command
	for _, tc := range []struct{ specs, argv []string }{
		{[]string{"EXA_API_KEY=ghp_typed-by-the-session"}, []string{"gh", "pr", "list"}},
		{[]string{"ghp_typed-by-the-session"}, []string{"gh", "pr", "list"}},
		{[]string{"EXA_API_KEY"}, []string{"ghp_typed-by-the-session", "x"}},
		{[]string{"ghp_typed-by-the-session"}, []string{"sh", "-c", "ghp_typed-by-the-session"}},
		{[]string{"EXA_API_KEY"}, []string{"gh", "pr", "view", "--repo", "ghp_typed-by-the-session/a/b"}},
	} {
		if err := secretsRun(&Globals{}, tc.specs, tc.argv); err == nil {
			t.Errorf("secretsRun(%q, %q) was accepted", tc.specs, tc.argv)
		}
	}
	raw, _ := os.ReadFile(d.p.Audit())
	if strings.Contains(string(raw), "typed-by-the-session") || len(d.events(t)) != 5 {
		t.Errorf("the audit log holds what the caller typed, or lost a refusal: %s", raw)
	}
}

func TestSecretsRunIgnoresTheSessionsPathAndProxies(t *testing.T) {
	d := newSecretsDevice(t, `echo "path=$PATH proxy=${HTTPS_PROXY-unset}" > "$TMPDIR/gh-saw"`)
	// a fake op in a directory the session controls must not get the token
	decoy := t.TempDir()
	if err := os.WriteFile(filepath.Join(decoy, "op"), []byte("#!/bin/sh\necho stolen > \"$TMPDIR/stolen\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", decoy+":/usr/bin:/bin")
	t.Setenv("HTTPS_PROXY", "http://evil.example:3128")
	if err := secretsRun(&Globals{}, []string{"EXA_API_KEY"}, []string{"gh", "pr", "list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.scratch, "stolen")); err == nil {
		t.Fatal("the op on the session's PATH was started")
	}
	want := "path=" + strings.Join(secretrun.TrustedDirs, ":") + " proxy=unset"
	if got := d.saw(t, "gh-saw"); got != want {
		t.Errorf("gh saw %q, want %q", got, want)
	}
}

func TestSecretsRunStopsWhenTheAuditLogCantBeWrittenAfterItOpened(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to any file, so there's no log it can't write")
	}
	d := newSecretsDevice(t, `echo ran > "$TMPDIR/gh-saw"`)
	orig := openAuditLog
	t.Cleanup(func() { openAuditLog = orig })
	openAuditLog = func(path string) (*audit.Log, error) {
		l, err := audit.Open(path)
		if err == nil {
			err = os.Chmod(path, 0o400) // it opened; now it can't be appended to
		}

		return l, err
	}
	err := secretsRun(&Globals{}, []string{"EXA_API_KEY"}, []string{"gh", "pr", "list"})
	if err == nil || !strings.Contains(err.Error(), "audit log") {
		t.Fatalf("err = %v, want the audit log named", err)
	}
	if _, err := os.Stat(filepath.Join(d.scratch, "gh-saw")); err == nil {
		t.Error("the command ran with no record of it")
	}
}
