```
   ___     z
  [-.-]  z     >_ bud
  /[_]\        > ...
   ] [         > [o.o] oh! hi. let's go.
```

<!-- the art stays outside the centered div: GitHub centers each line of a code block on its own, which breaks the alignment -->
<div align="center">

**One small Go binary. One isolated Neovim. Eight languages ready to go.**

[![CI](https://github.com/jgarner9/bud/actions/workflows/ci.yml/badge.svg)](https://github.com/jgarner9/bud/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jgarner9/bud?color=6ca36c)](https://github.com/jgarner9/bud/releases/latest)
![Platforms](https://img.shields.io/badge/macOS%20%7C%20Linux-amd64%20%7C%20arm64-7a6ca3)
![Neovim](https://img.shields.io/badge/Neovim-0.12%2B-57A143?logo=neovim&logoColor=white)

</div>

---

## Install

Linux or macOS, amd64 or arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
```

The script downloads the release for your platform, checks its signature and checksum, puts `bud` in `~/.local/bin`, and offers to run `bud install`. That asks for a profile, terminal and languages, shows the package manager command for anything missing and runs it once you agree, then sets up the config, plugins and language tools, Claude Code and clod.

<details>
<summary><b>Read the script first, pass options, or build from source</b></summary>

&nbsp;

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh -o install.sh
less install.sh
sh install.sh
```

Options go after `sh -s --`, e.g. `curl -fsSL .../install.sh | sh -s -- --profile minimal --yes`:

| Option             | Default        | What it does                                 |
| ------------------ | -------------- | -------------------------------------------- |
| `--version <tag>`  | latest release | Install a specific release                   |
| `--bin-dir <dir>`  | `~/.local/bin` | Where to put the `bud` binary                |
| `--profile <name>` | `full`         | `full` or `minimal`, handed to `bud install` |
| `--yes`            | off            | Run `bud install` without asking             |
| `--no-install`     | off            | Install the binary, skip `bud install`       |

Without a terminal to ask on and without `--yes`, the script installs the binary and skips `bud install`.

From source:

```sh
git clone https://github.com/jgarner9/bud && cd bud
go install ./cmd/bud
bud install
```

</details>

## What is it?

Bud is a fast, reproducible IDE: Neovim plus a small Go CLI that installs, launches, updates and syncs it. The whole Neovim config ships inside the binary, so every machine gets exactly the same setup. It runs as its own Neovim (`NVIM_APPNAME=bud`) and never touches `~/.config/nvim`; `bud uninstall` removes it without a trace.

## Features

### The editor

Neovim 0.12 with plugins pinned in a committed lockfile and lazy-loaded, inside a 50 ms budget to the first screen.

- **Navigation:** fzf-lua for files, grep, buffers and symbols; two-key jumps (`gw`, `s`); a symbols outline; oil.nvim as the explorer; four pinned files per project.
- **Editing:** blink.cmp completion, format on save, gitsigns and diffview, debugging with nvim-dap.
- **Interface:** key hints as you type, a floating command line, breadcrumbs, Catppuccin Macchiato.
- **Markdown:** `.md` files open rendered (up to 250 KB); `<leader>um` switches to plain text for editing (`ui.markdown` turns rendering off).

All keys: [docs/keys.md](docs/keys.md).

### Languages

One Lua file per language wires up its parsers, LSP server, linter and formatter. Eight ship with bud:

| Language | Server | Lint | Format on save |
| --- | --- | --- | --- |
| **Go** | gopls | golangci-lint | goimports |
| **Python** | ty + ruff | ruff | ruff |
| **TypeScript / JavaScript** | tsc + ESLint | the servers | prettier, else tsc |
| **C** | clangd | clangd + clang-tidy | clangd, with a `.clang-format` |
| **Bash / sh** | | shellcheck | shfmt |
| **HTML** | `html` | | prettier, else the server |
| **CSS / SCSS / Less** | `cssls` | the server | prettier, else the server |
| **Lua** | lua_ls | selene | stylua, with a `stylua.toml` |

Prettier runs only in projects with a Prettier config. The `minimal` profile (for servers) starts with Python and Bash. Add your own with `bud lang add`: [docs/languages.md](docs/languages.md).

### Layouts

A layout names the panes that open around the editor: explorer, git, Claude Code, terminal, debugger. bud ships `default`, `focus`, `debug` and `review`; each project can have its own. [docs/layouts.md](docs/layouts.md).

### Claude Code

Each project gets one Claude Code session, shared by everything that uses it:

- **The pane** (`<leader>ac`): Claude Code's own TUI beside your code.
- **Inline actions:** explain, refactor or write tests for a selection; draft commit messages.
- **Reviewed edits:** every change Claude proposes opens as a diff to accept or reject.
- **Chat** (`<leader>ch`, `bud chat`): chat mode, outside any project.
- **Issues:** turn a note into Linear issues, and have Claude fix one in a worktree.

[docs/claude-code.md](docs/claude-code.md) explains how it fits together; [docs/issues.md](docs/issues.md), [docs/tools.md](docs/tools.md) and [docs/claude-tui.md](docs/claude-tui.md) go further.

### Dashboard and projects

Bare `bud` opens a dashboard: your projects with their git state, a health line, and banners for doctor problems and updates, each with a one-key fix. Opening a directory registers it as a project; `bud projects` pins and manages them.

### Tools

Small Neovim extensions in a directory of their own, loaded after the first screen. Write one, or have Claude write and test it with `:BudGen`. [docs/tools.md](docs/tools.md).

### Updates and health

- **`bud doctor`** checks dependencies, languages, config sync, startup time and Claude Code, with the fix for each problem.
- **`bud update`** installs signed releases (stable or nightly), checks itself afterwards and rolls back if a check breaks. Stable updates install in the background.
- **Honest installs:** bud shows the package manager command and asks first, and never upgrades anything behind your back.

## Usage

```sh
bud                 # the dashboard
bud .               # open the current directory as a project
bud ~/code/thing    # open a project
bud main.go util.go # open files from the project they belong to
```

### Commands

| Command | What it does |
| --- | --- |
| `bud install` | Set up: profile, terminal, languages, system packages, config |
| `bud update` | Install a newer release and sync everything; `--check` only looks |
| `bud doctor` | Health checks, each with its fix |
| `bud lang` | List, enable, disable and add languages |
| `bud layout` | List layouts and pick the default |
| `bud projects` | List, add, remove and pin projects |
| `bud config` | Read and write settings by key |
| `bud claude` | Check and set up Claude Code |
| `bud chat` · `bud agent` | Claude Code outside the editor |
| `bud kit` | Sync bud's Claude Code plugins |
| `bud tools` | Manage editor tools |
| `bud launch` · `bud bundle` | Open bud in a new terminal window · install the desktop app |
| `bud uninstall` | Remove everything bud installed |

Global flags go before the command: `bud [--verbose] [--profile full|minimal] <command> [flags] [args]`. Every command and flag: [docs/commands.md](docs/commands.md). Shell completion: `eval "$(bud completion zsh)"`.

### First keys

Leader is <kbd>Space</kbd>; press it and wait to see what follows.

| Keys | Does |
| --- | --- |
| <kbd>;</kbd> | The command line |
| <kbd>Space</kbd> <kbd>p</kbd> · <kbd>Space</kbd> <kbd>f</kbd> | Open a project · find a file |
| <kbd>Space</kbd> <kbd>/</kbd> | Grep the project |
| <kbd>Space</kbd> <kbd>e</kbd> · <kbd>Space</kbd> <kbd>t</kbd> | Explorer · terminal |
| <kbd>Space</kbd> <kbd>a</kbd> <kbd>c</kbd> | Claude Code's pane |
| <kbd>Space</kbd> <kbd>u</kbd> <kbd>m</kbd> | Rendered or plain markdown |
| <kbd>j</kbd> <kbd>k</kbd> | Leave Insert mode |
| <kbd>Space</kbd> <kbd>,</kbd> | Settings |
| <kbd>Space</kbd> <kbd>q</kbd> <kbd>q</kbd> | Quit |

The rest: [docs/keys.md](docs/keys.md).

### Settings

`<leader>,` opens the settings screen; `bud config` does the same from the shell. Every key and its default: [docs/config.md](docs/config.md). When something's off, start with `bud doctor`, then [docs/troubleshooting.md](docs/troubleshooting.md).

## Roadmap

- [x] CLI, isolated config, installer, doctor, uninstall
- [x] Plugins, lazy loading, the language engine and eight languages
- [x] Dashboard and project registry
- [x] Background self-update with rollback, signed releases, nightly channel
- [x] Layouts, a Kitty/Ghostty launcher, app bundles
- [x] Claude Code built in: per-project sessions, its TUI, inline actions, reviewed edits, chat
- [x] Settings screen, `bud config`, completions, docs, debugging

The full plan and every decision are in [`BASE.md`](BASE.md).

## Hacking on Bud

You'll need Go and [`just`](https://github.com/casey/just); linting needs `stylua` and `selene`.

```sh
just                 # list recipes
just check           # everything CI runs
just run update      # go run ./cmd/bud update
just lang-check go   # check one language's tooling on a sample project
```

The Go side is standard library only, and flags go before positional arguments. [`CLAUDE.md`](CLAUDE.md) has the architecture tour. `bud dev link` points your editor at a checkout.

Releases come from `VERSION`: run Claude Code's `/bud:release` (from [bud-plugins](https://github.com/jgarner9/bud-plugins)), which reviews everything since the last tag and bumps it on `main`; CI tags the release and GoReleaser builds it.

## License

[MIT](LICENSE). The plugins bud installs keep their own licenses.

<div align="center">
<sub>[o.o] beep boop, built one commit at a time</sub>
</div>
