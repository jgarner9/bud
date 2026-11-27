// Package secretrun is what bud secrets run is made of (Suite D142, D145): the
// checks on which secrets and which commands a session may ask for, and the
// op run command that gives one command those secrets and nothing else.
//
// The session never holds the service account token. The caller gives it to
// op run's own environment only, and the command runs under env -u, so it
// holds the secrets it asked for and not the token that fetched them.
package secretrun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"jgarner.dev/bud/internal/optoken"
)

// Allow is the commands a session may run with secrets, as argv prefixes: a
// command is allowed when its first words are one of these exactly. Anything
// else is refused, shells and interpreters among it, because they can print
// what they were given. Adding a command is a change to this list (Suite D145)
var Allow = [][]string{
	{"gh", "pr"},
	{"gh", "issue"},
	{"gh", "api"},
	{"gh", "run"},
}

// refusedGH are gh subcommands that are allowed by prefix but change or
// destroy something that the base rules ask Jason about, so bud secrets run
// can't be a way around that
var refusedGH = []string{"pr merge", "issue delete", "run delete"}

// passEnv is the only part of the caller's environment a command keeps. The
// rest (GH_HOST and the proxy variables, which could send gh's token to
// another host, among it) is dropped, so a session can't steer a secret by
// what it exports. PATH is not here: the command gets TrustedDirs
var passEnv = []string{
	"HOME", "USER", "LOGNAME", "LANG", "TERM", "TMPDIR", "TZ", "NO_COLOR",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
}

// steering are variables that point a command somewhere else or change what
// it trusts, so no secret may be given under one of these names
var steering = []string{
	"PATH", "GH_HOST", "GH_REPO", "GH_CONFIG_DIR", "GH_PATH", "GH_BROWSER", "GH_EDITOR", "GH_PAGER",
	"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "SSL_CERT_FILE", "SSL_CERT_DIR", "GODEBUG", "GOFLAGS",
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
}

// TrustedDirs is where op, env and the allowed command are found, and the
// PATH the command gets. The session's own PATH is never used: a fake op in a
// directory the session controls would be handed the service account token
var TrustedDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/home/linuxbrew/.linuxbrew/bin"} // leak-check: allow (Linuxbrew's own directory)

// Lookup finds name in TrustedDirs. A file anyone can write to is refused
func Lookup(name string) (string, error) {
	if strings.Contains(name, "/") {
		return "", fmt.Errorf("%q is a path; name the command", name)
	}
	for _, dir := range TrustedDirs {
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
			continue
		}
		if fi.Mode().Perm()&0o002 != 0 {
			return "", fmt.Errorf("%s can be written by anyone, so it won't be given a secret", p)
		}

		return p, nil
	}

	return "", fmt.Errorf("no %s in %s", name, strings.Join(TrustedDirs, ", "))
}

var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Secret is one secret a command asks for: the synced variable it reads, and
// the variable the command sees it as
type Secret struct {
	Name string // the variable in opencode.env
	As   string // the variable the command gets; Name when no rename was asked for
	Ref  string // the op:// reference; never a value
}

// ParseSecrets turns --secret specs, NAME or NAME=AS, into secrets, looking
// each NAME up in refs (opencode.env: variable to op:// reference). Errors
// list names, never references or values
func ParseSecrets(specs []string, refs map[string]string) ([]Secret, error) {
	if len(specs) == 0 {
		return nil, errors.New("name at least one secret with --secret NAME")
	}
	seen := map[string]bool{}
	var out []Secret
	for _, spec := range specs {
		name, as, renamed := strings.Cut(spec, "=")
		if !renamed {
			as = name
		}
		ref, ok := refs[name]
		if !ok || !strings.HasPrefix(ref, "op://") {
			return nil, fmt.Errorf("no secret %q is synced to this device; the names are %s", name, nameList(refs))
		}
		if !nameRE.MatchString(as) {
			return nil, fmt.Errorf("--secret %s: %q isn't a variable name", spec, as)
		}
		if bad := reservedName(as); bad != "" {
			return nil, fmt.Errorf("--secret %s: %q is %s, which a secret can't be put in", spec, as, bad)
		}
		if seen[as] {
			return nil, fmt.Errorf("--secret: %s is given twice", as)
		}
		seen[as] = true
		out = append(out, Secret{Name: name, As: as, Ref: ref})
	}

	return out, nil
}

func nameList(refs map[string]string) string {
	var names []string
	for k, v := range refs {
		if strings.HasPrefix(v, "op://") {
			names = append(names, k)
		}
	}
	if len(names) == 0 {
		return "none (bud sync writes opencode.env)"
	}
	sort.Strings(names)

	return strings.Join(names, ", ")
}

// reservedName says why a variable can't hold a secret: it steers op, the
// dynamic linker or the command's environment, or it's one bud passes through
func reservedName(as string) string {
	switch {
	case strings.HasPrefix(as, "OP_"):
		return "one of op's own settings"
	case strings.HasPrefix(as, "LD_"), strings.HasPrefix(as, "DYLD_"):
		return "a dynamic linker setting"
	case slices.Contains(passEnv, as):
		return "part of the environment bud passes through"
	case slices.Contains(steering, as):
		return "a setting that points a command somewhere else"
	}

	return ""
}

// Check says whether argv may run with secrets: it has to start with an
// allowed prefix, name the command rather than a path, and not be one of the
// subcommands that change or delete things. gh api is read-only
func Check(argv []string) error {
	if len(argv) == 0 {
		return errors.New("name a command after --")
	}
	if strings.Contains(argv[0], "/") {
		return fmt.Errorf("%q is a path; name the command, such as gh", argv[0])
	}
	if !slices.ContainsFunc(Allow, func(p []string) bool { return len(argv) >= len(p) && slices.Equal(argv[:len(p)], p) }) {
		return fmt.Errorf("%q can't run with secrets; the commands that can are %s", head(argv), allowList())
	}
	if argv[0] != "gh" {
		return nil
	}
	for _, a := range argv[1:] {
		if strings.Contains(a, "://") {
			return errors.New("gh can run with secrets against github.com only: name the path or the number, not a URL")
		}
	}
	if argv[1] == "api" {
		return checkGHAPI(argv[2:])
	}
	if len(argv) >= 3 {
		sub := argv[2]
		if strings.HasPrefix(sub, "-") && sub != "-h" && sub != "--help" {
			return fmt.Errorf("put the subcommand right after gh %s: flags come after it", argv[1])
		}
		if slices.Contains(refusedGH, argv[1]+" "+sub) {
			return fmt.Errorf("%q can't run with secrets: it changes something Jason approves", argv[1]+" "+sub)
		}
	}

	return checkRepoFlags(argv[2:])
}

// checkRepoFlags refuses a -R/--repo that names a host (HOST/OWNER/REPO):
// gh would take the call to that host
func checkRepoFlags(args []string) error {
	for i, a := range args {
		var v string
		switch {
		case (a == "-R" || a == "--repo") && i+1 < len(args):
			v = args[i+1]
		case strings.HasPrefix(a, "--repo="):
			v = strings.TrimPrefix(a, "--repo=")
		case strings.HasPrefix(a, "-R") && len(a) > 2:
			v = a[2:]
		}
		if strings.Count(v, "/") >= 2 {
			return errors.New("gh can run with secrets against github.com only: --repo takes OWNER/REPO, without a host")
		}
	}

	return nil
}

// head is the words of argv worth showing in a message: the command and its
// subcommand, never the arguments after them
func head(argv []string) string {
	if len(argv) > 2 {
		argv = argv[:2]
	}

	return strings.Join(argv, " ")
}

func allowList() string {
	var out []string
	for _, p := range Allow {
		out = append(out, strings.Join(p, " "))
	}

	return strings.Join(out, ", ")
}

// checkGHAPI keeps gh api to reads: a request body or a method other than GET
// is a write, and a URL or --hostname points the call somewhere else
func checkGHAPI(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-X" || a == "--method":
			if i+1 >= len(args) || !strings.EqualFold(args[i+1], "GET") {
				return errors.New("gh api can run with secrets for reads only (method GET)")
			}
			i++
		case strings.HasPrefix(a, "--method="):
			if !strings.EqualFold(strings.TrimPrefix(a, "--method="), "GET") {
				return errors.New("gh api can run with secrets for reads only (method GET)")
			}
		case strings.HasPrefix(a, "-X"):
			if !strings.EqualFold(a[2:], "GET") {
				return errors.New("gh api can run with secrets for reads only (method GET)")
			}
		case a == "-f" || a == "-F" || a == "--field" || a == "--raw-field" || a == "--input",
			strings.HasPrefix(a, "-f"), strings.HasPrefix(a, "-F"),
			strings.HasPrefix(a, "--field="), strings.HasPrefix(a, "--raw-field="), strings.HasPrefix(a, "--input="):
			return errors.New("gh api can run with secrets for reads only: a request body makes it a write")
		case a == "--hostname" || strings.HasPrefix(a, "--hostname="):
			return errors.New("gh api can run with secrets against github.com only: --hostname is refused")
		case strings.Contains(a, "://"):
			return errors.New("gh api can run with secrets against github.com's own paths only: name the path, not a URL")
		}
	}

	return nil
}

// Plan is the op run process to start
type Plan struct {
	Argv []string // op run -- env -u OP_SERVICE_ACCOUNT_TOKEN command ...
	Env  []string // op's environment: the passed-through variables, the references and the token
}

// Build makes the plan. op, env and command are the paths of those tools
// (see Lookup); environ is the caller's environment, of which only passEnv
// carries over, and the command's PATH is TrustedDirs. Nothing in the plan's
// Argv is secret; the token is in Env alone, and a reference is not a value
func Build(op, env, command string, secrets []Secret, argv []string, environ []string, tok optoken.Token) (Plan, error) {
	if err := Check(argv); err != nil {
		return Plan{}, err
	}
	penv := []string{"PATH=" + strings.Join(TrustedDirs, ":")}
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		if !slices.Contains(passEnv, k) {
			continue
		}
		// op run resolves a reference in any variable it starts, which would
		// let a session ask for a secret that --secret never named
		if strings.Contains(v, "op://") {
			return Plan{}, fmt.Errorf("%s holds an op reference; a secret can only be asked for with --secret", k)
		}
		penv = append(penv, kv)
	}
	for _, s := range secrets {
		penv = append(penv, s.As+"="+s.Ref)
	}
	penv = append(penv, optoken.EnvVar+"="+tok.Value())
	run := []string{op, "run", "--", env, "-u", optoken.EnvVar}

	return Plan{Argv: append(append(run, command), argv[1:]...), Env: penv}, nil
}

// Target is the allowed command prefix argv starts with, such as "gh pr", for
// the audit log; a command that isn't allowed is "(not allowed)", so a word
// the caller typed never reaches the log
func Target(argv []string) string {
	for _, p := range Allow {
		if len(argv) >= len(p) && slices.Equal(argv[:len(p)], p) {
			return strings.Join(p, " ")
		}
	}

	return "(not allowed)"
}
