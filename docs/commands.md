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

`-y` is short for `--yes` on `install`, `uninstall`, `claude setup`, `lang enable` and `lang rm`.

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
| `bud update --no-doctor` | Skips the app rebuild and the doctor at the end, for scripts. |
| `bud update --background` | What bud starts by itself at launch, detached and at most once a day, so launching never waits on the network. It installs a newer stable release when `update.auto` is on, else only notes it for the dashboard. `--no-update` skips it. You don't normally run it. |
| `bud doctor [--quick] [--json] [--net]` | Health checks with the fix for each problem; exits 1 when something is broken. `--quick` skips the slow ones: the startup timing, which the dashboard leaves out. On a device with a profile set, it also checks what `bud sync` left: the profile and registry validate, every file the sync wrote is unchanged, which layer (public or your overlay) each piece of content comes from, and the config on disk is what this bud would render. `--net` adds the checks that go online: whether the device's channel has newer content, whether each model endpoint answers and lists the registry's models, and whether a provider that reports model facts (OpenCode Go does) disagrees with the registry about any of them, and, when `BUD_SEARXNG_URL` is set, whether that SearXNG instance answers JSON searches. The `--net` checks share one 30-second deadline. It doesn't check your MCP servers or rules, which are yours (D144). |
| `bud uninstall [--keep-config] [--yes]` | Lists everything bud put on the machine and removes it after asking. `--keep-config` keeps `config.json` and your tools. Claude Code stays (see Troubleshooting). |
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

## The AI backend

The AI backend is a setting (`ai.backend`); [AI backends](backends.md) says how to choose one. The default, `claude`, runs Claude Code directly (below); `openclaw`, `codex`, `opencode` and `custom` go over the Agent Client Protocol (ACP).

| Command | What it does |
| --- | --- |
| `bud ai status [--json]` | Which backend is set, the command it runs, whether that's installed, and for OpenClaw whether the Gateway at `ai.openclaw.url` answers, with a fix for each problem. |
| `bud agent [path]` · `bud chat` | On any backend but `claude`, open the backend's own TUI from the shell: OpenClaw's `openclaw tui` on the project's Gateway session (the one the editor uses), or on its one chat session for `bud chat`. With `ai.backend = "claude"` they're Claude Code's, below. |
| `bud ai setup [--yes]` | On the `claude` backend, points you at `bud claude setup`. On the others, installs the backend's command with npm if it's missing (asking first), then for OpenClaw asks for the Gateway's URL and the file holding its token, and saves them. Models sign in on the Gateway host (`openclaw onboard` there). |

## Claude Code

| Command | What it does |
| --- | --- |
| `bud claude status` | Whether Claude Code is installed and logged in. |
| `bud claude setup [--yes]` | Installs Claude Code with Anthropic's installer if it's missing, then runs its login. See [Claude Code in bud](claude-code.md#setting-it-up). |
| `bud agent [path]` | Claude Code's own TUI in a project, from the shell. |
| `bud chat [--resume]` | Claude Code in chat mode, outside any project. `--resume` picks an earlier chat. |
| `bud plugin path` | Unpacks the `bud-ide` plugin bud ships (if needed) and prints its dir; the editor passes it to Claude Code with `--plugin-dir`. |
| `bud tools ls` | Your tools and bud's, and whether each is on. |
| `bud tools gen "<description>"` | Has Claude write a tool or language spec, in the editor. See [tools](tools.md). |
| `bud tools enable` / `disable [--reason] <name>` | Turns a tool on or off. |
| `bud tools rm [--yes] <name>` | Deletes one of your tools, after asking. |
| `bud tools promote [--repo dir] [--yes] <name>` | Moves one of yours into a bud checkout's `ide/tools/`, commits and pushes it, after asking. `--repo` defaults to the `bud dev link` checkout. |
| `bud issue target [--bud] [--json] [dir]` | Where an issue filed from the editor goes in Linear. `--json` prints it for the editor. |
| `bud issue set --team T [--project P] [--global] [project]` / `unset [project]` | Sets or clears it. See [filing issues](issues.md). |
| `bud issue checkout [path]` / `--unset` | Saves the bud checkout `<leader>aB` works in (checked to still be a bud git checkout), forgets it, or with neither says what is saved. See [filing issues](issues.md). |

How the pieces fit together is in [Claude Code in bud](claude-code.md).

## The home server

| Command | What it does |
| --- | --- |
| `bud mcp-gateway [--config file]` | Runs the MCP gateway (Suite D72): devices on the tailnet reach Linear, Exa and other keyed MCP servers at `http://<listen>/<server>/mcp`, the gateway adds each server's key from its environment, logs every tool call and refuses cloud servers to sessions whose `X-Bud-Privacy` tier they don't allow (no header counts as P0). The config defaults to `~/.local/state/bud/mcp-gateway.json`. It runs under `op run` as the unit in `server/mcp-gateway/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/mcp-gateway). |
| `bud router [--listen addr] [--registry file]` | Runs the router: one OpenAI-compatible endpoint (`GET /v1/models`, `POST /v1/chat/completions`, `/v1/completions`, `/v1/embeddings`) over every registry provider's models, which a request names by id or as `bud/<role>`. It adds each provider's key from its environment. `--listen` defaults to `127.0.0.1:8795` and must be a loopback or tailnet IP; `--registry` defaults to the synced content's. Routing only so far: privacy tiers, spend caps and ranking aren't built. It runs under `op run` as the unit in `server/router/`. |
| `bud calendar-mcp [--listen addr] [--url url] [--tz zone]` | Runs the calendar MCP server (Suite D82) on loopback, behind the MCP gateway, which lets only P0 sessions use it: Hermes lists calendars and events, and creates, moves and deletes events, which it asks Jason about first. Only Hermes's private profile, which has no cloud model, reaches it (Suite D95). It reads iCloud over CalDAV with the Apple ID and app-specific password `op run` puts in `BUD_CALDAV_USER` and `BUD_CALDAV_PASSWORD`. Reminders are events with alarms. It runs as the unit in `server/calendar-mcp/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/calendar-mcp). |
| `bud contacts-mcp [--listen 127.0.0.1:8794]` | Runs the read-only iCloud CardDAV worker for private Hermes using its loopback bearer token. See the [server setup](https://github.com/jgarner9/bud/tree/main/server/contacts-mcp). |
| `bud mcp-ops [--listen addr]` | Runs the Bud ops MCP server (Suite D83) on loopback, `127.0.0.1:8792` by default: read-only tools for `bud doctor` (offline, without the `--net` checks) and the sync status (the content version, the overlay commit and any drift), so Hermes can say whether everything is healthy. Spend, the approval queue and the review queue are listed and say they aren't built yet. Devices reach it through the gateway's `ops` server. It runs as the unit in `server/mcp-ops/`; see [its README](https://github.com/jgarner9/bud/tree/main/server/mcp-ops). |
| `bud hermes-gate <tiers file>` | The tier gate Hermes runs before every tool call (Suite D10, D48), as the `pre_tool_call` hook `bud sync` writes into `~/.hermes/config.yaml`; you don't run it yourself. It reads the call from stdin and prints `{}` (runs alone), an approve directive (Jason approves it in Hermes) or a block, from the tiers `bud sync` writes to `~/.local/state/bud/hermes-tiers.json`. See [how sync renders Hermes](https://github.com/jgarner9/bud/blob/main/docs/internals/sync.md#hermes). |

## Working on bud

| Command | What it does |
| --- | --- |
| `bud dev link [path]` | Points `~/.config/bud` at a checkout with symlinks, so edits show up on the next start. |
| `bud dev unlink` | Goes back to the config this bud ships. |

## Search and fetch

`bud search [--max N] [--time-range day|week|month|year] [--lang code] [--categories list] [--json] [--max-chars N] <query>` queries a SearXNG instance configured by `BUD_SEARXNG_URL` and returns compact, deduplicated results. `bud doctor --net` probes that endpoint and warns when it does not answer JSON searches.

`bud fetch [--raw] [--no-cache] [--allow-private] [--json] [--max-chars N] <url>` extracts readable text, with a 15-minute local cache. Private network addresses are blocked by default, including on redirects and at connection time. Use `--allow-private` only when the target is trusted.
