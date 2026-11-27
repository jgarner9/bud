package optoken

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secret = "ops_sekrit-token-value"

// fakeSecurity writes a stand-in for macOS's security tool that runs body
func fakeSecurity(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "security")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	return p
}

func tokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "op-service-account.env")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func noEnv(string) string { return "" }

func TestTheEnvironmentWinsOverTheKeychainAndTheFile(t *testing.T) {
	f := Finder{
		Getenv:   func(k string) string { return map[string]string{EnvVar: " " + secret + "\n"}[k] },
		GOOS:     "darwin",
		Security: fakeSecurity(t, "echo from-keychain"),
		File:     tokenFile(t, EnvVar+"=from-file\n"),
	}
	tok, err := f.Lookup()
	if err != nil || tok.Value() != secret || tok.Source != FromEnvironment {
		t.Fatalf("Lookup = %v, %v; want the environment's token", tok, err)
	}
}

func TestTheKeychainComesBeforeTheFileOnAMac(t *testing.T) {
	f := Finder{
		Getenv:   noEnv,
		GOOS:     "darwin",
		Security: fakeSecurity(t, `[ "$1 $2 $3 $4" = "find-generic-password -s `+KeychainService+` -w" ] || exit 9; echo `+secret),
		File:     tokenFile(t, EnvVar+"=from-file\n"),
	}
	tok, err := f.Lookup()
	if err != nil || tok.Value() != secret || tok.Source != FromKeychain {
		t.Fatalf("Lookup = %v, %v; want the Keychain's token", tok, err)
	}
}

func TestAMissingKeychainItemFallsBackToTheFile(t *testing.T) {
	f := Finder{
		Getenv:   noEnv,
		GOOS:     "darwin",
		Security: fakeSecurity(t, "echo 'could not be found' >&2; exit 44"),
		File:     tokenFile(t, "export "+EnvVar+"=\""+secret+"\"\n"),
	}
	tok, err := f.Lookup()
	if err != nil || tok.Value() != secret || tok.Source != FromFile {
		t.Fatalf("Lookup = %v, %v; want the file's token", tok, err)
	}
}

func TestTheKeychainIsLeftAloneOffAMac(t *testing.T) {
	f := Finder{
		Getenv:   noEnv,
		GOOS:     "linux",
		Security: fakeSecurity(t, "echo from-keychain"),
		File:     tokenFile(t, EnvVar+"="+secret+"\n"),
	}
	tok, err := f.Lookup()
	if err != nil || tok.Source != FromFile {
		t.Fatalf("Lookup = %v, %v; want the file's token", tok, err)
	}
}

func TestABrokenKeychainStopsTheLookupWithoutTheValue(t *testing.T) {
	// the tool prints a token on stdout and fails: the error says what the
	// tool said on stderr, never the output
	f := Finder{
		Getenv:   noEnv,
		GOOS:     "darwin",
		Security: fakeSecurity(t, "echo "+secret+"; echo 'User interaction is not allowed.' >&2; exit 36"),
		File:     tokenFile(t, EnvVar+"=from-file\n"),
	}
	_, err := f.Lookup()
	if err == nil || !strings.Contains(err.Error(), "User interaction is not allowed") {
		t.Fatalf("err = %v, want the tool's own message", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error holds the token: %v", err)
	}
}

func TestAnEmptyKeychainItemSaysHowToAddItAgain(t *testing.T) {
	f := Finder{Getenv: noEnv, GOOS: "darwin", Security: fakeSecurity(t, "echo")}
	_, err := f.Lookup()
	if err == nil || !strings.Contains(err.Error(), "security add-generic-password") {
		t.Fatalf("err = %v, want the add command", err)
	}
}

func TestAFileThatDoesNotSetTheTokenIsAnError(t *testing.T) {
	f := Finder{Getenv: noEnv, GOOS: "linux", File: tokenFile(t, "OTHER=1\n")}
	_, err := f.Lookup()
	if err == nil || errors.Is(err, ErrNone) || !strings.Contains(err.Error(), "doesn't set "+EnvVar) {
		t.Fatalf("err = %v, want the file named as not setting the token", err)
	}
}

func TestNoTokenAnywhereSaysWhereToPutOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "none.env")
	for goos, wantKeychain := range map[string]bool{"darwin": true, "linux": false} {
		f := Finder{Getenv: noEnv, GOOS: goos, Security: fakeSecurity(t, "exit 44"), File: file}
		_, err := f.Lookup()
		if !errors.Is(err, ErrNone) {
			t.Fatalf("%s: err = %v, want ErrNone", goos, err)
		}
		msg := err.Error()
		if !strings.Contains(msg, EnvVar) || !strings.Contains(msg, file) {
			t.Errorf("%s: %q doesn't name the variable and the file", goos, msg)
		}
		if strings.Contains(msg, "Keychain") != wantKeychain {
			t.Errorf("%s: %q, Keychain mention should be %v", goos, msg, wantKeychain)
		}
	}
}

func TestATokenFormatsAsItsSourceNeverItsValue(t *testing.T) {
	tok := Token{value: secret, Source: FromKeychain, Detail: KeychainService}
	for _, got := range []string{
		fmt.Sprint(tok), fmt.Sprintf("%v", tok), fmt.Sprintf("%+v", tok), fmt.Sprintf("%#v", tok), fmt.Sprintf("%s", tok),
	} {
		if strings.Contains(got, secret) {
			t.Errorf("a format printed the token: %q", got)
		}
	}
	if got := tok.String(); !strings.Contains(got, FromKeychain) || !strings.Contains(got, KeychainService) {
		t.Errorf("String() = %q, want the source named", got)
	}
}
