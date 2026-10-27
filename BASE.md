# Bud IDE: BASE plan

> Status: planning draft. `open` (dirs and several files), `install` (asks the setup questions, installs missing system packages), `update` (config sync only), `doctor`, `uninstall`, `version`, the global flags, `internal/paths`, `internal/config` and the release pipeline exist; everything else is still a plan. The name `bud` is settled (D6).
> Date: 2026-10-24, updated 2026-10-26

## 0. The one-paragraph version

One GitHub repo (`jgarner9/bud`) holds everything: a small **Go CLI** (`bud`) that installs, launches, updates and extends the environment, a **Neovim config** that runs in its own isolated namespace (`NVIM_APPNAME=bud`, so it never touches an existing `~/.config/nvim`), declarative **language specs** that drive LSP / lint / format / treesitter, an **AI layer** built on Claude Code (one session per project that chat, inline help, a tool generator and the TUIs you build in Neovim all share), a **dashboard** you land on when you run `bud` with no arguments, and **app bundles** for macOS and Linux that open your terminal of choice with a custom pane layout. A one-line `curl | sh` grabs the Go binary, and the binary does the rest. The Neovim config (and later the language specs, layouts and migrations) is embedded in the binary, so the binary is the whole release: updating means swapping in a newer `bud` and syncing its embedded config, automatically in the background at boot (D9) or by hand with `bud update`. No repo clone is needed. `git` is still required, but only because lazy.nvim uses it to fetch plugins.

### Guiding principles

1. **Fast first.** Lazy-load everything, keep startup under ~50 ms, no blocking work at boot (update checks are async).
2. **One source of truth per concern.** A language is defined in exactly one file. A layout is defined in exactly one file.
3. **Reproducible.** Plugin versions are pinned by a committed lockfile that ships inside the binary, so every machine on the same `bud` version gets the same thing.
4. **Layered config.** Shipped defaults (embedded, bud-owned) < machine-local overrides < generated/user extras. Secrets never live in the repo.
5. **Go does the system stuff, Lua does the editor stuff.** The CLI installs, launches and updates. Neovim configures itself.

## Decisions made

| #   | Decision                                                                                                                     | Date       | Notes                                                                                                                                                                                                                                                                                                                                                                                                                       |
| --- | ---------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | **The Go CLI uses the standard library only**: no cobra, no third-party modules at all.                                      | 2026-10-24 | Subcommands are a switch on `os.Args[1]` with one `flag.FlagSet` per command. Flags always come before positionals (see D4), so plain `flag` parsing is enough. Help text is written by hand. Shell completions are small hand-written scripts in `completions/`, added later. Self-update uses `net/http`, `crypto/sha256`, `archive/tar` and `compress/gzip`. There's no AI API client: Claude is reached through the Claude Code CLI (D26). |
| D2  | **All config is JSON** (`config.json`, tool `manifest.json`, `projects.json`, chat sessions).                                | 2026-10-24 | `encoding/json`, so it follows from D1. JSON has no comments, so the defaults are documented in `docs/config.md` and `bud config get/set` keeps you from hand-editing most of the time. Unknown keys are kept on rewrite, so a newer config never gets wiped by an older binary.                                                                                                                                            |
| D3  | **Superseded by D26.** ~~The AI kit is harness-neutral, with one adapter per harness (Claude Code, OpenCode, Hermes).~~ | 2026-10-24 | The kit stays, but only as Claude Code setup (4.1). |
| D4  | **Flags go before positional arguments.** The shape is `bud [global flags] <command> [command flags] [args]`.                | 2026-10-25 | This is plain stdlib `flag` behavior, with no interspersed parsing and no argv rewriting. `bud --layout focus .`, not `bud . --layout focus`. `bud kit enable --profile minimal context7`, not `bud kit enable context7 --profile minimal`. Anything after the first positional is treated as positional, so a flag there is passed through as an argument. `--` still works to end flags explicitly.                             |
| D5  | **The config ships embedded in the binary.** No repo clone at runtime; `~/.config/bud` is a synced copy.                     | 2026-10-25 | Repo files the editor needs (`nvim/`, later `languages/`, `layouts/`, `tools/`, `migrations/`) are pulled in by `//go:embed` in the root `embed.go`, since embed can't reach parent dirs. One binary version means one config version, updates are checksummed releases, and syncing works offline. Updating is self-update (download, verify, swap, re-exec) followed by a sync from the new binary. Synced files are bud-owned and get overwritten without warning (D21); personal changes go in machine config (D13) or `user-tools/`. Config changes ship by cutting a release; `bud dev link` covers the edit loop. Bud itself needs no `git`, but lazy.nvim still does. |
| D6  | **The name is `bud`**, the repo is `jgarner9/bud`. | 2026-10-26 | Already hardcoded in `install.sh`, `.goreleaser.yaml` and the README. |
| D7  | **Releases are signed.** `checksums.txt` gets an SSH signature from a dedicated Ed25519 release key. | 2026-10-26 | The release workflow signs with `ssh-keygen -Y sign` (key kept as an Actions secret). `install.sh` verifies with `ssh-keygen -Y verify` against a public key pinned in the script, which works on any Mac or Linux box with OpenSSH, so no extra tool. Self-update verifies the same signature in Go with `crypto/ed25519` plus a small SSHSIG parser, keeping D1. Checksums then cover each archive. |
| D8  | **Leader is `<Space>`**, local leader is `\`. | 2026-10-26 | Already set in `nvim/init.lua`. |
| D9  | **Updates auto-apply at boot, in the background.** Channels: `stable` (default, latest release) and `nightly`. | 2026-10-26 | At most once a day, `bud` spawns a detached check before handing off to nvim, so startup never waits on the network. A newer release is downloaded, verified and swapped in the background; the config sync runs at the next launch. `--no-update` skips it, and `update.auto = false` drops to notify-only. `nightly` is a rolling prerelease that a scheduled workflow rebuilds from `main`. |
| D10 | **Day-one languages:** Go, Python, TypeScript/JavaScript, C, Bash, HTML/CSS. | 2026-10-26 | One spec file each in `languages/` (TS and JS share one; HTML and CSS are separate files). More are added the usual way (section 2). |
| D11 | **Terminals: Kitty and Ghostty**, Kitty is the default when none is configured. | 2026-10-26 | Others (WezTerm, Alacritty, iTerm2) can come later as one launcher file each. |
| D12 | **The dashboard and pane layouts live inside Neovim.** | 2026-10-26 | One process, works over SSH and in any terminal. Keybinds stay the same on the dashboard, in the editor and in chat, so muscle memory carries across. |
| D13 | **XDG paths on macOS too**, and machine config lives in state: `~/.local/state/bud/config.json`. | 2026-10-26 | Matches where Neovim already looks, and gives one code path. Keeping machine config out of `~/.config/bud` means `bud dev link` can turn that dir into a symlink to a checkout without dragging local settings into the repo. |
| D14 | **Claude Code is the AI backend, and chat runs through it.** Narrowed by D26: no other providers or harnesses for now. | 2026-10-26 | Generated tools stay machine-local in `user-tools/` until promoted. The goal is Neovim-native TUIs that drive Claude Code, not just a terminal pane, see 4.2. |
| D15 | **Secrets come from env vars or the OS keychain only.** | 2026-10-26 | `secret-tool` on Linux, Keychain on macOS. No password-manager CLI for now. |
| D16 | **System packages:** Homebrew on macOS; on Linux pacman first, then apt, then dnf. No Homebrew-on-Linux. | 2026-10-26 | Until the installers are real, `bud install` fails with a clear list of what's missing and how to get it, instead of pretending. |
| D17 | **`install.sh` owns its own flags**, including `--profile` and `--yes`, and asks before running `bud install`. | 2026-10-26 | It reads the answer from `/dev/tty`, since stdin is the script under `curl \| sh`. With no terminal and no `--yes`, it installs the binary and skips `bud install` with a note. The profile is handed to `bud install` as `bud --profile <name> install`, with `--yes` when given, and `bud install` gets the terminal as stdin so it can ask its own questions. |
| D18 | **Bare `bud` opens the dashboard; `bud <dir>` opens and registers a project; `bud a b c` opens each file in its own buffer.** | 2026-10-26 | Bare `bud` doesn't register the current directory. |
| D19 | **Neovim 0.11+ is required, and bud only reports an older version.** | 2026-10-26 | `bud install` and `bud doctor` say what's wrong, they never upgrade nvim themselves. |
| D20 | **Self-update only replaces binaries `install.sh` put in place.** | 2026-10-26 | Anything installed another way (Homebrew, `go install`, a distro package) gets a clear refusal that names the tool to update it with. |
| D21 | **Sync overwrites hand edits to bud-owned files**, with no warning or backup. | 2026-10-26 | Same as today's `SyncConfig`. Lasting changes go in machine config or `user-tools/`. |
| D22 | **macOS and Linux only.** No Windows or WSL for now. | 2026-10-26 | |
| D23 | **Chat history is per machine for now.** | 2026-10-26 | Chat runs through Claude Code (D14), so history lives in its sessions. Syncing them between machines is a nice-to-have, not worth much effort. |
| D24 | **Start from scratch.** No existing Neovim config to migrate and no Claude Code setup to adopt. | 2026-10-26 | |
| D25 | **Opening a project or files also starts Claude Code there**, as one session per project that every bud AI UI shares. | 2026-10-26 | Neovim starts and owns it (the Go CLI execs into nvim, so it can't keep a child), right after the UI is up so startup never waits. It runs in the project root, gets the opened files as context, and is reachable from Lua through `bud.ai.claude`, so the chat pane, full-screen chat, inline actions and any TUI you build all talk to the same session. See 4.2. |
| D26 | **Claude Code only, to begin with.** bud's AI is the per-project Claude Code session (D25) plus the TUIs you build in Neovim on top of it. | 2026-10-26 | Out of scope for now: other harnesses (OpenCode, Hermes), the adapter layer, other model providers, a direct API client, and inline ghost-text completion. Auth is Claude Code's own login (subscription or API key), so bud stores no AI keys. `kit/` is Claude Code setup synced into `~/.claude` (4.1). `bud.ai.claude`'s interface avoids Claude Code specifics, so another backend could be added later without rewriting the TUIs, but nothing is built for that. |

---

## 1. Repo structure

```text
bud/
├── install.sh
│   # the one-liner target: detect OS/arch, fetch bud binary, run `bud install`
├── README.md
├── go.mod
│   # no third-party deps, so no go.sum to speak of
├── embed.go
│   # package bud: //go:embed for nvim/ (and later languages/, layouts/, tools/, migrations/), see D5
├── .goreleaser.yaml
│   # builds bud for linux/darwin × amd64/arm64, attaches to GitHub releases
├── .github/workflows/
│   # CI: go test, lua lint (stylua/selene), headless nvim smoke test, releases
│
├── cmd/bud/
├── internal/
│   # Go packages (not importable from outside)
│   ├── cli/
│   │   # one file per command (open, install, update, lang, tools, ...)
│   ├── config/
│   │   # reads/writes ~/.local/state/bud/config.json (D13), merges defaults
│   ├── paths/
│   │   # XDG paths on Linux, ~/Library on macOS, one place to resolve them
│   ├── installer/
│   │   # system deps: nvim, git, ripgrep, fd, node, fonts
│   │   ├── brew.go  apt.go  dnf.go  pacman.go
│   │   └── release.go
│   │       # fallback: download prebuilt binaries into bud's own bin dir
│   ├── launcher/
│   │   # spawns the chosen terminal with the layout
│   │   └── terminals/
│   │       # kitty.go ghostty.go first (D11); wezterm.go alacritty.go iterm2.go later
│   ├── projects/
│   │   # project registry (add/remove/list/recent), backs the dashboard
│   ├── updater/
│   │   # release check, download + verify, binary self-replace, re-exec, rollback, migrations
│   ├── bundle/
│   │   # builds the .app and .desktop / AppImage
│   ├── claude/
│   │   # finds and checks the claude CLI (installed, logged in, version) for install and doctor
│   ├── kit/
│   │   # bud kit: syncs kit/ into Claude Code's user config (~/.claude), with a ledger
│   └── doctor/
│       # health checks
│
├── nvim/
│   # the Neovim config, embedded in the binary and synced to ~/.config/bud
│   ├── init.lua
│   │   # tiny: bootstrap lazy.nvim, load bud.core
│   ├── lazy-lock.json
│   │   # COMMITTED plugin lockfile (reproducibility)
│   └── lua/bud/
│       ├── core/
│       │   # options, keymaps, autocmds, the extension loader
│       ├── plugins/
│       │   # lazy.nvim specs grouped by area
│       │   ├── editor.lua  nav.lua  ui.lua  git.lua  lsp.lua  ai.lua  dashboard.lua
│       ├── lang/
│       │   # the language engine: reads ../../languages/*, wires LSP/lint/fmt/ts
│       ├── ai/
│       │   # claude.lua: the per-project Claude Code session + the API every AI UI uses (4.2);
│       │   # inline actions, side-pane chat, tool generator UI (:BudGen)
│       ├── chat/
│       │   # full-screen chat mode + history panel + session store
│       ├── dashboard/
│       │   # custom start screen (silly + minimal styles, taglines.lua)
│       ├── layout/
│       │   # pane layout engine (reads ../../layouts/*)
│       └── settings/
│           # in-editor settings UI (update, toggle langs/tools, pick terminal)
│
├── languages/
│   # ONE FILE PER LANGUAGE (the extension point)
│   ├── _template.lua
│   ├── go.lua  python.lua  typescript.lua (TS + JS)  c.lua  bash.lua  html.lua  css.lua  (D10)
│
├── layouts/
│   # pane layouts (the "design" of the environment)
│   ├── default.lua
│   │   # e.g. tree | editor | ai, terminal along the bottom
│   ├── focus.lua
│   │   # editor only
│   └── review.lua
│       # editor + diff + git
│
├── tools/
│   # curated AI-generated or hand-written tools that were "promoted"
│   └── <tool-name>/
│       ├── manifest.json
│       │   # name, description, keymaps/commands it adds, deps
│       └── init.lua
│
├── bundles/
│   ├── macos/
│   │   # Info.plist template, icon.icns, launcher stub
│   └── linux/
│       # bud.desktop template, icons, AppImage recipe
│
├── kit/
│   # Claude Code setup: MCP servers, skills, agents, commands, hooks, instructions (see 4.1)
│
├── completions/
│   # hand-written bash/zsh/fish completions (later)
│
├── migrations/
│   # numbered scripts run by `bud update` when a release changes layout/paths
│   └── 0001_example.lua
│
└── docs/
    # how-tos: adding a language, writing a tool, layouts, troubleshooting
```

### Where things live on a machine (outside the repo)

There's no repo clone on user machines. The `bud` binary carries the shipped files, and everything below is either synced from it or machine-local.

Linux and macOS use the same XDG paths (D13), which is also where Neovim looks on both.

| What                                                   | Where                                                    |
| ------------------------------------------------------ | -------------------------------------------------------- |
| The `bud` binary (holds the embedded config)           | `~/.local/bin/bud` (or `install.sh --bin-dir`)           |
| Previous binary, kept for rollback                     | `~/.local/share/bud/bud.prev`                            |
| Nvim config (synced copy of the embedded `nvim/`)      | `~/.config/bud`                                          |
| Machine config (terminal, channel, langs, `ai.claude.*`) | `~/.local/state/bud/config.json`                         |
| Personal generated tools (not in the repo yet)         | `~/.local/share/bud/user-tools/`                         |
| Bud-managed binaries + Mason packages                  | `~/.local/share/bud/bin`, `~/.local/share/bud/mason`     |
| Project registry + recent files                        | `~/.local/state/bud/projects.json`                       |
| Chat history                                           | Claude Code's own session store (D23)                    |
| Secrets (MCP keys)                                     | env vars or OS keychain (`secret-tool` / macOS Keychain) |

`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` override the defaults when set.

`bud` resolves all of these through `internal/paths`, so nothing else hardcodes a path.

`~/.config/bud` holds a `.bud-manifest.json` listing every file the last sync wrote. Sync overwrites those files, deletes ones a newer release dropped, and never touches anything else in the directory. For working on bud itself, `bud dev link` replaces `~/.config/bud` with a symlink to a checkout's `nvim/` so Lua edits show up right away. Sync sees the symlink and leaves it alone until `bud dev unlink`.

### Key architectural choices (and why)

- **The binary is the release (D5).** Shipping config inside `bud` means the CLI and the editor config can never drift apart, installs need no `git`, and an update is one verified download. The cost is that a config change needs a release, which GoReleaser makes a tag push.
- **Languages are data, not code.** Each `languages/<lang>.lua` returns a table. The Lua engine wires it up, and the Go CLI can read the same file by running `nvim --headless` to dump it as JSON (so there's no second format to keep in sync).
- **Mason installs language tooling, Go installs system tooling.** Mason handles LSP servers, linters, formatters and debuggers per language spec. `bud install` handles Neovim itself, git, ripgrep, fd, a Nerd Font, and runtimes like node/python when a language needs them.
- **Plugin stack (proposed, all swappable):** lazy.nvim, native `vim.lsp.config` (Neovim 0.11+), mason.nvim, conform.nvim (format), nvim-lint (lint), nvim-treesitter, blink.cmp (completion), fzf-lua or snacks.picker (finding things), oil.nvim (file editing as a buffer), flash.nvim (jump), aerial.nvim (symbols outline), trouble.nvim (diagnostics list), gitsigns + a diff view, codecompanion.nvim or avante.nvim (AI chat/edits).
- **The pane layout lives inside Neovim**, not in the terminal. That way it works identically in any terminal and over SSH. An optional tmux/zellij wrapper gives persistent sessions on remote boxes.

---

## 2. Language specs (the extensibility contract)

A language file answers six questions: what files, what parser, what LSP, what linter, what formatter, what it needs installed.

```lua
-- languages/go.lua (illustrative shape, not final)
return {
  filetypes  = { "go", "gomod", "gowork", "gotmpl" },
  treesitter = { "go", "gomod", "gowork", "gosum" },
  lsp        = { gopls = { settings = { gopls = { gofumpt = true } } } },
  lint       = { "golangci-lint" },
  format     = { "goimports", "gofumpt" },
  dap        = "delve",                 -- optional debugger
  mason      = { "gopls", "golangci-lint", "goimports", "gofumpt", "delve" },
  system     = { "go" },                -- things `bud install` makes sure exist
  root       = { "go.work", "go.mod", ".git" },
}
```

Adding a language is: copy `_template.lua`, fill it in, run `bud lang add <name>` (or let the AI do it, see section 4). Removing is deleting the file or disabling it in local config. Enabled languages per machine are a list in `config.json`, so a small SSH box can run only `bash` and `python` while your laptop runs everything.

---

## 3. Navigation (what "easy navigability" means here)

| Need                                | Tool                       | Default key idea                      |
| ----------------------------------- | -------------------------- | ------------------------------------- |
| Find file / grep / buffers / recent | fzf-lua or snacks.picker   | `<leader>f`, `<leader>/`, `<leader>b` |
| Jump anywhere on screen             | flash.nvim                 | `s`                                   |
| Pin hot files                       | harpoon-style marks        | `<leader>1..4`                        |
| Symbols in file / workspace         | aerial + LSP               | `<leader>o`, `<leader>S`              |
| Go to def/refs/impl                 | LSP                        | `gd`, `gr`, `gi`                      |
| File tree vs edit-dir-as-buffer     | neo-tree (pane) + oil.nvim | `<leader>e`, `-`                      |
| Diagnostics list                    | trouble.nvim               | `<leader>x`                           |
| Switch project                      | dashboard picker           | `<leader>p`                           |

A `which-key` popup documents all of it, so the keymap is self-describing.

---

## 4. AI layer

Claude Code only, for now (D26). Every part below runs on the project's Claude Code session (4.2) and is drawn by TUIs in Neovim. Three parts:

1. **Assistance:** chat pane (part of the default layout), "explain / refactor / write tests for selection" and commit-message drafting. Claude Code's own login is the only auth.
2. **Tool generator (`:BudGen` in nvim, `bud tools gen` in the CLI):** you describe what you want ("a command that opens the test file for the current file", "add Zig support"), the AI writes either a **tool** (`manifest.json` + `init.lua`) or a **language spec**, shows you a diff, and on accept it lands in `user-tools/` and loads immediately, no restart.
3. **Lifecycle commands** so generated stuff never rots: list, enable, disable, remove, and **promote** (move from your machine-local `user-tools/` into the repo's `tools/`, commit, push, so every machine gets it).

Guardrails: generated code runs through a headless nvim load test before it's activated, a broken tool is auto-disabled with a notification, and nothing is promoted to the repo without your explicit accept.

### 4.1 Claude Code setup (`bud kit`)

The repo tracks the Claude Code setup you want on every machine: MCP servers, skills, subagents, slash commands, hooks and global instructions. `bud kit sync` writes it into Claude Code's user config, so every session bud starts (4.2), and any `claude` you run by hand, gets the same tools. Context7 is the first entry, and the kit grows from there.

**In the repo:**

```text
kit/
├── kit.json
│   # the index: what's in the kit, per-profile on/off
├── mcp/
│   # one JSON file per MCP server
│   └── context7.json
├── skills/
│   # SKILL.md folders
├── agents/
│   # subagent definitions (*.md with frontmatter)
├── commands/
│   # slash commands (*.md)
├── hooks/
│   # hook scripts + hooks.json
└── CLAUDE.md
    # your global instructions ("You are Bud. Be a bud.")
```

An MCP entry never holds a secret. It names the env vars it needs, and sync writes them as Claude Code's `${VAR}` references, which it expands at runtime:

```json
// kit/mcp/context7.json (illustrative)
{
  "name": "context7",
  "description": "Up-to-date library docs",
  "transport": "http",
  "url": "https://mcp.context7.com/mcp",
  "headers": { "CONTEXT7_API_KEY": "$secret:CONTEXT7_API_KEY" },
  "profiles": ["full", "minimal"]
}
```

**Where each piece goes:**

| Kit piece         | Written to                                             |
| ----------------- | ------------------------------------------------------ |
| MCP servers       | `claude mcp add-json --scope user`                     |
| Skills            | symlinked into `~/.claude/skills/`                     |
| Agents / commands | `~/.claude/agents/`, `~/.claude/commands/`             |
| Hooks             | merged into `~/.claude/settings.json`                  |
| Instructions      | a fenced bud-managed block in `~/.claude/CLAUDE.md`    |

These paths and commands come from Claude Code's current docs and get re-checked when sync is built.

**Sync rules:**

1. Bud only touches what it owns. It keeps a ledger (`~/.local/state/bud/kit-ledger.json`) of everything it wrote. When something is removed from the repo, the next sync removes it from Claude Code. Anything you set up by hand isn't in the ledger, so it's never touched.
2. Where `claude` has a command for something (like `claude mcp add-json`), sync uses it instead of editing files. JSON files are merged key by key with `encoding/json`, keeping unknown keys.
3. **Secrets** are resolved from env vars or the OS keychain (D15). If a secret is missing, sync warns and skips that item instead of failing.

**Commands:**

```text
bud kit sync [--dry-run]                       # write the kit into Claude Code (also runs in bud update)
bud kit status                                 # per item: synced / drifted / missing secret
bud kit ls
bud kit add mcp [--url … | --cmd …] <name>     # scaffold kit/mcp/<name>.json
bud kit add skill|agent|command <name>         # scaffold (Claude can draft it, like :BudGen)
bud kit rm <name>                              # remove from the kit (next sync uninstalls it)
bud kit adopt <name>                           # pull something you set up by hand in ~/.claude INTO the kit
bud kit enable|disable [--profile …] <name>
```

**Candidate tools to grow the kit** (only Context7 is decided, so pick from these):

| Tool                   | Kind       | What it's for                                                                  |
| ---------------------- | ---------- | ------------------------------------------------------------------------------ |
| ✅ Context7            | MCP (http) | current library/framework docs                                                 |
| GitHub                 | MCP        | PRs, issues, code search                                                       |
| Playwright / browser   | MCP        | drive a browser to test web UIs                                                |
| Web search (e.g. Exa)  | MCP        | fresh research beyond docs                                                     |
| Linear / issue tracker | MCP        | tickets, if you use one                                                        |
| Basic Memory           | MCP        | your notes knowledge base (you already use it)                                 |
| `bud-dev` skill        | skill      | teaches Claude how _this_ repo works: add a language spec, a tool, a layout    |
| code-reviewer          | subagent   | a second pass over a diff before you commit                                    |
| `/commit`, `/pr`       | commands   | your house style for commit messages and PRs                                   |
| format-on-edit         | hook       | run the language formatter after Claude edits a file                           |

### 4.2 A Claude Code session alongside the editor (D25)

Opening a project or files in bud also starts Claude Code in the same place. Every AI UI in the config talks to that one session, so Claude already knows which project you're in and what you have open, and you can build your own TUIs in Neovim on top of it.

**Who starts it, and when.** Neovim does, from `nvim/lua/bud/ai/claude.lua`. The Go CLI can't: `bud` hands its process over to nvim with `syscall.Exec`, so there's nothing left to own a child process, and starting things inside the editor is Lua's job anyway (principle 5). The session starts in a `vim.schedule` callback after `VimEnter`, so the editor is on screen before Claude Code begins loading (principle 1).

**Where it runs.**

| How bud was opened              | Claude Code's working directory                                | Context it gets                    |
| ------------------------------- | -------------------------------------------------------------- | ---------------------------------- |
| `bud <dir>`                     | that directory (the project root)                              | the project                        |
| `bud <file>` / `bud a.go b.go`  | the project root found from the files (language `root` markers, then `.git`), else the files' directory | the opened files, as `@file` context |
| bare `bud` (dashboard)          | no session until you open a project from the dashboard         |                                    |
| full-screen chat (5.5)          | its own session in `~/.local/state/bud/chat/`, not tied to code | whatever you pull in with `@file` etc. |

**What runs.** One Claude Code process per project root per Neovim instance, run as a job in headless streaming mode (`claude -p --input-format stream-json --output-format stream-json`). Lua owns the conversation, so any UI can render it however it likes. For the times you want Claude Code's own interface, `:BudClaude term` opens it in a terminal buffer on the **same session** (it resumes the session id), and closing it hands the session back to the native UIs. Only one of the two drives the session at a time.

**What it can see in the editor.** bud also runs the IDE bridge, the WebSocket protocol Claude Code uses for editor integrations (the one `claudecode.nvim` implements), and starts Claude Code pointed at it. That lets Claude Code see your open buffers, current selection and diagnostics, and lets its proposed edits show up as Neovim diffs you accept or reject.

**The API your TUIs use.** `require("bud.ai.claude")` is the only way anything in bud talks to Claude Code, so nothing spawns its own process:

```lua
local claude = require("bud.ai.claude")

claude.session()                      -- the current project's session (starting it if needed)
claude.send("explain this", { context = { "@selection" } })
claude.on("message", function(msg) ... end)   -- also: tool_use, permission_request, done, exit
claude.interrupt()                    -- stop the current reply
claude.status()                       -- "starting" | "ready" | "busy" | "stopped" | "missing"
```

The side-pane chat, full-screen chat, inline actions, `:BudGen` and any TUI you add are all just consumers of this module.

**Lifecycle.**

- **Opening a project** from the dashboard or `<leader>p` starts a session for that root, or reuses it if this Neovim already has one.
- **Reopening a project later** resumes its most recent conversation by default (`ai.claude.resume = "continue"`); set `"new"` to start fresh each time.
- **Quitting Neovim** stops every session on `VimLeavePre`. If Claude Code crashes, the status line shows it and one key restarts it on the same session.
- **Starting a session costs nothing.** No tokens are spent until you send something.

**Permissions.** Claude Code's permission questions ("allow this edit?", "run this command?") come through as `permission_request` events and appear as a Neovim prompt, using the hook headless mode provides for that (`--permission-prompt-tool`, pointed at a tool on bud's bridge). bud never loosens Claude Code's permission mode on its own.

**When Claude Code isn't there.** If `claude` isn't installed or isn't logged in, no session starts and the editor works as normal. The dashboard shows a banner with a one-key fix (`bud install`, which installs Claude Code and walks you to its login), and `bud doctor` reports it.

**Config** (machine config, D13): `ai.claude.autostart` (default `true`), `ai.claude.resume` (`"continue"` or `"new"`), and `ai.claude.args` for extra flags.

Flag names and the bridge protocol details get re-checked against Claude Code's docs when this is built, like the paths in 4.1. The module's interface avoids Claude Code specifics, so another backend could slot in later (D26), but none is planned.

---

## 5. How the program is used

### 5.1 CLI surface

```text
bud                         # open the dashboard (doesn't register the current dir)
bud <path>                  # open a directory as a project (registers it) or a file; starts Claude Code there (D25)
bud <file1> <file2> ...     # open files, one buffer each
bud --layout focus .        # open with a specific layout (flags before the path)
bud chat [--resume <title>] # jump straight into full-screen chat

bud install [--profile full|minimal] [--yes]   # system deps + nvim + plugins + language tooling
bud doctor                  # health checks: versions, missing tools, broken specs, keys present
bud update [--check] [--channel stable|nightly]   # self-update the binary, then sync its embedded config + restore lockfile, migrate
bud dev link [path] | unlink                   # point ~/.config/bud at a checkout's nvim/ for live editing
bud uninstall [--keep-config]

bud projects ls | add <path> | rm <name> | pin <name>
bud lang ls | add <name> | rm <name> | enable <name> | disable <name>
bud tools ls | gen "<description>" | enable | disable | rm | promote <name>
bud kit sync | status | ls | add | rm | adopt | enable | disable
bud agent [path]            # open Claude Code's own TUI in a project, outside Neovim
bud layout ls | set-default <name>
bud config get <key> | set <key> <value> | edit

bud launch [--terminal kitty] [--layout default] [path]   # what the app bundle calls
bud bundle [--macos|--linux]                             # build/install the app bundle locally
bud bootstrap <user@host>                                # set up a remote machine over SSH
bud version
```

Global flags: `--no-update` (skip boot check), `--verbose`, `--profile`, and `--layout` for the bare open.

**Flag order (D4):** `bud [global flags] <command> [command flags] [args]`. Global flags go before the command (`bud --verbose update --check`), command flags go right after the command name, and positionals always come last. When you type the wrong order, the help text shows the right one.

### 5.2 First-run flow

1. `curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh`
2. The script detects OS + arch, downloads the matching `bud` release binary, verifies the signature on `checksums.txt` and then the archive's checksum (D7), drops it in `~/.local/bin`, and asks before running `bud install` (skip the question with `--yes`, D17).
3. `bud install` asks (or reads flags):
   - profile: **full** (desktop: everything, app bundle, fonts) or **minimal** (SSH/server: no bundle, no fonts, fewer languages)
   - terminal emulator (auto-detects what's installed)
   - which languages to enable (defaults from a preset)
   - Claude Code: installs it if missing and checks you're logged in (`claude` runs its own login; skippable, and bud works without it). Then it syncs the kit (Context7 etc.), prompting once for each missing secret and storing it in the keychain.
4. It installs system deps via the OS package manager, syncs the embedded `nvim/` into `~/.config/bud`, then runs a headless `nvim` to install plugins from the lockfile and Mason packages for enabled languages. No clone. `git` is only there for lazy.nvim.
5. On a desktop profile, it installs the app bundle (`/Applications/Bud.app` or `~/.local/share/applications/bud.desktop`).
6. Ends with `bud doctor` output and "run `bud` to start".

### 5.3 Over SSH / new machines

- Simplest: `ssh host 'curl -fsSL .../install.sh | sh -s -- --profile minimal --yes'`
- Convenience: `bud bootstrap user@host` runs exactly that from your local machine and can optionally copy your local `config.json` (never secrets) across.
- On the remote, `bud` works the same in whatever terminal you SSH'd from. Pair with tmux/zellij if you want the session to survive disconnects.

### 5.4 Dashboard (the home screen)

Bud is meant to be the all-in-one AI + dev stop. The dashboard is where every session starts: the app bundle always opens here, and so does `bud` with no arguments. It reads the project registry written by the Go CLI, the chat history store, the async update check and `doctor` results.

There are two styles, picked with `dashboard.style = "silly" | "minimal"` in config. Pressing `t` on the dashboard swaps between them on the fly.

#### Silly style (default)

```text

        ██████╗ ██╗   ██╗██████╗
        ██╔══██╗██║   ██║██╔══██╗
        ██████╔╝██║   ██║██║  ██║
        ██╔══██╗██║   ██║██║  ██║
        ██████╔╝╚██████╔╝██████╔╝
        ╚═════╝  ╚═════╝ ╚═════╝
              (•‿•)ノ  hey bud.
      "what are we breaking today?"

   p  Open project        c  Chat with bud
   f  Open file           h  Chat history
   r  Recent files        g  Generate a tool
   n  New file            L  Lazy (plugins)
   s  Settings            M  Mason (tooling)
   u  Update              q  Quit

   Pinned                      Recent chats
    ● bud         main ✱ 3      "go embed vs fs"     2h
    ● dollarwise  main ✓        "nvim layout ideas"  1d

   ⬆ v0.4.2 available, press u     ⚠ rust-analyzer missing, press d
   ⚡ 38 ms · 41 plugins · claude ready          tip: <leader>s jumps to symbols
```

The tagline under the buddy rotates from a small list in the repo (`nvim/lua/bud/dashboard/taglines.lua`), for example "hey bud, what are we breaking today?", "compiling vibes…", "it works on my machine (this one)", "no thoughts, just keybinds". The buddy face changes with state: `(•‿•)` normal, `(°o°)` when an update is waiting, `(-_-)zz` after midnight.

#### Minimal style

```text
  bud

  p  project    f  file    c  chat    h  history
  L  lazy       s  settings           q  quit

  ● bud  ● dollarwise                 ⬆ update
```

Only the essentials: no art, no tips, no stats. Warnings shrink to a single symbol on the bottom line.

#### The two-key open

Two keys are all you need to get to work, and they behave the same everywhere (dashboard, editor, chat):

| Action         | On the dashboard | Anywhere else |
| -------------- | ---------------- | ------------- |
| Open a project | `p`              | `<leader>p`   |
| Open a file    | `f`              | `<leader>f`   |

`p` opens a fuzzy picker over registered + recent projects, plus a "browse for a directory…" entry, and picking one opens it in the default layout. `f` fuzzy-finds files in the current project, or in recent files when you're on the dashboard, and an absolute path typed into it works too.

#### Other global keys

| Key                            | Opens                          |
| ------------------------------ | ------------------------------ |
| `<leader>L` (`L` on dashboard) | Lazy plugin manager UI         |
| `<leader>M` (`M` on dashboard) | Mason tool installer UI        |
| `<leader>c`                    | Full-screen chat (section 5.5) |
| `<leader>h`                    | Toggle the chat history panel  |
| `<leader>d`                    | Back to the dashboard          |
| `<leader>,`                    | Settings                       |

#### Ideas for what else the dashboard could show

You asked for help thinking this through. These are all optional, and each can be a toggle in config. ✅ marks the ones I'd turn on by default in the silly style.

- ✅ **Git status per pinned project:** branch, dirty/clean, ahead/behind.
- ✅ **Recent chats:** the last 2–3 sessions, so you can jump right back into a thought.
- ✅ **Health line:** startup time, plugin count, whether Claude Code is installed and logged in.
- ✅ **Update and doctor banners**, each with a one-key fix.
- ✅ **Tip of the day:** a random keybind from which-key, to help you learn your own setup.
- **Open PRs / review requests** on GitHub through the `gh` CLI (off by default, it's a network call).
- **TODO/FIXME count** for the current or last project.
- **Recently generated AI tools**, with quick enable/disable.
- **Clock + date**, or a tiny calendar line.
- **Scratchpad:** one persistent quick-notes buffer, one key away (`x`).
- **Session restore:** "reopen what you had open last time in <project>".

### 5.5 Chat mode (full-screen bud)

A general-purpose chat that isn't tied to code, for bouncing ideas around. You can still pull code in whenever you want.

```text
┌─ History ──────────┐┌─ chat: "nvim layout ideas" ─────────── claude ─┐
│ Today              ││                                                │
│ ▸ nvim layout ideas││  you  should the AI pane be on the left or     │
│   go embed vs fs   ││       right?                                   │
│ Yesterday          ││                                                │
│   rust lifetimes   ││  bud  Right. Your file tree is on the left,    │
│   dinner recipes   ││       and keeping them apart means...          │
│ Pinned             ││                                                │
│ ★ bud roadmap      ││                                                │
│                    ││ ────────────────────────────────────────────── │
│ / search           ││ > _                                            │
└────────────────────┘└────────────────────────────────────────────────┘
```

- **Opening it:** `c` from the dashboard, `<leader>c` from anywhere, or `bud chat` from the shell. It takes over the whole screen as a separate tab, so your editor layout is left exactly as it was. `<leader>c` again (or `q` in normal mode) takes you back.
- **History panel:** toggle it with `<leader>h`. Sessions are grouped by day, pinned ones stay at the top, and `/` searches titles and contents. In the panel, `Enter` resumes, `r` renames, `d` deletes, `p` pins, and `n` starts a new chat.
- **Storage:** chat runs through Claude Code (D14), so sessions live in its own store and the history panel reads them from there. They're per machine for now (D23) and never committed. Titles are auto-generated from the first message, and you can rename them.
- **Context on demand:** `@file`, `@selection`, `@project` and `@diff` pull code into the conversation, and a "send to editor" action turns a code block from the chat into a buffer or a patch.
- **Quick chat:** besides the full-screen mode, the default layout keeps a slim side-pane chat for in-flow questions. It shares the same history store.
- **Model switcher:** pick a Claude model per chat from the header (handed to Claude Code as `--model`). The default comes from config.

### 5.6 App bundle + layout

- **macOS:** `Bud.app` is a tiny bundle whose executable runs `bud launch`. The bundle always opens on the dashboard, unless you dropped a folder or file onto the icon, in which case it opens that directly. `bud launch` opens your configured terminal (Kitty or Ghostty, Kitty when none is set, D11) running `nvim` with the layout applied. Dragging a folder onto the dock icon opens it as a project.
- **Linux:** `bud.desktop` with `Exec=bud launch %F`, plus an icon. AppImage is optional and probably unnecessary since `bud` is already a single binary.
- **Layout files** describe panes in Neovim terms, e.g. default = file tree (left, 30 cols) | editor (center) | AI chat (right, 60 cols), terminal (bottom, 12 rows, hidden until toggled). Layouts are chosen per project or per launch (`--layout`).

### 5.7 Update flow

```text
boot (detached, at most once per day, skippable with --no-update, D9)
  └─ ask GitHub releases for the channel's newest version
     (stable = latest release, nightly = rolling prerelease from main) and compare to `bud version`
       ├─ nothing new → silent
       └─ new version → steps 1–2 below in the background, the rest at next launch
                        (update.auto = false → dashboard banner instead)

bud update  /  Settings → Update  /  [u] on dashboard
  1. if a newer release exists:
       download the archive, checksums.txt and its signature; verify the signature, then the SHA-256 (D7)
       keep the running binary as bud.prev, swap the new one in place (rename, so it's atomic)
       re-exec the NEW binary for the rest (the old one still holds the old embedded config)
  2. sync the embedded nvim/ into ~/.config/bud (overwrite managed files, drop removed ones)
  3. run new migrations/
  4. nvim headless: Lazy restore (lockfile) + Mason install for new language specs
  5. bud kit sync (bring Claude Code's config in line with the kit)
  6. bud doctor; on failure → swap bud.prev back, re-exec it to sync its config, report
```

With no newer release, `bud update` skips step 1 and just re-syncs, which also repairs a config someone edited by hand. Because plugin versions come from `lazy-lock.json`, and that file ships in the binary, "update" means "move every machine to the same known-good snapshot", not "pull the latest of 40 plugins and hope".

### 5.8 Extending, day to day

| I want to…                | Do this                                                                                                                                |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Add a language            | `bud lang add zig` (AI drafts the spec) or copy `_template.lua` → `bud install` picks up its Mason deps                                |
| Add a quick tool          | `:BudGen "…"` in the editor → review diff → active now, lives in `user-tools/`                                                         |
| Keep a tool everywhere    | `bud tools promote <name>` → moved into repo `tools/`, committed, pushed → ships in the next release                                   |
| Drop something            | `bud tools rm` / `bud lang disable`, or delete the file                                                                                |
| Change a layout           | edit `layouts/*.lua` in the repo (with `bud dev link`, `:BudLayout reload` shows it live) → ships in the next release                 |
| Ship config changes       | commit, push a `v*` tag → GoReleaser builds the release → every machine gets it on `bud update`                                        |
| Add an AI tool everywhere | `bud kit add mcp <name>` (or `bud kit adopt <name>`) → commit → release → every machine gets it on update                              |
| Build a Claude TUI        | write a Lua module on `require("bud.ai.claude")` (4.2), in `nvim/lua/bud/` to ship it or `user-tools/` to keep it local         |
| Per-machine tweak         | `bud config set …` (never committed)                                                                                                   |

---

## 6. Suggested build order

1. **Skeleton:** repo layout, `bud` with `open`, `install` (one OS), `doctor`, isolated `NVIM_APPNAME`.
2. **Editor core:** lazy.nvim + lockfile, navigation set, language engine with 3–4 languages.
3. **Dashboard (both styles) + project registry + the two-key open.**
4. **Updater** (release check, verified download, binary swap + re-exec, config sync, lockfile restore, rollback, `bud dev link`).
5. **One-liner + releases** (GoReleaser and `install.sh` exist; still to do: signing, the nightly workflow, `--profile`/`--yes`, `bootstrap`).
6. **Launcher + layouts + bundles** (macOS then Linux).
7. **The per-project Claude Code session and `bud.ai.claude` (4.2)**, then **`bud kit sync`** with Context7, then **full-screen chat + history panel**, then the side-pane chat, then the **tool generator** with lifecycle commands.
8. Polish: settings UI, more languages, docs.

---

## 7. Open questions for you

Answered questions are now decisions D6–D24 above. What's left:

1. **AI kit contents.** Beyond Context7, which tools from the candidates table in 4.1, plus anything not listed? (To discuss.)
2. **Dashboard extras.** Which of the "what else to show" ideas in 5.4 do you want, and any taglines for the rotation?
3. **Lua in the language preset?** D10 leaves out Lua. Do you want it anyway, for editing bud's own config and tools?
4. **Auto-apply on nightly.** D9 auto-applies on whichever channel you're on. Is that right for `nightly`, or should nightly only notify?
5. **First chat UI.** Both run on the 4.2 session, so this is only about order: build the native buffer UI first, or ship `:BudClaude term` (Claude Code's own TUI) first and the native UI after?
6. **Release signing key.** A new Ed25519 key just for releases (recommended, so your personal git signing key never sits in CI), or reuse an existing one?
7. **Sessions when switching projects.** Keep the previous project's Claude Code session running so switching back is instant (costs memory, no tokens), or stop it when you leave? I'd keep the last two or three.
8. **Several bud windows on one project.** Two Neovim instances on the same project each get their own Claude Code process. Fine as is, or should the second one attach to the first one's session?
