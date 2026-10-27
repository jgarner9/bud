<div align="center">

```
   ___     z
  [-.-]  z     >_ bud
  /[_]\        > ...
   ] [         > [o.o] oh! hi. let's go.
```

**One small Go binary. One isolated Neovim. Seven languages ready to go.**

[![CI](https://github.com/jgarner9/bud/actions/workflows/ci.yml/badge.svg)](https://github.com/jgarner9/bud/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jgarner9/bud?color=6ca36c)](https://github.com/jgarner9/bud/releases/latest)
![Platforms](https://img.shields.io/badge/macOS%20%7C%20Linux-amd64%20%7C%20arm64-7a6ca3)
![Neovim](https://img.shields.io/badge/Neovim-0.12%2B-57A143?logo=neovim&logoColor=white)

</div>

---

> **Early days.** Bud is young and still booting up. The editor core works today; the dashboard, layouts, updater and Claude Code integration are on the way. Expect things to move around.

## What is it?

Bud is a fast, reproducible IDE built on Neovim and driven by a small CLI called `bud`. The binary *is* the release: the whole Neovim config ships inside it, so installing or updating means one checksummed download, and every machine ends up with exactly the same setup.

It lives in its own namespace (`NVIM_APPNAME=bud`), so your existing `~/.config/nvim` is never touched. Try it, keep it, or `bud uninstall` it without a trace.

## What you get today

|     |     |
| --- | --- |
| **An isolated Neovim** | Plugins come from Neovim's built-in `vim.pack`, pinned in a committed lockfile and lazy-loaded where it counts. Headless startup is around 28 ms. |
| **Languages as data** | One file per language in [`languages/`](languages/) wires up the LSP server, linter, formatter and tree-sitter parsers. Copy [`_template.lua`](languages/_template.lua) to add your own. |
| **Easy navigation** | fzf-lua for files, grep, buffers and symbols; two-key jumps with `gw` and `s`; a symbols outline; oil.nvim and a neo-tree side pane; four pinned files per project. |
| **Batteries included** | blink.cmp completion, gitsigns and diffview, which-key hints, format on save, Catppuccin Macchiato. |
| **A doctor** | `bud doctor` checks system deps, tool versions, config sync and machine config, and tells you the exact fix. |
| **Honest installs** | `bud install` shows the package manager command and asks before running it. Nothing gets upgraded behind your back. |

### Day-one languages

| Language | Server | Lint | Format on save |
| --- | --- | --- | --- |
| **Go** | gopls | golangci-lint | goimports |
| **Python** | basedpyright + ruff | ruff | ruff (imports, then format) |
| **TypeScript / JavaScript** | tsc (TS 7 native) + ESLint | via the servers | prettier with a Prettier config, otherwise tsc |
| **C** | clangd | clangd (+ clang-tidy) | clangd, in projects with a `.clang-format` |
| **Bash / sh** | | shellcheck | shfmt |
| **HTML** | VS Code's `html` | | prettier with a Prettier config, otherwise the server |
| **CSS / SCSS / Less** | VS Code's `cssls` | the server | prettier with a Prettier config, otherwise the server |

Pick the ones you want during `bud install`. The `minimal` profile (for SSH boxes and servers) starts with just Python and Bash, and needs no node.

## Install

Linux or macOS, amd64 or arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
```

The script downloads the release for your platform, checks it against the release's SHA-256 checksums, puts `bud` in `~/.local/bin`, and asks before running `bud install`. That asks a few questions (profile, terminal, languages), installs any missing system packages through Homebrew, pacman, apt or dnf once you say yes, then sets up the config, plugins, parsers and language tools.

<details>
<summary><b>Read the script first, pass options, or build from source</b></summary>

&nbsp;

Read before running:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh -o install.sh
less install.sh
sh install.sh
```

Options go after `sh -s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh -s -- --profile minimal --yes
```

| Option             | Default        | What it does                                  |
| ------------------ | -------------- | --------------------------------------------- |
| `--version <tag>`  | latest release | Install a specific release                    |
| `--bin-dir <dir>`  | `~/.local/bin` | Where to put the `bud` binary                 |
| `--profile <name>` | `full`         | `full` or `minimal`, handed to `bud install`  |
| `--yes`            | off            | Run `bud install` without asking              |
| `--no-install`     | off            | Install the binary, skip `bud install`        |

Without a terminal to ask on (say, a non-interactive SSH command) and without `--yes`, the script installs the binary and skips `bud install`.

From source:

```sh
git clone https://github.com/jgarner9/bud && cd bud
go install ./cmd/bud
bud install
```

</details>

## Use it

```sh
bud .               # open the current directory as a project
bud ~/code/thing    # open a project
bud main.go util.go # open files, from the project root they belong to
```

| Command         | What it does |
| --------------- | ------------ |
| `bud open`      | Opens Neovim on a directory, or on files from their project root. `bud <path>` is the same thing |
| `bud install`   | Asks for profile, terminal and languages, installs missing system packages, sets up the config, then runs `bud doctor`. `--yes` takes the defaults |
| `bud update`    | Syncs the config from this binary and restores pinned plugins, parsers and language tools |
| `bud doctor`    | Health checks; exits 1 when something's broken |
| `bud version`   | Prints the version |
| `bud uninstall` | Lists everything bud put on the machine and removes it once you confirm. `--keep-config` keeps your settings |

Global flags go before the command: `bud [--verbose] [--profile full|minimal] <command> [flags] [args]`.

### A few keys to start with

Leader is <kbd>Space</kbd>. Press it and wait, and which-key shows the rest.

| Keys | Does |
| --- | --- |
| <kbd>Space</kbd> <kbd>f</kbd> | Find file |
| <kbd>Space</kbd> <kbd>/</kbd> | Grep the project |
| <kbd>Space</kbd> <kbd>b</kbd> · <kbd>Space</kbd> <kbd>r</kbd> | Buffers · recent files |
| <kbd>g</kbd> <kbd>w</kbd> · <kbd>s</kbd> | Jump to any word · jump by character |
| <kbd>Space</kbd> <kbd>e</kbd> · <kbd>-</kbd> | File tree · edit the parent directory as a buffer |
| <kbd>Space</kbd> <kbd>m</kbd> · <kbd>Space</kbd> <kbd>1</kbd>–<kbd>4</kbd> | Pin a file · jump to a pinned file |
| <kbd>Space</kbd> <kbd>o</kbd> · <kbd>Space</kbd> <kbd>S</kbd> | Symbols outline · workspace symbols |
| <kbd>Space</kbd> <kbd>x</kbd> | Diagnostics |
| <kbd>Space</kbd> <kbd>c</kbd> <kbd>f</kbd> | Format buffer |
| <kbd>Space</kbd> <kbd>g</kbd> <kbd>d</kbd> | Diff the working tree |

## Roadmap

- [x] CLI skeleton, isolated config, installer, doctor, uninstall
- [x] Plugin set, lazy loading, the language engine and seven languages
- [ ] `bud lang` to add, enable and disable languages
- [ ] A dashboard to land on (silly by default, minimal if you prefer) and a project registry
- [ ] Self-update in the background, with doctor-based rollback
- [ ] Signed releases and a nightly channel
- [ ] Pane layouts, a Kitty/Ghostty launcher, and app bundles
- [ ] **Claude Code built in:** one session per project, shared by side-pane chat, full-screen chat, inline actions and a tool generator

The full plan and every decision made so far live in [`BASE.md`](BASE.md).

## Hacking on Bud

You'll need Go and [`just`](https://github.com/casey/just). Linting needs `stylua` and `selene` (`brew install stylua selene`).

```sh
just                 # list recipes
just check           # everything CI runs: lint, Go and Lua tests, headless smoke test
just run update      # go run ./cmd/bud update
just lang-check go   # install a language's tools in a temp dir and check it on a sample project
```

The Go side uses the standard library only, and flags always go before positional arguments. [`CLAUDE.md`](CLAUDE.md) has the architecture tour.

To release, push a `v*` tag: GoReleaser builds linux/darwin × amd64/arm64 and attaches the archives and `checksums.txt` to a GitHub release, where `install.sh` picks them up.

<div align="center">
<sub>[o.o] beep boop, built one commit at a time</sub>
</div>
