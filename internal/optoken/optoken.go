// Package optoken finds the 1Password service account token (Suite D142,
// D144). The token is never exported into a session's environment: a command
// that needs it asks here at the moment it starts an op process, and puts the
// value in that process's environment alone.
//
// The sources, in order: OP_SERVICE_ACCOUNT_TOKEN in this process's
// environment, the macOS Keychain item bud-op-service-account, and the token
// file the home server's units load. A token's value is never in an error and
// never printed: Token formats as its source.
package optoken

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"jgarner.dev/bud/internal/envfile"
)

// EnvVar is the variable op reads a service account token from
const EnvVar = "OP_SERVICE_ACCOUNT_TOKEN"

// KeychainService is the macOS Keychain item the token lives in
const KeychainService = "bud-op-service-account"

// ErrNone is what Lookup wraps when no source has a token, so a caller can
// carry on without one; any other error means a source is there but broken
var ErrNone = errors.New("no 1Password service account token")

// Where a token came from
const (
	FromEnvironment = "environment"
	FromKeychain    = "keychain"
	FromFile        = "token file"
)

// Token is a service account token and where it was found
type Token struct {
	value string
	// Source is one of FromEnvironment, FromKeychain and FromFile
	Source string
	// Detail names the file, or the Keychain item, it came from
	Detail string
}

// Value is the token itself, for the environment of an op process and
// nowhere else
func (t Token) Value() string { return t.value }

// String says where the token came from, never what it is, so a stray %v or
// %s in a log line can't leak it
func (t Token) String() string {
	if t.Detail == "" {
		return "service account token from the " + t.Source
	}

	return "service account token from the " + t.Source + " (" + t.Detail + ")"
}

// GoString keeps %#v from printing the value
func (t Token) GoString() string { return t.String() }

// Finder looks for the token. The zero value reads this process's
// environment and runtime.GOOS; tests and callers fill in what they need
type Finder struct {
	// Getenv reads the environment; os.Getenv when nil
	Getenv func(string) string
	// GOOS is the OS the Keychain applies to; runtime.GOOS when empty
	GOOS string
	// File is the token file, usually paths.OpTokenFile; none is tried when empty
	File string
	// Security is the macOS security tool; /usr/bin/security when empty
	Security string
}

// Lookup returns the token from the first source that has one. A source that
// has none is skipped; one that is there but broken (a file with no token in
// it, a locked Keychain) stops the lookup with an error that says what to fix
func (f Finder) Lookup() (Token, error) {
	getenv := f.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := strings.TrimSpace(getenv(EnvVar)); v != "" {
		return Token{value: v, Source: FromEnvironment, Detail: EnvVar}, nil
	}

	goos := f.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "darwin" {
		tok, found, err := f.keychain()
		if err != nil {
			return Token{}, err
		}
		if found {
			return tok, nil
		}
	}

	if f.File != "" {
		v, err := envfile.Value(f.File, EnvVar)
		switch {
		case err == nil:
			return Token{value: v, Source: FromFile, Detail: f.File}, nil
		case !errors.Is(err, os.ErrNotExist):
			return Token{}, fmt.Errorf("read the token file: %w", err)
		}
	}

	return Token{}, f.missing(goos)
}

// keychain reads the Keychain item. Not found is not an error; the item
// being there but unreadable (locked, access refused) is
func (f Finder) keychain() (Token, bool, error) {
	bin := f.Security
	if bin == "" {
		// the fixed path, not PATH: a session can export a PATH with its own
		// security first, and bud secrets run would take its answer for the token
		bin = "/usr/bin/security"
		if _, err := os.Stat(bin); err != nil {
			return Token{}, false, nil
		}
	}
	cmd := exec.Command(bin, "find-generic-password", "-s", KeychainService, "-w")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		// 44 is errSecItemNotFound
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return Token{}, false, nil
		}

		return Token{}, false, fmt.Errorf("read the %s Keychain item: %w%s", KeychainService, err, stderrOf(err))
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return Token{}, false, fmt.Errorf("the %s Keychain item is empty: add it again with %s", KeychainService, KeychainAddCommand)
	}

	return Token{value: v, Source: FromKeychain, Detail: KeychainService}, true, nil
}

// KeychainAddCommand adds the Keychain item from the clipboard: copy the token
// first, then run it. security's own -w prompt takes only 127 characters and a
// service account token is several hundred, so a token typed or pasted there is
// cut short and op fails to decode it. The token is in the command's arguments
// while it runs, and the clipboard is emptied after, even when the add fails
const KeychainAddCommand = `security add-generic-password -a "$USER" -s ` + KeychainService + ` -U -w "$(pbpaste)"; pbcopy < /dev/null`

// KeychainPromptLimit is the longest token security's -w prompt keeps
const KeychainPromptLimit = 127

// Len is the token's length, which says nothing about its value; doctor uses
// it to spot a token the Keychain prompt cut short
func (t Token) Len() int { return len(t.value) }

func (f Finder) missing(goos string) error {
	var how []string
	how = append(how, "set "+EnvVar)
	if goos == "darwin" {
		how = append(how, "add the Keychain item with "+KeychainAddCommand)
	}
	if f.File != "" {
		how = append(how, "put "+EnvVar+"=... in "+f.File+" (mode 0600)")
	}

	return fmt.Errorf("%w: %s", ErrNone, strings.Join(how, ", or "))
}

// stderrOf is what a failed command said, for the error; the security tool's
// messages name the item, never the value
func stderrOf(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return ": " + strings.TrimSpace(string(ee.Stderr))
	}

	return ""
}
