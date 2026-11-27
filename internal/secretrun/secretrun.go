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
// rest (GH_HOST, which could point gh's token at another host, among it) is
// dropped, so a session can't steer a secret by what it exports
var passEnv = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "TERM", "TMPDIR", "TZ", "NO_COLOR",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
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
	if len(argv) >= 3 && slices.Contains(refusedGH, argv[1]+" "+argv[2]) {
		return fmt.Errorf("%q can't run with secrets: it changes something Jason approves", argv[1]+" "+argv[2])
	}
	if argv[1] == "api" {
		return checkGHAPI(argv[2:])
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

// Build makes the plan. op, env and command are the paths of those tools, the
// command being argv[0] found on PATH; environ is the caller's environment, of
// which only passEnv carries over. Nothing in the plan's Argv is secret; the
// token is in Env alone, and a reference is not a value
func Build(op, env, command string, secrets []Secret, argv []string, environ []string, tok optoken.Token) (Plan, error) {
	if err := Check(argv); err != nil {
		return Plan{}, err
	}
	var penv []string
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if slices.Contains(passEnv, k) {
			penv = append(penv, kv)
		}
	}
	for _, kv := range penv {
		if k, v, _ := strings.Cut(kv, "="); k == "PATH" {
			for _, dir := range strings.Split(v, ":") {
				if !strings.HasPrefix(dir, "/") {
					return Plan{}, fmt.Errorf("PATH has %q, which isn't an absolute directory; a secret won't go to a command found there", dir)
				}
			}
		}
	}
	for _, s := range secrets {
		penv = append(penv, s.As+"="+s.Ref)
	}
	penv = append(penv, optoken.EnvVar+"="+tok.Value())
	run := []string{op, "run", "--", env, "-u", optoken.EnvVar}

	return Plan{Argv: append(append(run, command), argv[1:]...), Env: penv}, nil
}
