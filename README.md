# Bud IDE

> Early work in progress. Expect things to move around.

Bud is a fast, reproducible Neovim-based IDE driven by a small Go CLI. It runs in its own namespace (`NVIM_APPNAME=bud`), so it never touches an existing `~/.config/nvim`.

## What it will feature

- **One CLI, `bud`.** Installs, launches, updates and extends the environment. Pure Go standard library, no third-party modules.
- **An isolated Neovim config.** Built on lazy.nvim with plugin versions pinned in a committed lockfile, so every machine gets the same setup.
- **Languages as data.** One file per language in `languages/` sets up LSP, linting, formatting and treesitter. To add a language, copy the template and fill it in.
- **Easy navigation.** Fuzzy finding, jump-anywhere motions, symbol outlines, a file tree, and project switching.
- **Claude Code built in.** Opening a project also starts a Claude Code session there. Side-pane and full-screen chat, inline help, a generator that writes new editor tools, and your own Neovim TUIs all share that session. Your MCP servers, skills and commands sync into Claude Code on every machine.
- **A dashboard.** The start screen you land on when you run `bud` with no arguments, with recent projects one keypress away.
- **Pane layouts.** Named layouts (`default`, `focus`, `review`) that open in your terminal of choice.
- **App bundles.** A `.app` on macOS and a `.desktop` file or AppImage on Linux.
- **Simple updates.** `git pull` plus a lockfile restore, run by hand or automatically at startup in the background.
- **One-line install.** `curl | sh` fetches the binary, and the binary handles the rest.

## Install

Linux or macOS (amd64/arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
```

The script downloads the release binary for your platform, checks it against the release's SHA-256 checksums, puts it in `~/.local/bin`, and asks before running `bud install`, which checks your system dependencies and sets up the Bud Neovim config. If anything is missing, `bud install` lists it with the exact install command for your package manager (Homebrew on macOS; pacman, apt or dnf on Linux).

To read the script before running it:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh -o install.sh
less install.sh
sh install.sh
```

Options go after `sh -s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh -s -- --version v0.1.0 --bin-dir ~/bin --no-install
```

| Option             | Default        | What it does                                  |
| ------------------ | -------------- | --------------------------------------------- |
| `--version <tag>`  | latest release | Install a specific release                    |
| `--bin-dir <dir>`  | `~/.local/bin` | Where to put the `bud` binary                 |
| `--profile <name>` | `full`         | `full` or `minimal` (not used by bud yet)     |
| `--yes`            | off            | Run `bud install` without asking              |
| `--no-install`     | off            | Install the binary, skip `bud install`        |

Without a terminal to ask on (for example over a non-interactive SSH command) and without `--yes`, the script installs the binary and skips `bud install`.

From source:

```sh
git clone https://github.com/jgarner9/bud && cd bud
go install ./cmd/bud
bud install
```

## Status

Working today:

| Command       | What it does                                              |
| ------------- | --------------------------------------------------------- |
| `bud open`    | Launches Neovim with the Bud config and resolves the path |
| `bud install` | Checks system dependencies and installs the Bud Neovim config |
| `bud update`  | Syncs the config and restores pinned plugins              |
| `bud doctor`  | Checks deps, the Neovim version (0.11+), config sync and machine config; exits 1 on a failure |
| `bud version` | Prints the version                                        |

Everything else in the list above is still planned. See [`BASE.md`](BASE.md) for the full plan and the decisions made so far.

## Development

Requires Go and [`just`](https://github.com/casey/just).

```sh
just run       # go run ./cmd/bud
just build     # go build ./cmd/bud
just install   # go install ./cmd/bud
go test ./...
```

Flags go before positional arguments: `bud [global flags] <command> [command flags] [args]`.

### Releasing

Push a `v*` tag. The release workflow runs GoReleaser, which builds `bud` for linux/darwin × amd64/arm64 and attaches the archives and `checksums.txt` to a GitHub release, where `install.sh` picks them up.

```sh
git tag v0.1.0 && git push origin v0.1.0
```
