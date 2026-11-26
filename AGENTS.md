# AGENTS.md

How to work on this repository. OpenCode reads this file (Bud Suite D17), and it is the only instructions file in the repo.

## What this is

This repo is growing into the Bud Suite monorepo (Suite D22, D56): one Go module building one `bud` binary, plus the IDE, home-server units and content that syncs to devices. The [current Suite architecture](docs/internals/suite.md) records decisions that guide work here. The Suite decision log is [handoff/decisions.md](handoff/decisions.md) and the build plan is `handoff/design/build-plan.md`. "Suite Dn" refers to that log. `BASE.md` has its own decision numbers for the IDE and CLI.

| Path | What it holds |
| --- | --- |
| `cmd/bud/`, `internal/` | The one Go module and binary: install, sync, update, doctor, the IDE launcher |
| `ide/` | The Neovim IDE: `nvim/`, `languages/`, `layouts/`, `tools/`, `migrations/`, `bundles/` and its Lua tests, all embedded in the binary |
| `content/` | What syncs to every device running OpenCode: `AGENTS.md`, skills, agents, commands, plugins and `env/` (the model registry and config fragments) |
| `profiles/` | Example device profiles only; real ones live in the private overlay |
| `server/` | Home-server service units for the MCP gateway, calendar, ops and Signal, plus Hermes setup and its Bud plugin |
| `app/` | Notes for Bud's planned Herdr desktop and mobile integration |
| `docs/`, `scripts/` | User and internals docs (also the docs site), and build and test scripts |

Nothing personal goes in this public repo: no hostnames, tailnet names, home paths, real `op://` paths or personal skills. Those live in the private overlay (Suite D21, D59).

## Resuming work

Before starting a task, read [handoff/README.md](handoff/README.md): how Jason works (lead with the answer, plain Unicode and no emoji, options with a recommendation, he decides design), the PR workflow, and links to the current state, the open items waiting on him and the Suite decision log (D1 onward). Keep `handoff/state.md` and `handoff/revisit.md` current in the same PR as the work. Host names and exact home-server commands are in the private overlay's `handoff/`, never here.

## The IDE

Bud's IDE is a Neovim-based editor driven by a small Go CLI (`bud`). The CLI installs, launches, updates and syncs an isolated Neovim config (`NVIM_APPNAME=bud`, so it never touches `~/.config/nvim`). `BASE.md` is the full plan plus a numbered decision log (D1, D2, ...). Nearly all of it is built, but a few parts are still marked as later, so check the code before assuming a planned feature exists. When a decision changes, update `BASE.md` too.

The IDE chat is the pinned sudo-tee/opencode.nvim talking to local OpenCode (D150, superseding the ACP plan in D131/D132 and D138-D140; Suite D96). `bud.ai.agent` and `bud.ai.acp` stay as the programmatic session behind `:BudGen`, issue filing, working an issue, inline actions and commit drafts, and they run only `opencode acp` (Suite D139); there is no other backend and no IDE bridge. Don't extend opencode.nvim with a fork; bud's changes live in `bud.ai.opencode`. Don't add a model provider or an API client unless asked.

## Docs

`docs/` holds the user docs: `setup.md` (the whole setup in order, including OpenCode and the synced content), `config.md` (every key and its default; `TestDocsCoverEveryKey` fails when a setting is missing from it, so add a row when you add one), references for every command (`commands.md`) and key (`keys.md`), the architecture diagrams (`architecture.md`, Mermaid, updated in the same change as the code they draw), and how-tos for languages, tools, layouts, filing issues and troubleshooting. The README keeps short descriptions and links to these. Keep them in step when behaviour changes. The same files are the docs site, https://bud.jgarner.dev (D110): `site/` holds only the landing page, layout and styles, `scripts/site.sh` copies `docs/*.md` in beside them, and `.github/workflows/pages.yml` builds it with GitHub Pages' Jekyll on a push to main. A new page in `docs/` needs a line in `site/_data/nav.yml`, and a link out of `docs/` (to the README, say) has to be a full URL, since only `docs/*.md` is on the site. `docs/internals/` is the other kind: how each part of the code works, for working on bud (see "Where the detail is"). Move detail there when a paragraph here grows, and keep this file short, since it loads in every session.

## Commands

```sh
just                 # list recipes
just check           # everything CI checks: lint, go test ./..., the Lua and script tests, smoke test
just test            # go test ./...
just test -run TestOpen ./internal/cli   # single test (args pass through to go test)
just run <args>      # go run ./cmd/bud <args>, e.g. just run update
just build           # ./bud (gitignored)
just fmt             # gofmt -w ., with the gofmt of the Go toolchain go.mod selects (what CI runs)
just lua-fmt         # stylua over every Lua dir
just lua-test        # headless Lua tests in ide/tests/lua (plain asserts, no framework), 60 s limit each
just install-test    # install.sh's checks against fake releases signed with a throwaway key
just rotate-test     # rotate-release-key.sh against a throwaway repo, with fake op and gh
just release-plan-test # release-plan.sh's decisions against throwaway repos with a fake gh
just content-release-test # content-release.sh's releases against throwaway repos with fake gh, bud and signer
just hermes-plugin-test # the bud-signal Hermes plugin's Python tests (stdlib unittest, no Hermes needed)
just opencode-plugin-test # the bud-unattended-deny OpenCode plugin's tests (node --test, no OpenCode needed)
just site-test       # assemble the docs site from site/ and docs/*.md; fails when a page isn't in the sidebar
just leak-check      # fail on personal details in tracked files (tailnet hosts, real op:// vaults, home dirs, BUD_LEAK_PATTERNS)
just leak-test       # leak-check.sh's own tests
just smoke           # sync the config into temp XDG dirs, restore plugins, start nvim headless (needs network)
just lang-check [go] # install every language's tooling in temp XDG dirs, check each on a sample project (slow, needs network)
just snapshot        # local GoReleaser build into dist/, needs goreleaser installed
```

`just lint` and `just check` need `stylua` and `selene` (`brew install stylua selene`); they fail with that hint when either is missing. Lua style is in `stylua.toml` (2 spaces, double quotes, 120 columns) and `selene.toml` + `vim.yml` (Lua 5.1 plus the `vim` global). CI pins stylua, selene and Neovim versions in `.github/workflows/ci.yml`, and runs the smoke test on the oldest supported Neovim (0.12, D19) and the newest.

If the Go build cache isn't writable (for example inside a sandbox), prefix commands with `GOCACHE=$TMPDIR/gocache`.

## Hard constraints

- **Standard library only (D1).** No third-party Go modules, with two exceptions. The JSONC stripper is github.com/tidwall/jsonc (D153), picked because its output keeps the input's byte offsets and line breaks, which the registry's in-place patching and error lines depend on. The mail IMAP client is github.com/emersion/go-imap/v2 (Suite D138), behind `internal/mail`'s adapter, so the tools' guarantees stay bud's own. Commands use `flag.FlagSet`, not cobra. Config and data files are JSON (D2).
- **Flags go before positionals (D4):** `bud [global flags] <command> [command flags] [args]`. That's plain `flag` behavior, so don't add interspersed parsing.
- **macOS and Linux only (D22).**

## Architecture
**Command registry.** Each command is one file in `internal/cli/` that calls `RegisterCommand` from `init()`. `App.Run` (`internal/cli/cli.go`) parses global flags. If the first argument names a registered command it runs that command; otherwise it falls back to `open` with the argument as a path, so `bud .` and `bud file.go` work. A command with `Subs` takes one next (`bud lang enable go`); one with `Subs` and no `Run` is only a group, and its help lists them. Each command gets its own `FlagSet` through `Command.Flags`. Global flags (`--verbose`, `--no-update`, `--profile`, `--layout`) are parsed into a `*Globals` that `App.Run` hands to every `Command.Run`; `Globals.Logf` is the `--verbose` logger. Usage text is written by `globalUsage`/`commandUsage`, and `flagOrderHint` explains D4 when a global flag lands after the command.

**Prompts.** `terminalPrompter(yes)` returns a prompter, or nil with `--yes` or no terminal (nil takes every default); `confirmOnTerminal` guards destructive steps and refuses without a terminal, naming the `--yes` command. A command not built yet returns an error saying so, never a silent exit 0.

**Errors and output.** Commands return errors and never print errors or call `os.Exit`. `cli.ExitCode` (`internal/cli/errors.go`) is the only place that prints errors (one `bud: ...` line on stderr) and picks the exit code: 0 for success or `-h`, 2 for a `*UsageError` (build one with `usageErrorf`), 1 for anything else. `App.parse` silences the flag package and prints requested help to stdout, and the error plus usage to stderr. `App.Stdout`/`App.Stderr` let tests capture that output.

**Embedded config (D5).** The root package `bud` (`embed.go`) embeds `ide/nvim/`, `ide/languages/*.lua` (a file glob, so `_template.lua` isn't dropped), `ide/layouts/*.lua`, `ide/tools/`, `ide/migrations/*.lua` and `ide/bundles/` with `//go:embed`, and `bud.Config()` serves the first four as one tree: `ide/nvim/` at the root with `languages/`, `layouts/` and `tools/` mounted inside. It must stay at the repo root, because embed can't reach parent directories. `ide/migrations/` and `ide/bundles/` are embedded but not synced into the config dir (`bud.Migrations()`, `bud.Bundles()`). The binary is the release: `internal/nvim.SyncConfig` copies `bud.Config()` into `~/.config/bud` and records them in `.bud-manifest.json`. It overwrites managed files, deletes ones a release dropped, and never touches anything else. After a change it runs `bud.pack.restore()` in headless nvim, so plugins match the committed `ide/nvim/nvim-pack-lock.json`. The target dir comes from `paths.Resolve().Config`, which is `~/.config/bud` on macOS too, because that's where Neovim looks.

**Paths and machine config.** `internal/paths.Resolve` is the only place that builds a bud path (XDG on macOS too, D13); don't hardcode one elsewhere. That includes the language spec dirs (`LanguageSpecs`, `UserLanguageSpecs`). Files bud rewrites (`config.json`, `projects.json`, the sync manifest) go through `internal/atomicfile.WriteFile`. `internal/config` keeps `config.json` as a raw JSON tree so unknown keys survive `Save`, and `File.Settings()` decodes it over `Defaults()`.

**Doctor.** `internal/doctor.Run` returns one `Result` per check (`OK`/`Warn`/`Fail`). Only `Fail` makes `bud doctor` exit non-zero, which the updater will use for rollback. New checks are functions added to `Run`.

## Where the detail is

Each area of the code has a paragraph in `docs/internals/`. Read the file before changing that area; don't work from memory of it. Rules that hold everywhere are listed after the table.

| Working on | Read |
| --- | --- |
| A command in `internal/cli/` other than the above: colour, completion, `bud config`, uninstall, launch and open, install, `bud lang`, projects, dev link, migrations, version | `docs/internals/cli.md` |
| `bud bundle` and the desktop apps | `docs/internals/bundles.md` |
| `bud update`, rollback, the background check | `docs/internals/updater.md` |
| `bud.ai.acp`, the OpenCode transport and the fake ACP agent | `docs/internals/acp.md` |
| `bud.ai.*`: the session, the TUI pane, permissions, chat, inline actions, tools, the generator, filing and working issues | `docs/internals/session.md` |
| Anything under `ide/nvim/`: plugins, lazy loading, languages, layouts, debugging, UI, the dashboard, the settings screen | `docs/internals/neovim.md` |
| `bud bootstrap`, the nightly build, release signing | `docs/internals/release.md` |
| `bud mcp-gateway` (`internal/mcpgateway`): the home server's MCP gateway and its unit in `server/mcp-gateway/` | `docs/internals/mcp-gateway.md` |
| `bud router` (`internal/router`): the home server's OpenAI-compatible router over the registry's providers and its unit in `server/router/` | `docs/internals/router.md` |
| `bud calendar-mcp` (`internal/calendar`): the calendar MCP server over CalDAV and its unit in `server/calendar-mcp/` | `docs/internals/calendar.md` |
| `bud mail-mcp` (`internal/mail`): the iCloud mail MCP server over IMAP, through a thin adapter on go-imap | `docs/internals/mail.md` |
| `bud memory-mcp` (`internal/memorymcp`, `internal/memory`): the memory MCP server over the working-memory service and the embedding server, and its unit in `server/memory-mcp/` | `docs/internals/memory-mcp.md` |
| `bud improve` (`internal/improve`): the self-improvement loop's proposals, measurements, model scan and weekly digest, and its timer in `server/improve/` | `docs/internals/improve.md` |
| `bud memory-review` (`internal/memoryreview`): asking on Signal about staged memory records, and its unit in `server/memory-review/` | `docs/internals/memory-review.md` |
| `bud mcp-ops` (`internal/mcpops`): the Bud ops MCP server and its unit in `server/mcp-ops/` | `docs/internals/mcp-ops.md` |
| Signal for Hermes: the `bud-signal` plugin in `server/hermes/plugins/`, approvals by reaction or code, signal-cli's unit in `server/signal-cli/` | `docs/internals/signal.md` |
| bud-kb, its curator and its site: the page format (`internal/kb`), `bud curator` (`internal/curator`) and its timer in `server/curator/`, `bud kb-site` (`internal/kbsite`) and the Hugo layouts in `server/hugo/` | `docs/internals/kb.md` |
| `bud resume` (`internal/resume`): starting the next OpenCode orchestrator after a handoff, the `/handoff` command and the timer in `server/resume/` | `docs/internals/resume.md` |
| `internal/content`: content bundles, `content.json`, `channel.json` and their signatures | `docs/internals/content.md` |
| `bud sync` (`internal/syncer`): fetching, layering, rendering, the ledger and rollback; the adapters (`internal/adapter/opencode`, `hermes`) and the env fragments' `bud` block (`internal/adapter/env`) | `docs/internals/sync.md` |

Rules that apply wherever you are:

- The IDE's skills and chat agent are ordinary synced content (`content/skills/file-issue`, `content/skills/bud-dev`, `content/agents/chat.md`); there is no embedded plugin (Suite D139, BASE D154). Personal skills live outside bud.
- Never edit or renumber a shipped file in `ide/migrations/`.
- Never run `scripts/rotate-release-key.sh`; Jason runs it by hand.
- `ide/nvim/nvim-pack-lock.json` is generated with `vim.pack` itself and committed byte for byte. Plugin versions change only by regenerating it and shipping a release.
- The first screen has a 50 ms budget (D35), empty or opening any language's file. Measure before making anything lazy, and never touch `vim.lsp` before the first screen.
- Anything that needs `nvim` in a Go test skips without it; CI runs those in the smoke job.

## Release and install

Releases come from `VERSION` (D80): a push to main that changes it runs `.github/workflows/release.yml`, where `scripts/release-plan.sh` checks it (`X.Y.Z`, newer than the newest stable tag; `just release-plan-test`), the tests run, the workflow pushes the `vX.Y.Z` tag itself and GoReleaser builds linux/darwin × amd64/arm64 archives plus `checksums.txt`. Cloud sessions can't push tags (their git proxy allows only `refs/heads/*`), so to cut a release, use the `release` skill (`content/skills/release`), where an OpenCode session walks a written review checklist over everything since the last tag and reports its findings before it bumps `VERSION` and pushes main (Copilot stays the per-PR gate, Suite D139); a hand-pushed `v*` tag must match `VERSION`, and re-running the workflow builds a tag that has no release yet. Archive names deliberately leave out the version (`bud_<os>_<arch>.tar.gz`) because `install.sh` downloads from `releases/latest/download/<name>`, so keep `.goreleaser.yaml` and `install.sh` in sync. Content releases (`content-YYYY.MM.DD.N` and the rolling `content-channel`, cut by `.github/workflows/content.yml` on a push to main that changes `content/`) are always prereleases for the same reason; how they're cut is in `docs/internals/release.md`. `install.sh` must stay POSIX `sh` and shellcheck-clean. It verifies the signature on `checksums.txt` with `ssh-keygen -Y verify` against its pinned `RELEASE_KEYS`, one per line (D41/D44; a test keeps them equal to `updater.ReleaseKeys`), then the checksum, installs to `~/.local/bin`, and asks on `/dev/tty` before running `bud install`, because stdin is the script under `curl | sh`.

## Tests

Tests swap package-level vars and restore them with `t.Cleanup` (`internal/cli`'s `TestMain` also turns off `refreshBundles` and `findBundles` for every test there, so none reaches GitHub or rebuilds this machine's Bud.app): `CommandRegistry`, `installer.Deps`, `installer.goos`, `version.Version`. Tests that need binaries on PATH use the `fakePath` helper in `internal/installer`. `App` tests build an `App` with a custom `Cmds` map and recorder commands instead of using the global registry.

## Commits

For a fix, commit on a dedicated branch and open a PR as part of the task. Address review findings on that PR and leave one summary comment after pushing the fixes. If the review is clean, leave the PR alone. Leave merging to Jason unless he asks.

Lowercase conventional style: `feat:`, `fix:`, `chore:`, `docs:`, followed by a short summary.

## Compact instructions

When compacting, keep: the decisions made and why (with their D-numbers), the file paths touched, the failing test or check and its exact output, the commands that reproduce it, and what is still to do. Drop file contents you have already read and tool output that led nowhere.
