package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"jgarner.dev/bud/internal/envfile"
	"jgarner.dev/bud/internal/optoken"
	"jgarner.dev/bud/internal/paths"
)

// swappable in tests, so no check reaches a real Keychain or 1Password
var (
	serviceAccountFinder = func(p *paths.Paths) optoken.Finder { return optoken.Finder{File: p.OpTokenFile()} }
	serviceAccountVaults = opVaultNames
)

// checkServiceAccount says where bud finds the 1Password service account
// token (Suite D144), and with --net whether it signs in and sees only the one
// vault it's meant to (D146: the vault is the boundary). It's only ever a
// warning: a missing token leaves op signing in as it did, and a Fail would
// roll back a bud update over it. A device that never launches OpenCode
// through op run has no opencode.env, and nothing to say
func checkServiceAccount(p *paths.Paths, opts Options) (Result, bool) {
	if _, err := os.Stat(p.OpenCodeEnv()); err != nil {
		return Result{}, false
	}
	r := Result{Name: "1password service account"}
	tok, err := serviceAccountFinder(p).Lookup()
	switch {
	case errors.Is(err, optoken.ErrNone):
		r.Status = Warn
		r.Detail = "no token: bud agent relies on op's own sign-in"
		r.Fix = strings.TrimPrefix(err.Error(), optoken.ErrNone.Error()+": ")
		return r, true
	case err != nil:
		r.Status = Warn
		r.Detail = err.Error()
		return r, true
	}
	r.Detail = tok.String()
	if !opts.Net {
		return r, true
	}

	ctx, cancel := opts.netContext()
	defer cancel()
	vaults, err := serviceAccountVaults(ctx, tok)
	switch {
	case err != nil:
		r.Status = Warn
		r.Detail += "; couldn't sign in with it: " + err.Error()
		r.Fix = "check the token is current: a rotated or deleted service account fails with a 403"
		if tok.Source == optoken.FromKeychain && tok.Len() <= optoken.KeychainPromptLimit+1 {
			// security's -w prompt keeps 127 characters; a token is several hundred
			r.Fix = fmt.Sprintf("the Keychain token is only %d characters, so it was probably cut short by security's prompt: copy the token from 1Password, then run %s and pbcopy < /dev/null", tok.Len(), optoken.KeychainAddCommand)
		}
	case len(vaults) == 1:
		r.Detail += "; signs in and sees one vault, " + vaults[0]
	default:
		r.Status = Warn
		r.Detail += fmt.Sprintf("; signs in and sees %d vaults: %s", len(vaults), strings.Join(vaults, ", "))
		r.Fix = "make a service account scoped to the Bud vault alone and replace the token"
	}

	return r, true
}

// opVaultNames lists the vaults the token's service account can see, with
// op vault list. op gets the token in its environment alone, and what it
// prints is vault names; an error carries only op's own message
func opVaultNames(ctx context.Context, tok optoken.Token) ([]string, error) {
	op, err := exec.LookPath("op")
	if err != nil {
		return nil, errors.New("op isn't installed")
	}
	cmd := exec.CommandContext(ctx, op, "vault", "list", "--format", "json")
	cmd.Env = envfile.With(envfile.Without(envfile.Without(os.Environ(), "OP_CONNECT_HOST"), "OP_CONNECT_TOKEN"), optoken.EnvVar, tok.Value())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, err
	}
	var vaults []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &vaults); err != nil {
		return nil, fmt.Errorf("read op's vault list: %w", err)
	}
	names := make([]string, 0, len(vaults))
	for _, v := range vaults {
		names = append(names, v.Name)
	}

	return names, nil
}
