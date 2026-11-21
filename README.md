```
            ide     sync
              \     /
       server--\   /--content
                \ /
                 |
                ___
               [o.o]   b u d   s u i t e
               /[_]\
                ] [
```

<!-- the art stays outside the centered div: GitHub centers each line of a code block on its own, which breaks the alignment -->
<div align="center">

**One Go binary for the IDE, device sync and home-server tools.**

[![CI](https://github.com/jgarner9/bud/actions/workflows/ci.yml/badge.svg)](https://github.com/jgarner9/bud/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jgarner9/bud?color=6ca36c)](https://github.com/jgarner9/bud/releases/latest)
![Platforms](https://img.shields.io/badge/macOS%20%7C%20Linux-amd64%20%7C%20arm64-7a6ca3)
![Neovim](https://img.shields.io/badge/Neovim-0.12%2B-57A143?logo=neovim&logoColor=white)

**[bud.jgarner.dev](https://bud.jgarner.dev)** · [Setting up](docs/setup.md) · [Keys](docs/keys.md) · [Commands](docs/commands.md) · [Config](docs/config.md)

</div>

---

Bud Suite gives you an isolated Neovim IDE, shared agent configuration across your devices, and tools for running a home-server assistant. The `bud` binary ships the editor config and the Bud MCP servers. It renders configuration for OpenCode and Hermes, which run as separate processes.

## Install

On Linux or macOS (amd64 or arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
```

The installer verifies the release signature and checksum, installs `bud`, and offers to run `bud install`. The IDE uses Neovim 0.12+ and a [Nerd Font](https://www.nerdfonts.com) for icons. See the [setup guide](docs/setup.md) for dependencies, installer options and backend setup. See [device profiles](docs/profiles.md) to configure Suite sync.

To build from source, run `go install ./cmd/bud` from the checkout, then `bud install`.

## The IDE

```sh
bud          # open the dashboard
bud .        # open the current project
bud main.go  # open a file
```

Bud embeds its Neovim config, pins plugins in a lockfile, and runs with `NVIM_APPNAME=bud`. The editor includes project search, completion, formatting, debugging and rendered Markdown. Eight [language specs](docs/languages.md) ship with it, and [layouts](docs/layouts.md) control which panes open around the editor. Press <kbd>Space</kbd> for key hints or see [the full keymap](docs/keys.md).

Claude Code is still the default and Bud drives it directly. OpenCode, Codex and OpenClaw are available over the Agent Client Protocol (ACP). Use `bud ai status` to check the active backend and see [AI backends](docs/backends.md) for setup and differences in chat, edit review and permissions.

## Sync, updates and health

| Command | What it does |
| --- | --- |
| `bud sync` | Pulls signed content for the device's `stable` or `edge` channel, layers the private overlay, and renders runtime config. Use `--dry-run` to see what would change. |
| `bud doctor` | Checks dependencies and configuration. On a device with sync set up, it also checks the ledger and reports drift in managed files and rendered config. |
| `bud update` | Installs a signed binary from the `stable` or `nightly` channel and rolls back if a health check starts failing. Use `--check` to check for a release. |

See [all commands](docs/commands.md) and [how sync works](docs/internals/sync.md).

## Home server

- **Hermes** runs as the always-on manager. Bud renders its configuration and syncs its skills. [Setup](server/hermes/README.md)
- **MCP gateway** serves the configured tools to the runtimes that can use them. [Gateway](docs/internals/mcp-gateway.md)
- **Ops MCP server** reports Bud health, sync status and configuration drift. [Operations](docs/internals/mcp-ops.md)
- **Calendar MCP server** connects the private Hermes profile to iCloud Calendar over CalDAV, including event alarms for reminders. [Calendar](docs/internals/calendar.md)

## Repository layout

| Path | What lives there |
| --- | --- |
| [`cmd/bud/`](cmd/bud/) and [`internal/`](internal/) | The Go module and the `bud` CLI |
| [`ide/`](ide/) | Embedded Neovim config, languages, layouts and plugins |
| [`content/`](content/) | Shared instructions, skills, agents, commands and model configuration |
| [`profiles/`](profiles/) | Example device profiles |
| [`server/`](server/) | Home-server service units and setup |
| [`app/`](app/) | Plans for Herdr desktop and mobile integration |
| [`docs/`](docs/) | User guides and internals |
| [`scripts/`](scripts/) | Build, release and test scripts |

## Status

The IDE, signed device sync, Hermes integration and home-server MCP services are in the tree today. The Suite work ahead includes connecting the runtimes to shared memory, switching IDE chat between local coding and server-side planning, and integrating the Herdr desktop and mobile workspaces. See [GitHub Releases](https://github.com/jgarner9/bud/releases) for published builds.

## Hacking on Bud

Run `just check` to run the CI checks locally. See [AGENTS.md](AGENTS.md) for the architecture, tool requirements and where to find implementation notes.

## License

[MIT](LICENSE). The plugins Bud installs keep their own licenses.

<div align="center">
<sub>[o.o] beep boop</sub>
</div>
