package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/optoken"
	"jgarner.dev/bud/internal/paths"
)

const testToken = "ops_doctor-test-token"

// serviceAccountSetup gives a throwaway device with an opencode.env, a token
// file that holds testToken, and a fake vault list
func serviceAccountSetup(t *testing.T, tokenFile string, vaults func(context.Context, optoken.Token) ([]string, error)) *paths.Paths {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(optoken.EnvVar, "")
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.OpenCodeEnv(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if tokenFile != "" {
		if err := os.MkdirAll(filepath.Dir(p.OpTokenFile()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.OpTokenFile(), []byte(tokenFile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldF, oldV := serviceAccountFinder, serviceAccountVaults
	t.Cleanup(func() { serviceAccountFinder, serviceAccountVaults = oldF, oldV })
	serviceAccountFinder = func(p *paths.Paths) optoken.Finder { return optoken.Finder{GOOS: "linux", File: p.OpTokenFile()} }
	serviceAccountVaults = vaults

	return p
}

func noVaults(context.Context, optoken.Token) ([]string, error) {
	return nil, errors.New("op must not run without --net")
}

func TestServiceAccountCheckHasNothingToSayWithoutOpencodeEnv(t *testing.T) {
	p := serviceAccountSetup(t, "", noVaults)
	if err := os.Remove(p.OpenCodeEnv()); err != nil {
		t.Fatal(err)
	}
	if _, ok := checkServiceAccount(p, Options{Net: true}); ok {
		t.Error("a device that never launches through op run got a check")
	}
}

func TestServiceAccountCheckNamesTheSourceAndStaysOfflineWithoutNet(t *testing.T) {
	p := serviceAccountSetup(t, optoken.EnvVar+"="+testToken+"\n", noVaults)
	r, ok := checkServiceAccount(p, Options{})
	if !ok || r.Status != OK || !strings.Contains(r.Detail, "token file") {
		t.Fatalf("got %+v, %v", r, ok)
	}
	if strings.Contains(r.Detail+r.Fix, testToken) {
		t.Errorf("the token is in the result: %+v", r)
	}
}

func TestServiceAccountCheckWarnsWithNoToken(t *testing.T) {
	p := serviceAccountSetup(t, "", noVaults)
	r, ok := checkServiceAccount(p, Options{Net: true})
	if !ok || r.Status != Warn || !strings.Contains(r.Fix, optoken.EnvVar) || !strings.Contains(r.Fix, p.OpTokenFile()) {
		t.Errorf("got %+v, %v; want a warning that says where to put the token", r, ok)
	}
}

func TestServiceAccountCheckWarnsWhenTheTokenFileSetsNoToken(t *testing.T) {
	p := serviceAccountSetup(t, "OTHER=1\n", noVaults)
	if r, _ := checkServiceAccount(p, Options{}); r.Status != Warn || !strings.Contains(r.Detail, "doesn't set") {
		t.Errorf("got %+v", r)
	}
}

func TestServiceAccountCheckWithNetWantsExactlyOneVault(t *testing.T) {
	var got string
	vaults := func(want []string, err error) func(context.Context, optoken.Token) ([]string, error) {
		return func(_ context.Context, tok optoken.Token) ([]string, error) {
			got = tok.Value()
			return want, err
		}
	}
	file := optoken.EnvVar + "=" + testToken + "\n"

	p := serviceAccountSetup(t, file, vaults([]string{"Bud Vault"}, nil))
	r, _ := checkServiceAccount(p, Options{Net: true})
	if r.Status != OK || !strings.Contains(r.Detail, "Bud Vault") {
		t.Errorf("one vault: %+v", r)
	}
	if got != testToken {
		t.Errorf("op was given %q, want the token file's token", got)
	}

	p = serviceAccountSetup(t, file, vaults([]string{"Bud Vault", "Personal"}, nil))
	r, _ = checkServiceAccount(p, Options{Net: true})
	if r.Status != Warn || !strings.Contains(r.Detail, "2 vaults") || !strings.Contains(r.Detail, "Personal") || r.Fix == "" {
		t.Errorf("two vaults: %+v", r)
	}

	p = serviceAccountSetup(t, file, vaults(nil, errors.New("[ERROR] 403 Service Account Deleted")))
	r, _ = checkServiceAccount(p, Options{Net: true})
	if r.Status != Warn || !strings.Contains(r.Detail, "Service Account Deleted") || strings.Contains(r.Detail+r.Fix, testToken) {
		t.Errorf("failed sign-in: %+v", r)
	}

	p = serviceAccountSetup(t, file, vaults([]string{}, nil))
	if r, _ = checkServiceAccount(p, Options{Net: true}); r.Status != Warn {
		t.Errorf("no vaults: %+v", r)
	}
}

func fakeOp(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "op"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestOpVaultNamesGivesOpTheTokenAndReadsTheNames(t *testing.T) {
	fakeOp(t, `[ "$1 $2 $3 $4" = "vault list --format json" ] && [ "$OP_SERVICE_ACCOUNT_TOKEN" = "`+testToken+`" ] && [ -z "$OP_CONNECT_TOKEN" ] || exit 9
echo '[{"id":"a","name":"Bud Vault"},{"id":"b","name":"Other"}]'`)
	t.Setenv("OP_CONNECT_TOKEN", "connect")
	tok, err := optoken.Finder{Getenv: func(string) string { return testToken }}.Lookup()
	if err != nil {
		t.Fatal(err)
	}
	got, err := opVaultNames(context.Background(), tok)
	if err != nil || strings.Join(got, ",") != "Bud Vault,Other" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestOpVaultNamesReportsOpsOwnMessageAndNothingElse(t *testing.T) {
	fakeOp(t, `echo "`+testToken+`"; echo '[ERROR] 403 Service Account Deleted' >&2; exit 1`)
	tok, _ := optoken.Finder{Getenv: func(string) string { return testToken }}.Lookup()
	_, err := opVaultNames(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "Service Account Deleted") || strings.Contains(err.Error(), testToken) {
		t.Errorf("err = %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := opVaultNames(context.Background(), tok); err == nil || !strings.Contains(err.Error(), "isn't installed") {
		t.Errorf("without op: %v", err)
	}
}

func TestServiceAccountCheckSaysWhenTheKeychainTokenWasCutShort(t *testing.T) {
	decodeErr := errors.New("failed to DecodeSACredentials: unexpected end of JSON input")
	fake := func(tokenLen int) string {
		sh := filepath.Join(t.TempDir(), "security")
		body := "#!/bin/sh\nprintf '%s\\n' \"$(head -c " + strconv.Itoa(tokenLen) + " /dev/zero | tr '\\0' 'x')\"\n"
		if err := os.WriteFile(sh, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}

		return sh
	}
	for _, tc := range []struct {
		name     string
		tokenLen int
		err      error
		hint     bool
	}{
		{"cut at the prompt's limit", 127, decodeErr, true},
		{"one past it was not cut", 128, decodeErr, false},
		{"a full token", 600, decodeErr, false},
		{"short, but op failed some other way", 127, errors.New("op isn't installed"), false},
		{"short, with a 403", 127, errors.New("[ERROR] (403) Forbidden"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := serviceAccountSetup(t, "", func(context.Context, optoken.Token) ([]string, error) { return nil, tc.err })
			serviceAccountFinder = func(*paths.Paths) optoken.Finder {
				return optoken.Finder{GOOS: "darwin", Getenv: func(string) string { return "" }, Security: fake(tc.tokenLen)}
			}
			r, ok := checkServiceAccount(p, Options{Net: true})
			if !ok || r.Status != Warn {
				t.Fatalf("got %+v", r)
			}
			cut := strings.Contains(r.Fix, "cut short") && strings.Contains(r.Fix, optoken.KeychainAddCommand)
			if cut != tc.hint || strings.Contains(r.Fix+r.Detail, "xxxx") {
				t.Errorf("fix %q, want the cut-off hint %v and never the token", r.Fix, tc.hint)
			}
			if !tc.hint && !strings.Contains(r.Fix, "check the token is current") {
				t.Errorf("fix %q, want the general advice", r.Fix)
			}
		})
	}
}
