# Commands

Every `bud` command, its subcommands and its flags. `bud <command> -h` prints the same from the binary.

Flags go before positional arguments, and global flags go before the command:

```sh
bud [global flags] <command> [command flags] [args]
```

| Global flag | What it does |
| --- | --- |
| `--verbose` | Log what bud is doing on stderr. |
| `--profile full\|minimal` | The install profile, for `bud install`. |
| `--layout <name>` | The pane layout to open the editor with, just this once. |
| `--no-update` | Skip the background update check at launch. |

`-y` is short for `--yes` on `install`, `uninstall`, `lang enable` and `lang rm`.

## Opening things

| Command | What it does |
| --- | --- |
| `bud [path ...]` | Same as `bud open`. No path opens the dashboard. |
| `bud open [path ...]` | One directory opens as a project (and registers it). Files open from the root of the git repo they're in, else from the first file's directory. |
| `bud launch [--terminal kitty\|ghostty] [--layout name] [path ...]` | Opens bud in a new terminal window. `terminal` in `config.json` picks the default, else the first one installed. |
| `bud bundle [--macos\|--linux] [--dest dir]` | Builds the desktop app: `Bud.app` on macOS, `bud.desktop` on Linux. With Kitty as the terminal, `Bud.app` is its own copy of kitty, an app called Bud that Cmd+Q and window managers treat apart from kitty (match it in AeroSpace with `if.app-id = 'dev.jgarner.bud'`); run it again after kitty updates, which `bud doctor` points out. `bud install` does this on the full profile. |

## Setting up and staying healthy

| Command | What it does |
| --- | --- |
| `bud install [--yes]` | Asks for the profile, terminal and languages, shows the package manager command for anything missing and runs it once you agree (on Linux it downloads a checksum-verified Neovim or tree-sitter-cli itself when the package manager's is missing or too old), syncs the config, restores plugins, then runs `bud doctor`. `--yes` takes every default. |
| `bud install --device <name> [--yes]` | Sets up the components a [device profile](profiles.md) names, such as `dev-machine` (the overlay's profile of that name first, else bud's example) and remembers it as the `device` setting, so a later `bud install` does the same. `ide` is the install above; the others say which phase of the build brings them and exit 1. Without `--device` or the setting, it's the IDE alone. `--profile` stays the IDE's install size. |
| `bud update` | Installs a newer release (only for a binary `install.sh` installed), syncs the config, restores plugins, parsers and language tools, rebuilds the desktop app if you've run `bud bundle` and it's out of date (another bud built it, kitty updated, or it moved; not while it's open), then runs the full `bud doctor` and exits 1 on a failure. Rolls back by itself if a check that passed before now fails. |
| `bud update --check [--channel stable\|nightly]` | Only says whether a newer release is out. |
| `bud update --from-main` | Development: builds the tip of `main` from source and installs it, so you needn't wait for the nightly or cut a release. It clones `main` into `~/.cache/bud/source` (later runs only fetch), runs `go build` with the release flags, and installs the result the way a release is installed, with the same rollback. Needs `git` and Go on `PATH`. The build is **unsigned**: it skips the release signature and checksum check, and says so when it runs. Content isn't touched; it stays on its own signed channel, so run `bud sync` afterwards. The build is named `X.Y.Z-nightly.YYYYMMDDHHMM.main.<commit>`, so it counts as a nightly built at that time: a later nightly or release replaces it, and running it again on the same commit does nothing. It can't go with `--check`, `--background` or `--channel`. |
| `bud update --no-doctor` | Skips the app rebuild and the doctor at the end, for scripts. |
| `bud update --background` | What bud starts by itself at launch, detached and at most once a day, so launching never waits on the network. It installs a newer stable release when `update.auto` is on, else only notes it for the dashboard. `--no-update` skips it. You don't normally run it. |
| `bud doctor [--quick] [--json] [--net]` | Health checks with the fix for each problem; exits 1 when something is broken. `--quick` skips the slow ones: the startup timing, which the dashboard leaves out. On a device with a profile set, it also checks what `bud sync` left: the profile and registry validate, every file the sync wrote is unchanged, which layer (public or your overlay) each piece of content comes from, and the config on disk is what this bud would render. `--net` adds the checks that go online: whether the device's channel has newer content, whether each model endpoint answers and lists the registry's models, and whether a provider that reports model facts (OpenCode Go does) disagrees with the registry about any of them, and, when `BUD_SEARXNG_URL` is set, whether that SearXNG instance answers JSON searches. On a device with `opencode.env`, doctor also names where the 1Password service account token comes from (its warning, never a failure, says where to put one when there is none), and with `--net` signs in with it and warns unless it sees exactly one vault (Suite D144, D146). The `--net` checks share one 30-second deadline. It doesn't check your MCP servers or rules, which are yours (D144). |
| `bud uninstall [--keep-config] [--yes]` | Lists everything bud put on the machine and removes it after asking. `--keep-config` keeps `config.json` and your tools. |
| `bud bootstrap [--config] user@host` | Installs this version of bud on another machine over SSH, minimal profile by default. `--config` copies your `config.json` across first. |
| `bud version` | Prints the version. |
| `bud completion bash\|zsh\|fish` | Prints shell completion. Add `eval "$(bud completion zsh)"` to your shell rc. |
| `bud sync [--dry-run] [--rollback]` | Brings this device's content and OpenCode config up to its channel: fetches the channel's newest signed content bundle (refusing anything unsigned or older), fast-forwards your overlay, and renders `~/.config/opencode/opencode.jsonc` plus its agents and commands (and, on a `server` device, Hermes's `~/.hermes/config.yaml`, copying the synced skills into `~/.hermes/skills` and locking them there) from the model registry and env fragments. A file you edited by hand, or that bud didn't write, is skipped and named, never overwritten. A sync that has nothing to do changes nothing. `--dry-run` lists what would change; `--rollback` switches back to the previous content version still on disk, and later syncs stay there until a newer version is out. Needs the `device` setting. See [how sync works](https://github.com/jgarner9/bud/blob/main/docs/internals/sync.md). |
| `bud registry check <registry.jsonc> [overlay.jsonc]` | Validates the model registry, with an overlay's merged on top when given, and lists every problem with its fix. See [the model registry](registry.md). |
| `bud registry refresh [--write] <registry.jsonc> <provider>` | Rechecks one provider's models against the provider's own model list: facts that drifted (inputs, caps, context, output, price), models it no longer lists, and models the registry doesn't have. `--write` rewrites the drifted facts in place, keeping comments and every hand-kept field, and writes nothing if the result wouldn't pass `registry check`. It never adds or removes a model. See [the model registry](registry.md#keeping-it-true). |
| `bud content pack --version YYYY.MM.DD.N [--out dir] <content dir>` | Packs a content dir into `content-<version>.tar.gz` and `content.json`, the bundle CI signs and publishes. You don't normally run it. |
| `bud content channel --edge V [--stable V] [--out dir]` | Writes `channel.json` naming each channel's content version, for the content release workflow to sign and publish; stable can't be ahead of edge. You don't normally run it. |
| `bud content offered [--channel edge] <channel.json>` | Prints the content version a published `channel.json` offers on one channel, refusing it unless `<file>.sig` verifies. The workflow reads it before promoting. You don't normally run it. |

## Running commands

`bud run [--timeout D] [--tail N] [--context N] [--max-failures N] [--json] [--max-chars N] -- <command> [args...]` runs a command and saves its full combined output in a private log under Bud's state directory. It prints a compact summary with failure lines and a bounded tail. A failing command returns its exit status; a timeout returns 124. Use `--sh` for an explicit shell command. Read a saved log with `bud run --log <path> --lines 1:40` or `--tail N`.

## Finding code

`bud locate [--kind any|def|use] [--lang <language>] [--max <hits>] [--json] [--max-chars <chars>] [--] <query> [path ...]` finds a symbol or literal phrase in local files. Paths default to the working directory. Put `--` before a query that starts with a dash.

A single identifier is a case-sensitive symbol. Definitions rank first, and `--kind def` or `--kind use` filters them. Other queries use literal smart-case matching. The result groups hits by file and includes a line number and a short excerpt for each hit. `--lang` can narrow a search to Go, Lua, Python, TypeScript, JavaScript, Rust or shell.

The default is 20 hits and 6,000 text characters. Set either limit to 0 to disable that display cap. JSON output includes counts, tool notes and hits, and is unaffected by the character cap. No matches is a successful result.

Locate uses ripgrep if installed and falls back to grep, which can also search ignored files. Optional ast-grep improves definition detection, and repomap can rank phrase hits. Locate needs no network or model.

## Languages, layouts and projects

| Command | What it does |
| --- | --- |
| `bud lang ls` | Every language spec, whether it's on, and why a broken one is broken. |
| `bud lang enable [--yes] <name ...>` | Turns languages on and installs their system packages and tools. |
| `bud lang disable <name ...>` | Turns languages off. Their tools stay installed until the next `bud update` trims them. |
| `bud lang add <name>` / `rm [--yes] <name>` | Starts a spec of your own from the template, or removes one after asking. See [languages](languages.md). |
| `bud layout ls` | The layouts, marking the default and this project's. |
| `bud layout set-default <name>` | The layout projects open in. |
| `bud layout set <name> [project]` / `unset [project]` | One project's own layout, or back to the default. See [layouts](layouts.md). |
| `bud projects ls` | Registered projects, pinned first. Opening a directory registers it. |
| `bud projects add [--opened]` / `rm` / `pin` / `unpin` | Manage the registry. `add` registers a directory without opening it (`--opened` also records it as opened now); `rm` forgets a project, leaving its files alone. |

## Config

| Command | What it does |
| --- | --- |
| `bud config ls [--changed]` | Every setting, defaults filled in, or only what you changed. |
| `bud config get <key>` | One setting. |
| `bud config set [--force] <key> <value>` | Changes one. The value is JSON, or a plain string. Unknown keys are refused unless `--force`. |
| `bud config unset <key>` | Back to the default. |
| `bud config edit` | Opens `config.json` in bud's editor and checks it when you close it. |
| `bud config path` | Where `config.json` is. |

Every key and its default is in the [config reference](config.md).

## OpenCode

OpenCode is the one AI agent bud runs (Suite D139); there is no backend to choose. In the editor it goes over the Agent Client Protocol (ACP), and signing in to its models is OpenCode's own step.

| Command | What it does |
| --- | --- |
| `bud ai status [--json]` | Whether OpenCode is installed, which version, and a fix for each problem. |
| `bud ai setup [--yes]` | Installs the pinned OpenCode with npm if it's missing (asking first), then prints the status. |
| `bud agent [path]` | OpenCode's own TUI in a project, from the shell. When `bud sync` has written `opencode.env`, it runs through `op run` against a private OpenCode server; otherwise it launches OpenCode directly. `op run` signs in with the service account token when bud can find one (its own environment, the macOS Keychain item `bud-op-service-account`, or `~/.config/bud-server/op-service-account.env`), so the launch needs no 1Password sign-in; OpenCode itself never gets the token (Suite D142, D144). |
| `bud secrets run --secret NAME[=AS] [--secret ...] -- <command> [args...]` | Runs a command with secrets from the Bud vault and never shows a session their values (Suite D142, D145). `NAME` is a variable in `opencode.env` (what `bud sync` wrote); `NAME=AS` hands it to the command as `AS`, such as `GITHUB_PERSONAL_ACCESS_TOKEN=GH_TOKEN` for `gh`. bud finds the service account token itself, starts `op run` with only the named references, and runs the command under `env -u` so it holds the secrets and not the token. Only `gh pr`, `gh issue`, `gh api` (reads only) and `gh run` are allowed, and not `gh pr merge`, `gh issue delete` or `gh run delete`; a shell, `printenv`, `env` or a path is refused, and so is a `gh` command with flags before its subcommand (`gh pr -R o/r merge`), a URL, or a repository with a host. `op`, `env` and the command are found only in `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin` and `/bin` (and Linuxbrew's), never on the session's `PATH`. The command gets a small environment (`HOME`, the XDG directories and a few more, and that fixed `PATH`), not the session's, so no proxy settings either. Every run and every refusal is in the audit log, with names, counts and exit codes and never a value or what the caller typed, and a log that won't take the record stops the command before it starts. Not to be confused with `bud run`, which summarizes a command's output. |
| `bud chat` | OpenCode's own TUI, run as the synced `chat` agent (it reads and looks things up but can't edit files or run commands), in a directory that belongs to no project. `--resume` is refused: OpenCode keeps one chat, which opens where it left off. |

## Tools and issues

| Command | What it does |
| --- | --- |
| `bud tools ls` | Your tools and bud's, and whether each is on. |
| `bud tools gen "<description>"` | Has the agent write a tool or language spec, in the editor. See [tools](tools.md). |
| `bud tools enable` / `disable [--reason] <name>` | Turns a tool on or off. |
| `bud tools rm [--yes] <name>` | Deletes one of your tools, after asking. |
| `bud tools promote [--repo dir] [--yes] <name>` | Moves one of yours into a bud checkout's `ide/tools/`, commits and pushes it, after asking. `--repo` defaults to the `bud dev link` checkout. |
| `bud issue target [--bud] [--json] [dir]` | Where an issue filed from the editor goes in Linear. `--json` prints it for the editor. |
| `bud issue set --team T [--project P] [--global] [project]` / `unset [project]` | Sets or clears it. See [filing issues](issues.md). |
| `bud issue checkout [path]` / `--unset` | Saves the bud checkout `<leader>aB` works in (checked to still be a bud git checkout), forgets it, or with neither says what is saved. See [filing issues](issues.md). |

## The home server

| Command | What it does |
| --- | --- |
| `bud mcp-gateway [--config file] [--registry file]` | Runs the MCP gateway (Suite D72): devices on the tailnet reach Linear, Exa and other keyed MCP servers at `http://<listen>/<server>/mcp`, the gateway adds each server's key from its environment, logs every tool call, raises a call's tier when a registry privacy rule matches its arguments (Suite D112) and refuses cloud servers to sessions whose `X-Bud-Privacy` tier they don't allow (no header counts as P0). The rules come from `--registry`, or else the synced content's registry with the overlay's. The config defaults to `~/.local/state/bud/mcp-gateway.json`. It runs under `op run` as the unit in `server/mcp-gateway/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/mcp-gateway). |
| `bud router [--listen addr] [--registry file] [--topup-cap dollars] [--spend file]` | Runs the router: one OpenAI-compatible endpoint (`GET /v1/models`, `POST /v1/chat/completions`, `/v1/completions`, `/v1/embeddings`) over every registry provider's models, which a request names by id or as `bud/<role>`. It adds each provider's key from its environment. `--listen` defaults to `127.0.0.1:8795` and must be a loopback or tailnet IP; `--registry` defaults to the synced content's. It tracks spend per model from responses' usage; `--topup-cap` (default 10, 0 for none) is the dollars a month of top-up it will spend before answering 402, and `--spend` is the ledger (default `router-spend.json` in the state dir). Ranking isn't built. It runs under `op run` as the unit in `server/router/`. |
| `bud calendar-mcp [--listen addr] [--url url] [--tz zone]` | Runs the calendar MCP server (Suite D82) on loopback, behind the MCP gateway, which lets only P0 sessions use it: Hermes lists calendars and events, and creates, moves and deletes events, which it asks Jason about first. Only Hermes's private profile reaches it (Suite D95), and the router keeps any chat holding a calendar call on the local model (Suite D117, D119). It reads iCloud over CalDAV with the Apple ID and app-specific password `op run` puts in `BUD_CALDAV_USER` and `BUD_CALDAV_PASSWORD`. Reminders are events with alarms. It runs as the unit in `server/calendar-mcp/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/calendar-mcp). |
| `bud mail-mcp [--listen addr] [--imap host:port] [--smtp host:port] [--trash folder] [--log file] [--record file]` | Runs the iCloud mail MCP server (Suite D106) on loopback, behind the MCP gateway, for P0 sessions only. Hermes lists folders, searches and reads mail without marking it read, moves mail to Deleted Messages and back (logged for the daily digest), and sends and replies, each of which waits for Jason's approval on Signal with the whole message shown (D107). It logs in with the Apple ID and app-specific password `op run` puts in `BUD_MAIL_USER` and `BUD_MAIL_PASSWORD`, and sends from `BUD_MAIL_FROM` (unset turns sending off). `--record file` runs one read-only session instead and writes it, redacted, for checking what iCloud supports. It runs as the unit in `server/mail-mcp/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/mail-mcp) and [how it works](https://github.com/jgarner9/bud/blob/main/docs/internals/mail.md). |
| `bud memory-mcp [--listen addr] [--socket path] [--registry file] [--no-backfill]` | Runs the memory MCP server (Suite D102) on loopback, behind the MCP gateway, which tells it each session's tier (D108). Every runtime searches and stages Bud's working memory through its `memory_search` and `memory_stage` tools; a search sees only what its session's tier may, and a staged record waits for Jason's review. It reads the memory service's socket (default `$XDG_RUNTIME_DIR/bud-memory/memory.sock`) with five tokens `op run` puts in `BUD_MEMORY_TOKEN_*`, embeds with the registry's embedding model, and embeds newly approved records every minute. It runs as the unit in `server/memory-mcp/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/memory-mcp). |
| `bud improve run [--public owner/name] [--overlay owner/name] [--digest-day sunday] [--digest-hour 10]` | One day's round of the self-improvement loop (Suite D47, D125 to D129) on the home server: records router spend and doctor counts, scans every provider's model list against the registry once a week (changed facts become a registry proposal, new models are listed with the roles they could fill), follows the draft pull requests it opened, and checks a merged change a week later, proposing a revert when a number got worse. When the digest is due it asks Jason one Signal prompt: a yes opens each new proposal as a draft pull request labelled `bud-proposal`, his no drops them. It needs `BUD_IMPROVE_GITHUB_TOKEN` from `op run` and runs as the timer in `server/improve/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/improve). |
| `bud improve list` | Lists the queued proposals: id, status, target (`public` or `overlay`), where it is (pull request, edge or stable version) and title. |
| `bud improve show [<id>]` | Prints the digest as it would be asked now, or one proposal's description, check results and note. |
| `bud improve add <file\|->` | Queues a proposal written by hand (JSON: `title`, `evidence`, `target`, `files` with whole `before` and `after` contents, `criteria`), for the next digest. A public one may change only `content/` and is refused when it holds a personal detail; a change Jason already turned down isn't queued again. |
| `bud memory-review [--socket path] [retier <id> <P0\|P1\|P2>]` | Asks Jason on Signal about each memory record an agent staged (Suite D113), through the bud-signal plugin's reviewer socket, with the whole record in the prompt. A yes approves it, so memory search finds it; his no deletes it; an unanswered prompt leaves it staged and asks again a day later. At most four prompts wait at once. It reads the memory service's socket with the reviewer token `op run` puts in `BUD_MEMORY_TOKEN_REVIEWER`, and writes the counts (never the bodies) to `memory-review.json` in bud's state dir for the ops `reviews` tool. `retier <id> <tier>` runs once instead of the loop (Suite D136): it asks on Signal, with the whole record and where it goes, and moves an approved record to that tier only on his yes (the reviewer token needs the `retier` and `export` actions). It runs as the unit in `server/memory-review/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/memory-review). |
| `bud curator run [--kb dir] [--socket path] [--registry file] [--tiers P1,P2]` | The KB curator's nightly run on the home server (Suite D123, D124): reads approved working memory through the memory service's `export` action, asks the registry's `curator` role on local providers only which facts each new record states, promotes a fact into bud-kb once two independent records agree, queues contradictions, flags sections untouched for 6 months and archives them at 12, commits each change with its evidence and pushes. Needs `BUD_MEMORY_TOKEN_CURATOR`; runs as the timer in `server/curator/`, see [its README](https://github.com/jgarner9/bud/tree/main/server/curator). |
| `bud curator promote <record id>` | Promotes one memory record's facts on the next run without waiting for a second record. |
| `bud curator resolve <id> keep\|take\|confirm` | Answers an item on the KB's review queue: keep the page's value, take memory's, or confirm an evergreen section is still right. Commits and pushes. |
| `bud curator restore <projects\|systems>/<page>#<section>` | Moves an archived section back onto its page. Commits and pushes. |
| `bud resume queue [--dir checkout] [--agent name] <file\|->` | Queues the resume prompt an OpenCode orchestrator session writes when it hands off (JGA-394), from a file or `-` for stdin, so the next session starts without Jason. The `/handoff` command runs it as its last step. It refuses when a prompt is already queued. |
| `bud resume run [--tmux bin] [--opencode bin] [--min-age 2m] [--max-per-day 8]` | What the timer in `server/resume/` runs every two minutes: when a prompt has been queued for `--min-age`, it claims it once and starts OpenCode with it in the detached tmux session `bud-orchestrator`, replacing the session that queued it, and waits for it to report that opencode started. Asks in that session wait for Jason (`tmux attach -t bud-orchestrator`) and are refused after 10 minutes unanswered. It never auto-approves. More than `--max-per-day` starts in 24 hours fails and leaves the prompt queued. See [its README](https://github.com/jgarner9/bud/tree/main/server/resume). |
| `bud resume status` | Shows the kill switch, whether an orchestrator is running, the queued prompt, and the prompts claimed in the last 24 hours (what the cap counts). |
| `bud resume off` / `bud resume on` | The kill switch: `off` waits for a run in the middle of a start, then stops the running orchestrator and starts no more, leaving a queued prompt queued; `on` lets the timer start it again. |
| `bud kb-site [--listen addr] [--kb dir] [--hugo bin] [--interval d] [--no-pull] [--once]` | Builds the KB site from bud-kb's committed pages with Hugo and serves it on loopback or the tailnet, `127.0.0.1:8799` by default (Suite D53, D122). Every minute it pulls the clone in `~/.local/share/bud/kb` with `--ff-only` and builds a new HEAD into a fresh directory; the build replaces the live site only when Hugo succeeds, every internal link resolves and nothing looks like a secret, so a bad commit keeps the last good site. `--once` builds HEAD and exits. It runs as the unit in `server/hugo/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/hugo). |
| `bud contacts-mcp [--listen 127.0.0.1:8794]` | Runs the read-only iCloud CardDAV worker for private Hermes using its loopback bearer token. See the [server setup](https://github.com/jgarner9/bud/tree/main/server/contacts-mcp). |
| `bud mcp-ops [--listen addr]` | Runs the Bud ops MCP server (Suite D83) on loopback, `127.0.0.1:8792` by default: read-only tools for `bud doctor` (offline, without the `--net` checks) and the sync status (the content version, the overlay commit and any drift), so Hermes can say whether everything is healthy, plus this month's spend (`spend`), the count of approval requests waiting (`approvals`) and the count of staged memory records waiting on Jason's review as `bud memory-review` last counted them, with `bud improve`'s proposals by status (`reviews`), all counts with no ids or bodies. Devices reach it through the gateway's `ops` server. It runs as the unit in `server/mcp-ops/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/mcp-ops). |
| `bud hermes-gate <tiers file>` | The tier gate Hermes runs before every tool call (Suite D10, D48), as the `pre_tool_call` hook `bud sync` writes into `~/.hermes/config.yaml`; you don't run it yourself. It reads the call from stdin and prints `{}` (runs alone), an approve directive (Jason approves it in Hermes) or a block, from the tiers `bud sync` writes to `~/.local/state/bud/hermes-tiers.json`. See [how sync renders Hermes](https://github.com/jgarner9/bud/blob/main/docs/internals/sync.md#hermes). |

## Working on bud

| Command | What it does |
| --- | --- |
| `bud dev link [path]` | Points `~/.config/bud` at a checkout with symlinks, so edits show up on the next start. |
| `bud dev unlink` | Goes back to the config this bud ships. |

## Search and fetch

`bud search [--max N] [--time-range day|week|month|year] [--lang code] [--categories list] [--json] [--max-chars N] <query>` queries a SearXNG instance configured by `BUD_SEARXNG_URL` and returns compact, deduplicated results. `bud doctor --net` probes that endpoint and warns when it does not answer JSON searches.

`bud fetch [--raw] [--no-cache] [--allow-private] [--json] [--max-chars N] <url>` extracts readable text, with a 15-minute local cache. Private network addresses are blocked by default, including on redirects and at connection time. Use `--allow-private` only when the target is trusted.
