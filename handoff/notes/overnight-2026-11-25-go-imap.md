# Overnight handoff: go-imap for internal/mail, 2026-11-25

**Done 2026-11-26.** A Claude Code session picked this up, implemented the spec, and opened the PR for JGA-399 (see `handoff/state.md`). What differed from the plan below: main had meanwhile merged tidwall/jsonc (BASE D153), so go-imap is bud's second third-party module, not the first; `send_test.go` also used the transcript replays, so it moved onto the in-process server too; and go-imap's in-memory server can't flag a folder `\Trash` or `\Noselect` or advertise `LOGINDISABLED`, so the tests answer `LIST` from a table and script the few servers it can't produce. The note below is kept as the record of the overnight plan.

Written by the OpenCode session that took Jason's overnight ask, finalized at the moment he passed the work to Claude Code with the implementation not yet started. Everything a fresh session needs to finish it is here or in the files it points at. Two other overnight sessions are running; "Fit with the other sessions" is for them. `git status` in the worktree is the live truth if this note has drifted.

## The ask

Jason, overnight 2026-11-25:

> IMAP client (internal/mail/ — ~4.3K lines). This is the single best candidate. The go-imap package is battle-tested across many projects. Replacing a homegrown IMAP client with a proven one would: cut ~2.5–3K lines, bring in years of edge-case fixes (race conditions in UIDPLUS, malformed responses, TLS edge cases), keep behavior the same or more correct.

> I'm giving a D1 exception here btw, I am working to change this if you are looking for it.

## Decided and verified

- **Suite D138** is recorded in `handoff/decisions.md` (this worktree): `internal/mail`'s IMAP client becomes `github.com/emersion/go-imap/v2` `v2.0.0-beta.9`, bud's first third-party Go module, under Jason's D1 exception. He is rewording BASE.md D1 himself — do not edit BASE.md D1, and expect his AGENTS.md bullet to change.
- **Package check:** there is no Google-authored go-imap; the maintained line is emersion/go-imap v2 (default branch `v2`, `v2.0.0-beta.9`, MIT, active, used by several mail clients; dependencies `emersion/go-sasl`, and `emersion/go-message` in its module graph).
- **The spec is `handoff/design/go-imap-mail.md`** — the adapter design, every preserved error sentence byte for byte, the capability-matrix test plan against the library's own in-process server (`imapserver`/`imapmemserver`), and the command list. Read it first.

## Physical state

- **Worktree:** `<the go-imap worktree>`, branch `feat/mail-go-imap`, based on `origin/main` at `14b8efa` ("Fix terminal sizing for bud agent with OpenCode (#128)").
- The main checkout `<the main checkout>` was **not touched** by this session; it is on another session's `fix/syncer-test-git-signing`.
- Baseline before any change: `go test ./internal/mail` → `ok jgarner.dev/bud/internal/mail 0.182s`.
- The worktree's `git status --porcelain` is exactly: `M go.mod`, `M handoff/decisions.md`, `?? go.sum`, `?? handoff/design/go-imap-mail.md`, `?? handoff/notes/overnight-2026-11-25-go-imap.md`. Nothing is committed, nothing pushed, no PR yet; the implementation has not been started.

## In flight — nothing

A coder subagent was dispatched to implement the spec and was cancelled before writing any code. `internal/mail` is untouched original code; the worktree holds only the spec, the decision log, the handoff note and the go.mod/go.sum dependency lines. There is no build report and no half-done implementation to untangle. `go test ./internal/mail` passes as the old client (nothing imports the library yet). Whoever picks this up writes the implementation from `handoff/design/go-imap-mail.md` from scratch; `git status --porcelain` in the worktree should show exactly: `M go.mod`, `M handoff/decisions.md`, `?? go.sum`, `?? handoff/design/go-imap-mail.md`, `?? handoff/notes/overnight-2026-11-25-go-imap.md`.

## Unblocked (2026-11-26 ~03:40 UTC)

Both of these were auto-denied unattended earlier in the night ("Denied: nobody answered this permission request within 10 minutes, so it was refused (Bud Suite D111). Don't retry it; leave the action for the user and say what you would have done."); Jason then answered "yes" in session and both went through:

1. The module is on disk: `go get github.com/emersion/go-imap/v2@v2.0.0-beta.9` ran in the worktree, and `go.mod` carries the require (marked `// indirect` until the imports land) with `go.sum` written. **Do not run `go mod tidy` until the code imports the library** — tidy on a tree with no imports would drop the require. Run it at the very end to clear the `// indirect` marker.
2. The Linear issue is filed as **JGA-399** (In Progress, milestone Bud 2.0): https://linear.app/jgarner-dev/issue/JGA-399/replace-the-homegrown-imap-client-in-internalmail-with-go-imap — the commit message and the PR body should carry it.

## The remaining plan

1. Read `handoff/design/go-imap-mail.md`, then `git status` / `git diff` in the worktree.
2. Run `go test ./internal/mail`: it passes as the old client — that is the expected starting state, since nothing imports the library yet. If it instead fails with "no required module provides for github.com/emersion/go-imap/v2", something reset the dependency; the fetch is one command: `go get github.com/emersion/go-imap/v2@v2.0.0-beta.9`.
3. With the module on disk, run the loop until green: `go test ./internal/mail`, `go vet ./internal/mail`, `gofmt -w internal/mail`, then `go test ./...` and `gofmt -l .` for the whole tree. The module source at `<GOMODCACHE>/github.com/emersion/go-imap/v2@v2.0.0-beta.9/` is the authority for any API name; the spec's numbered rules and error sentences are the contract.
4. Docs, in the same PR: `docs/internals/mail.md` (the IMAP and Tests sections rewritten to match the spec), a row in `handoff/state.md`, rows in `handoff/revisit.md` (the module fetch, the Linear filing, and the live-run item that stays), the one-line AGENTS.md D1-bullet amendment (coordinate with Jason's rework in flight), and this handoff note itself (the convention of `handoff/notes/project-handoff-2026-11-21.md`; drop it if Jason prefers).
5. `just leak-check` before the PR (the mail change touches no Lua, so `just check` needs stylua/selene only for unrelated halves; `go test ./...` at minimum).
6. Commit on `feat/mail-go-imap`, lowercase conventional with the issue id, e.g. `feat: replace the mail IMAP client with go-imap (JGA-399, Suite D138)`. Then the standing PR workflow: push, open the PR with what changed and why, assign Jason, request the Copilot review, wait for it, fix findings on the same branch, one summary comment after. Never merge.
7. The live-run item (iCloud's MOVE/UIDPLUS, Sent Messages filing, trash retention) stays in the docs' "Not yet" and revisit; `--record`'s tracer is deliberately preserved for it.

## Fit with the other two sessions

- `internal/mail`, `go.mod`, `docs/internals/mail.md` and the handoff files listed above are **owned by this branch**; the other sessions should not edit them in parallel.
- D-number collision: D138 is claimed in this worktree's `handoff/decisions.md`. If another session's decision reaches main as D138 first, renumber this one at merge time; the log is append-only and numbers are effectively assigned at merge tonight.
- Nothing outside the worktree was edited, so there is no overlap with the signing-fix session or any other checkout; the spec deliberately touches no `content/`, `server/`, IDE or plugin paths.
- The permission reality of tonight's sessions still holds: simple local commands run; anything network-reaching hangs 10 minutes and is refused. Do not attempt network commands unattended; leave them for Jason as paste blocks.

## The Linear issue (filed as JGA-399)

Filed 2026-11-26 with the draft below, unchanged: https://linear.app/jgarner-dev/issue/JGA-399/replace-the-homegrown-imap-client-in-internalmail-with-go-imap. The draft is kept here for reference.

Jason, overnight 2026-11-25:

> IMAP client (internal/mail/ — ~4.3K lines). This is the single best candidate. The go-imap package is battle-tested across many projects. Replacing a homegrown IMAP client with a proven one would: cut ~2.5–3K lines, bring in years of edge-case fixes (race conditions in UIDPLUS, malformed responses, TLS edge cases), keep behavior the same or more correct.

> I'm giving a D1 exception here btw, I am working to change this if you are looking for it.

**What changes.** The homegrown IMAP client in `internal/mail` — `imap.go` (the command engine), `wire.go` (the response parser) and `utf7.go` (modified UTF-7 folder names) — is replaced by `github.com/emersion/go-imap/v2` (v2.0.0-beta.9), the maintained Go IMAP library (MIT, active, used by several mail clients; deps: `emersion/go-sasl`, `emersion/go-message`). This is bud's first third-party Go module, under the D1 exception Jason granted on 2026-11-25; the BASE.md D1 rewording is Jason's own change in flight. Recorded as Suite D138.

**What must not change.** The tool contracts in docs/internals/mail.md: folders open with EXAMINE and bodies with BODY.PEEK, so nothing a read does marks mail read; a move to a server with neither MOVE nor UIDPLUS is refused rather than expunging others' deletions; a refused login is ErrAuth with the app-specific-password hint and never shows the password; a missing uid is ErrNoMessage; search text with a line break is refused; one connection per tool call, held to the call's two-minute deadline.

**Where it fits.** `account.go` (session glue), `cleanup.go` (move and restore), `record.go` (--record; its tracer wraps net.Conn below the client, so the recording survives), `actions.go`/`server.go` (unchanged), `docs/internals/mail.md` (the IMAP section), `handoff/decisions.md` (Suite D138).

**Tests.** The hand-written transcript replays (`testdata/*.imap`, `imap_test.go`) go away with the parser. The package's tests move onto go-imap's own `imapserver` as an in-process fake (same module, no new dependency), covering login both ways (LOGIN and AUTHENTICATE PLAIN under LOGINDISABLED), list and status, search with UTF-8 text, summaries, partial body fetch, move over MOVE and the COPY/STORE/UID EXPUNGE fallback, and the refusals. `server_test.go` and `send_test.go` keep passing unchanged.

**Done when** the checks pass on the PR (`go test ./internal/mail`, gofmt, go vet, leak-check), the docs and the decision log are updated in the same PR, and the one live check left (Sent Messages filing, trash retention, MOVE and UIDPLUS on iCloud) stays listed as the first live run.

## Resume prompt (paste into Claude Code)

Resume the go-imap overnight task (JGA-399, Suite D138) from the handoff note `handoff/notes/overnight-2026-11-25-go-imap.md` in the worktree at `<the go-imap worktree>` (branch `feat/mail-go-imap`, based on main `14b8efa`; the main checkout is another session's, don't work there). Read `handoff/design/go-imap-mail.md` (the spec) before touching anything: the implementation has not been started, and the spec is complete and authoritative. The go-imap module is already fetched (go.mod carries it, go.sum written; do not run `go mod tidy` until the code imports the library). BASE.md D1's rewording is Jason's own, so leave it alone. Write the implementation per the spec's numbered rules, run the compile-and-test loop until green (`go test ./internal/mail`, `go vet`, `gofmt`), then take the numbered plan in the handoff note: the docs rows in the same PR, commit on the branch carrying JGA-399, push and open the PR with a Copilot review per the standing workflow. Follow `handoff/README.md`'s "How Jason works" throughout, and report what the checks printed before calling anything done.
