# Secrets for sessions: `bud secrets run`

How an OpenCode session on a device uses a Bud Vault secret without holding the 1Password service account token or being able to print what it fetched (Suite D142 to D148). The design is in `handoff/design/op-run-access.md`.

## The pieces

| Piece | Where | What it does |
| --- | --- | --- |
| `bud secrets run` | `internal/cli/secrets.go` | The one thing a session may run to use a secret. Checks the request, starts `op run`, records the call. |
| `secretrun` | `internal/secretrun` | The allowlist (`Allow`), the refusals, the name checks and the `op run` argv and environment. No I/O, so it is tested without `op`. |
| `optoken` | `internal/optoken` | Finds the service account token: `OP_SERVICE_ACCOUNT_TOKEN` in bud's environment, the macOS Keychain item `bud-op-service-account`, then `paths.OpTokenFile()`. |
| `envfile` | `internal/envfile` | Reads env files and edits environment lists. `bud resume` and `optoken` share it. |
| `bud agent` | `internal/cli/agent.go` | Gives `op run` the token from `optoken` so the launch needs no sign-in, and keeps it from OpenCode with `env -u`. |
| `bud doctor` | `internal/doctor/serviceaccount.go` | Names the token's source; with `--net`, signs in and warns unless the service account sees exactly one vault. |
| Rules | `content/env/base.jsonc` | `bud secrets run *` allowed; `op`, `security`, anything naming `op-service-account`, and reading that file denied. |

## One call

`bud secrets run --secret GITHUB_PERSONAL_ACCESS_TOKEN=GH_TOKEN -- gh pr list`:

1. Refuse with no `--secret` or no command (a usage error).
2. Open the audit log. A log that won't open stops the command: a secret shouldn't reach a command unrecorded. The run is also written to it before the command starts, and a failed write stops it too.
3. `secretrun.Check` the command. It must name the command (no `/`), start with an allowed prefix, and not be `gh pr merge`, `gh issue delete` or `gh run delete`. `gh api` is reads only: no `-X` other than GET, no `-f`/`-F`/`--field`/`--raw-field`/`--input`, no `--hostname`, no URL.
4. Read `opencode.env` (`paths.OpenCodeEnv`, written by `bud sync`) and look each `NAME` up. `NAME=AS` hands it over as `AS`; `AS` can't be `OP_*`, `LD_*`, `DYLD_*` or a variable bud passes through. An unknown name lists the synced names, never a reference.
5. Find the token with `optoken`, and `op`, `env` and the command with `secretrun.Lookup`, which searches only `secretrun.TrustedDirs` (`/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`, `/bin`, and Linuxbrew's) and refuses a file anyone can write. The session's own `PATH` is never used: a fake `op` ahead of the real one would be handed the token.
6. `secretrun.Build`: the environment for `op` is `PATH` set to `TrustedDirs`, the variables in `passEnv` (`HOME`, `TERM`, the XDG directories and a few more), `AS=<the secret's op reference>` for each secret, and the token. Nothing else of the session's environment carries over, so `GH_HOST` and the proxy variables can't send a token to another host. A passed-through variable that holds an `op` reference is refused, since `op run` would resolve it. A secret can't be given under a name in `steering` (`GH_HOST`, `GH_REPO`, the proxy and certificate variables, `GH_ENTERPRISE_TOKEN` and others).
7. Run `op run -- env -u OP_SERVICE_ACCOUNT_TOKEN <command>`. `op run` resolves only the references it was given and masks their values in the command's output. The command holds the secrets and not the token.
8. Write a second audit line when the command ends and pass its exit code through. A line is `source: secrets`, `kind: secret_run`, with `target` the allowed prefix (`gh pr`, or `(not allowed)`), `decision` `allowed` or `refused`, and detail holding how many secrets were asked for, the names of those that passed, and the exit code or a fixed reason. What the caller typed (a spec, a command's arguments) is never recorded, since a session can put any value it holds there.

## What this does and doesn't stop

The service account sees only Bud Vault (`bud doctor --net` checks it sees one vault), so that vault is the boundary (D146). The allowlist, the clean environment and masking keep a secret out of a transcript and stop a session steering it by accident. They don't stop a session that is set on getting a value: a `gh` call it controls can echo what it was given in a way masking doesn't catch. The deny rules for `op`, `security` and the token file are about what a session types and are a speed bump. A separate-user broker would make the boundary real and isn't planned.

## Where the token lives

- **macOS:** the login Keychain, item `bud-op-service-account`. Add it with `security add-generic-password -a "$USER" -s bud-op-service-account -U -w`, which asks for the token at a hidden prompt, so it never reaches shell history or the process list.
- **Home server:** `~/.config/bud-server/op-service-account.env` (mode 0600), which the units already load. The planning OpenCode has its own under its own user.
- A launch with no token anywhere uses `op`'s own sign-in, as before.

## Adding a command

A command is a prefix in `secretrun.Allow`. Add it, and its refusals (anything that changes or deletes something Jason approves) in `Check`, with the test that pins both.
