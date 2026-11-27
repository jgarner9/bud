package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"jgarner.dev/bud/internal/audit"
	"jgarner.dev/bud/internal/envfile"
	"jgarner.dev/bud/internal/optoken"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/secretrun"
)

func init() { RegisterCommand(Secrets()) }

// Secrets is where a session gets a command run with a vault secret
// (Suite D142, D145); see docs/internals/secrets.md
func Secrets() *Command {
	var specs stringList
	return &Command{
		Name:  "secrets",
		Usage: "Run a command with secrets from the Bud vault, without the session seeing the values",
		Subs: []*Command{{
			Name:  "run",
			Usage: "run --secret NAME[=AS] [--secret ...] -- gh pr list: run an allowed command with the synced secrets it names",
			Flags: func(fs *flag.FlagSet) {
				fs.Var(&specs, "secret", "a synced variable to give the command, NAME or NAME=AS to give it as AS (repeatable)")
			},
			Run: func(g *Globals, args []string) error { return secretsRun(g, specs, args) },
		}},
	}
}

// stringList is a repeatable string flag
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// secretsTokenFinder is where bud secrets run looks for the service account
// token; swappable so tests never reach a real Keychain
var secretsTokenFinder = func(p *paths.Paths) optoken.Finder { return optoken.Finder{File: p.OpTokenFile()} }

// openAuditLog is swappable so a test can have a log that opens and then
// can't be written
var openAuditLog = audit.Open

// secretsRun checks the request, starts the command under op run with only
// the secrets it named, and records the call. Every refusal and every run is
// in the audit log, with names and exit codes and never a value. A log that
// won't open stops the command: nothing gets a secret unrecorded
func secretsRun(g *Globals, specs, argv []string) error {
	if len(specs) == 0 {
		return usageErrorf("secrets run: name at least one secret with --secret NAME")
	}
	if len(argv) == 0 {
		return usageErrorf("secrets run: put the command after --, such as: bud secrets run --secret NAME -- gh pr list")
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	log, err := openAuditLog(p.Audit())
	if err != nil {
		return fmt.Errorf("secrets run: no secret goes to a command the audit log can't record: %w", err)
	}

	// names is what the log holds of the request: how many secrets were asked
	// for until they've been checked, then the names of those that passed. The
	// text of a spec is never recorded, since a session can type anything there
	var names []string
	event := func(action, decision string, detail map[string]any) audit.Event {
		if detail == nil {
			detail = map[string]any{}
		}
		detail["requested"] = len(specs)
		if len(names) > 0 {
			detail["secrets"] = names
		}

		return audit.Event{
			Source: audit.SourceSecrets, Kind: audit.KindSecretRun, Action: action,
			Target: secretrun.Target(argv), Decision: decision, Detail: detail,
		}
	}
	// refuse records the refusal under a fixed reason (the error can quote
	// what the caller typed) and returns the error, which only the caller sees
	refuse := func(reason string, err error) error {
		log.Record(event("run", "refused", map[string]any{"reason": reason}))
		return fmt.Errorf("secrets run: %w", err)
	}

	if err := secretrun.Check(argv); err != nil {
		return refuse("command not allowed", err)
	}
	pairs, err := envfile.Read(p.OpenCodeEnv())
	if err != nil {
		return refuse("no synced secrets", fmt.Errorf("no secrets are synced to this device: bud sync writes opencode.env: %w", err))
	}
	refs := map[string]string{}
	for _, kv := range pairs {
		refs[kv[0]] = kv[1]
	}
	secrets, err := secretrun.ParseSecrets(specs, refs)
	if err != nil {
		return refuse("secret request refused", err)
	}
	for _, s := range secrets {
		names = append(names, s.Name)
	}

	tok, err := secretsTokenFinder(p).Lookup()
	if err != nil {
		return refuse("no token", err)
	}
	var bins [3]string
	for i, name := range []string{"op", "env", argv[0]} {
		if bins[i], err = secretrun.Lookup(name); err != nil {
			return refuse("no trusted "+name, err)
		}
	}
	plan, err := secretrun.Build(bins[0], bins[1], bins[2], secrets, argv, os.Environ(), tok)
	if err != nil {
		return refuse("plan refused", err)
	}

	// The run is recorded before the command starts, and a record that can't
	// be written stops it: log.Record drops write errors, and a secret that
	// went out unrecorded is the one case this log exists to prevent
	if err := log.Write(event("run", "allowed", nil)); err != nil {
		return fmt.Errorf("secrets run: no secret goes to a command the audit log can't record: %w", err)
	}
	g.Logf("running %s with %s", headOf(argv), strings.Join(names, ", "))
	cmd := exec.Command(plan.Argv[0], plan.Argv[1:]...)
	cmd.Env = plan.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		log.Record(event("exit", "allowed", map[string]any{"exit": 0}))
		return nil
	case errors.As(err, &ee):
		code := max(ee.ExitCode(), ExitFailure) // -1 when a signal ended it
		log.Record(event("exit", "allowed", map[string]any{"exit": code}))
		return &commandExitError{code: code}
	}
	log.Record(event("exit", "allowed", map[string]any{"error": "op couldn't start"}))

	return fmt.Errorf("secrets run: start op: %w", err)
}

// headOf is the command and its subcommand, which is all the audit log keeps
// of what ran: the rest are arguments
func headOf(argv []string) string {
	if len(argv) > 2 {
		argv = argv[:2]
	}

	return strings.Join(argv, " ")
}
