# Secrets for sessions through `bud secrets run`: design

> Status: Tasks 2, 3, 4, 6, 7 and 8 are built (JGA-413 to JGA-415, JGA-417 to JGA-419, one PR). Not done: Task 1's measurement (JGA-412), the Mac Keychain step and its measurements (JGA-416), and the two rollouts (JGA-420, JGA-421).
> Date: 2026-11-27

## 0. The one-paragraph version

A Bud agent session (OpenCode on archie or the Mac) can run a command that needs a Bud Vault secret, without Jason signing in to 1Password and without the session holding the service account token. A session runs `bud secrets run --secret NAME -- command`. bud finds the Bud Vault-scoped token itself (its environment, the macOS Keychain, or the token file the home server's units load), starts `op run` with only the references the session named, and runs an allowlisted command under `env -u OP_SERVICE_ACCOUNT_TOKEN`, so it holds the secrets and not the token. Raw `op` stays denied. `bud agent` uses the same lookup, so launching Bud on the Mac needs no sign-in once the Keychain item is there.

The decisions are D142 to D148 in [decisions.md](../decisions.md); this file doesn't repeat them. How it works is in [docs/internals/secrets.md](../../docs/internals/secrets.md).

### Guiding principles

1. **The vault is the boundary.** The service account sees only Bud Vault. The allowlist, the clean environment and masking prevent accidents and transcript leaks, not a determined session (D146).
2. **Sessions never hold the token.** bud reads it at the moment it starts `op run` and puts it in that process alone (D142).
3. **One owner for secrets handling.** Bud-owned code, not rules about what a session may type (D7).
4. **No new prompts for ordinary use.** `bud secrets run` is allowed, and gated inside bud (D143, D145).

## 1. What was there

- `bud agent` ran `op run --env-file=opencode.env -- env -u OP_SERVICE_ACCOUNT_TOKEN opencode ...`, with `op` signing in however it could (on the Mac, the 1Password app).
- `bud resume` read the token from `--op-token-file`, for `op read` alone.
- Every home-server unit loads `~/.config/bud-server/op-service-account.env`.
- `op` and `op *` were denied to sessions. The Hermes gate unwraps `bash -c` and `env`; OpenCode's rules match the command string, so a rule on a wrapper can't see the command inside it. That is why `bud secrets run` checks the inner command itself (D145).
- **A name that was taken.** `bud run` already summarizes a command's output, so the wrapper is `bud secrets run`.

## 2. Options considered

| Option | Verdict |
| --- | --- |
| A. Allow `op run *` for sessions | Rejected. It only works if the session has the token, which also lets it `op read`; one rule can't keep the two apart. |
| B. `bud secrets run`, token held by bud | **Built.** Reuses the `bud resume` pattern, no new prompts, small. Same Unix user, so the token file is protected by policy, not by the OS. |
| C. A broker under a separate system user | Real isolation, but the command would run as the broker user and lose the session's files. Not planned. |
| `op run --environment` and the Environments MCP server | Deferred and not adopted (D147). |

## 3. Build order

| Task | Issue | State |
| --- | --- | --- |
| 1. Measure what a session sees today | JGA-412 | Pending: needs a run inside a `bud agent` session on each device (section 4) |
| 2. `internal/optoken` and `internal/envfile` | JGA-413 | Built |
| 3. `bud agent` signs `op run` in with the looked-up token | JGA-414 | Built |
| 4. `bud doctor` checks the service account and its vault scope | JGA-415 | Built |
| 5. Token into the Mac Keychain, measure the ACL | JGA-416 | Jason's step |
| 6. `bud secrets run` | JGA-417 | Built |
| 7. Session rules | JGA-418 | Built |
| 8. Docs and decisions | JGA-419 | Built |
| 9. Roll out on archie | JGA-420 | After the PR merges |
| 10. Roll out on the Mac | JGA-421 | After Task 5 and the PR |

Not planned: scoping the tool secrets (GitHub, Exa) out of OpenCode's launch environment into per-command calls, `op run --environment` support, a separate-user broker.

## 4. What Task 1 has to answer

`bud agent` resolves every line of `opencode.env` and hands the values to OpenCode, whose shell tool normally inherits that environment. If so, a session already sees launch-time secrets with `printenv`, and `bud secrets run` adds secrets that weren't loaded at launch and a way to move tool secrets out of the session-wide environment. Inside a `bud agent` session on each device, run these (names only, never values) and record the result here:

```sh
env | cut -d= -f1 | sort
command -v op security
test -r ~/.config/bud-server/op-service-account.env && echo token-file-readable
```

| Device | Secret variable names visible | `op` on PATH | Token file readable |
| --- | --- | --- | --- |
| Mac | not measured | not measured | not measured |
| Home server | not measured | not measured | not measured |

## 5. Open measurements (Task 5)

1. Whether `bud doctor` goes green on the Mac with the 1Password app closed.
2. Whether adding `-T <bud path>` to the Keychain item makes another process's `security find-generic-password -w` prompt, and whether `bud update` replacing the binary re-prompts. Bud's own signature isn't Apple's, so this can't be predicted. The answer is recorded as D149 and decides whether the ACL is worth keeping.
