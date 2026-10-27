# Bud IDE: BASE plan

> Status: planning draft. `open` (dirs and several files), `install` (asks the setup questions, installs missing system packages), `update` (config sync only), `doctor`, `uninstall`, `version`, the global flags, `internal/paths`, `internal/config` and the release pipeline exist; everything else is still a plan. The name `bud` is settled (D6).
> Date: 2026-10-24, updated 2026-10-26

## 0. The one-paragraph version

One GitHub repo (`jgarner9/bud`) holds everything: a small **Go CLI** (`bud`) that installs, launches, updates and extends the environment, a **Neovim config** that runs in its own isolated namespace (`NVIM_APPNAME=bud`, so it never touches an existing `~/.config/nvim`), declarative **language specs** that drive LSP / lint / format / treesitter, an **AI layer** built on Claude Code (one session per project that chat, inline help, a tool generator and the TUIs you build in Neovim all share), a **dashboard** you land on when you run `bud` with no arguments, and **app bundles** for macOS and Linux that open your terminal of choice with a custom pane layout. A one-line `curl | sh` grabs the Go binary, and the binary does the rest. The Neovim config (and later the language specs, layouts and migrations) is embedded in the binary, so the binary is the whole release: updating means swapping in a newer `bud` and syncing its embedded config, automatically in the background at boot (D9) or by hand with `bud update`. No repo clone is needed. `git` is still required, but only because Neovim's `vim.pack` uses it to fetch plugins.

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
| D5  | **The config ships embedded in the binary.** No repo clone at runtime; `~/.config/bud` is a synced copy.                     | 2026-10-25 | Repo files the editor needs (`nvim/` and `languages/`, later `layouts/`, `tools/`, `migrations/`) are pulled in by `//go:embed` in the root `embed.go`, since embed can't reach parent dirs. One binary version means one config version, updates are checksummed releases, and syncing works offline. Updating is self-update (download, verify, swap, re-exec) followed by a sync from the new binary. Synced files are bud-owned and get overwritten without warning (D21); personal changes go in machine config (D13) or `user-tools/`. Config changes ship by cutting a release; `bud dev link` covers the edit loop. Bud itself needs no `git`, but `vim.pack` still does. |
| D6  | **The name is `bud`**, the repo is `jgarner9/bud`. | 2026-10-26 | Already hardcoded in `install.sh`, `.goreleaser.yaml` and the README. |
| D7  | **Releases are signed.** `checksums.txt` gets an SSH signature from a dedicated Ed25519 release key. | 2026-10-26 | The release workflow signs with `ssh-keygen -Y sign` (key kept as an Actions secret). The signature is `checksums.txt.sig`, under the namespace `bud-release` (`ssh-keygen -Y sign -n bud-release`). `install.sh` verifies with `ssh-keygen -Y verify` against a public key pinned in the script, which works on any Mac or Linux box with OpenSSH, so no extra tool. Self-update verifies the same signature in Go with `crypto/ed25519` plus a small SSHSIG parser, keeping D1. Checksums then cover each archive. |
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
| D19 | **Neovim 0.12+ is required, and bud only reports an older version.** | 2026-10-26, raised from 0.11 on 2026-10-27 | `bud install` and `bud doctor` say what's wrong, they never upgrade nvim themselves. 0.12 is needed for `vim.pack` (D27). |
| D20 | **Self-update only replaces binaries `install.sh` put in place.** | 2026-10-26 | Anything installed another way (Homebrew, `go install`, a distro package) gets a clear refusal that names the tool to update it with. |
| D21 | **Sync overwrites hand edits to bud-owned files**, with no warning or backup. | 2026-10-26 | Same as today's `SyncConfig`. Lasting changes go in machine config or `user-tools/`. |
| D22 | **macOS and Linux only.** No Windows or WSL for now. | 2026-10-26 | |
| D23 | **Chat history is per machine for now.** | 2026-10-26 | Chat runs through Claude Code (D14), so history lives in its sessions. Syncing them between machines is a nice-to-have, not worth much effort. |
| D24 | **Start from scratch.** No existing Neovim config to migrate and no Claude Code setup to adopt. | 2026-10-26 | |
| D25 | **Opening a project or files also starts Claude Code there**, as one session per project that every bud AI UI shares. | 2026-10-26 | Neovim starts and owns it (the Go CLI execs into nvim, so it can't keep a child), right after the UI is up so startup never waits. It runs in the project root, gets the opened files as context, and is reachable from Lua through `bud.ai.claude`, so the chat pane, full-screen chat, inline actions and any TUI you build all talk to the same session. See 4.2. |
| D26 | **Claude Code only, to begin with.** bud's AI is the per-project Claude Code session (D25) plus the TUIs you build in Neovim on top of it. | 2026-10-26 | Out of scope for now: other harnesses (OpenCode, Hermes), the adapter layer, other model providers, a direct API client, and inline ghost-text completion. Auth is Claude Code's own login (subscription or API key), so bud stores no AI keys. `kit/` is Claude Code setup synced into `~/.claude` (4.1). `bud.ai.claude`'s interface avoids Claude Code specifics, so another backend could be added later without rewriting the TUIs, but nothing is built for that. |
| D27 | **Plugins are managed by Neovim's built-in `vim.pack`, not lazy.nvim.** | 2026-10-27 | lazy.nvim had no commits in 6 months; `vim.pack` ships with Neovim. Measured: lazy.nvim alone cost ~5.3 ms of a 14.6 ms headless start. `nvim-pack-lock.json` ships in the binary like the old lockfile, and `bud.pack.restore()` (run by install/update) drops plugins a release removed and checks the rest out at their locked revision. `vim.pack` has no lazy loading, so `bud.pack` has its own (`event`, `ft`, `cmd`, `keys`; JGA-107): blink.cmp and diffview load on first use, taking headless startup from ~42 ms to ~25 ms. Requires Neovim 0.12 (D19). |
| D28 | **fzf-lua is the picker.** | 2026-10-27 | Over snacks.picker, which had 5 commits in 6 months against fzf-lua's 130. Adds `fzf` as a system dependency. |
| D29 | **The plugin set for milestone 2** (reviewed for maintenance: commits in the 6 months to 2026-10-27). | 2026-10-27 | Nav: fzf-lua (D28), mini.jump2d (`gw` labels every word start with two-character labels like Helix, `s` jumps by one character then label; replaces flash.nvim), bud's own `bud.marks` for pinned files (harpoon, grapple and arrow are all unmaintained), aerial.nvim, neo-tree.nvim (+ plenary, nui, nvim-web-devicons), oil.nvim, which-key.nvim. oil owns directory buffers and `-`; neo-tree's netrw hijack is off, so it only opens as the side pane. Diagnostics list is fzf-lua's diagnostics picker on `<leader>x`, not trouble.nvim. Editor/UI/git: blink.cmp (pinned to a release tag), mini.statusline with bud's own sections, catppuccin (Macchiato), gitsigns.nvim, diffview-plus.nvim (diffview.nvim is unmaintained). Languages: nvim-lspconfig (server settings for `vim.lsp.config`), mason.nvim, conform.nvim, nvim-lint, nvim-treesitter `main` branch, whose `tree-sitter` CLI and C compiler become `installer.Deps` that install and doctor track. mini.nvim comes in as one plugin for jump2d and statusline. Debugging (nvim-dap) is deferred (JGA-108). Dropped: flash, harpoon, trouble, lualine, diffview, snacks, todo-comments. |
| D30 | **The language engine (`bud.lang`).** Specs are synced to `~/.config/bud/languages/`; a spec's `minimal = true` puts it in the minimal preset; format-on-save is on by default. | 2026-10-27 | The binary embeds `languages/*.lua` (so `_template.lua` comes along) and mounts it inside the synced `nvim/` tree. With no `languages` list in `config.json`, `full` enables every spec and `minimal` the ones marked `minimal`; Go applies the same rule to the specs it reads (D33). Enabled names with no spec are skipped quietly (kept in `bud.lang.unknown`), and a spec with an unknown field or wrong type is reported and skipped. `format_on_save` (default `true`) formats enabled languages on write with conform, falling back to the LSP server; `<leader>cf` formats by hand. Parsers and Mason packages install in `bud.pack.restore()` (install/update), never at startup; a missing server, linter or formatter is skipped without a warning. mason.nvim and nvim-lspconfig load on the first buffer of an enabled filetype, which keeps `vim.lsp` (~4 ms) and Mason (~3 ms) out of startup. `tree-sitter-cli` (0.26.1+, doctor warns when older) and a C compiler join `installer.Deps`. `dap` is accepted but not wired yet (JGA-108). |
| D31 | **Tools for Go, Python and TypeScript/JavaScript** (maintenance checked 2026-10-27). | 2026-10-27 | Go: gopls, golangci-lint, goimports (plain gofmt output; gofumpt left out). Python: basedpyright (type checking at `standard`, since its `recommended` default flags too much in code not written for it) and ruff's server for lint, with ruff formatting and sorting imports; basedpyright and ruff come from PyPI/GitHub, so the minimal profile needs no node. TypeScript/JavaScript: `tsc`, TypeScript 7's native language server (tsgo is deprecated into it), eslint's server (only attaches with an ESLint config) and prettier, which only formats in projects with a Prettier config (conform `require_cwd`), so tsc formats the rest. `lint`/`format` take a table by filetype too (`format = { go = { "goimports" } }`), since gopls formats go.mod. The smoke test runs with no languages enabled; `just lang-check` installs everything and checks each language on a sample project. |
| D32 | **Tools for C, Bash, HTML and CSS** (maintenance checked 2026-10-27). | 2026-10-27 | C: clangd, which lints (plus clang-tidy with a `.clang-tidy`) and formats in clang-format's style, but on save only in projects with a `.clang-format` (the spec's `format_markers`), since C codebases keep their own style; `<leader>cf` still formats. Bash: no language server, since bash-language-server needs node and Bash is in the minimal preset: shellcheck lints, shfmt formats on every save (with the project's `.editorconfig` when there is one). HTML and CSS (with SCSS and Less): VS Code's servers (`html`, `cssls`) from vscode-langservers-extracted, whose last release is from 2024-05 but which wraps Microsoft's maintained language services; nothing maintained matches their completion. Prettier formats them in projects with a Prettier config, as for TypeScript (D31), and the servers format the rest. nvim-treesitter has no Less parser. |
| D33 | **Go reads the specs through headless Neovim, and installs each enabled language's system deps.** | 2026-10-27 | `internal/lang` runs this binary's `bud.lang` with `nvim --clean --headless -l` and reads the specs back as JSON, so there's one format and one reader. Go's hand-kept language list and minimal preset are gone; no `languages` key in `config.json` means the profile's preset. node is no longer a base dep: a language that lists it in `system` brings it in (TypeScript, HTML, CSS), along with go and python3 (plus python3-venv on apt, for Mason's PyPI packages). Since reading specs needs nvim, `bud install` asks every question and installs once when nvim is already there, and on a fresh machine installs the base deps first, then asks for languages and installs theirs. `bud doctor` fails for a broken spec bud shipped (a bad release, worth rolling back) and only warns for a broken user spec, an enabled name with no spec, or a missing language dep. |
| D34 | **`bud lang` manages languages; your specs live in `user-tools/languages/` and are opt-in.** | 2026-10-27 | `ls` shows every spec (enabled or not, bud's or yours, broken ones with why, and enabled names with no spec). `enable` writes the list into `config.json` (starting from the preset when there's none), installs the new languages' system deps after asking, then runs `bud.pack.restore()` for their parsers and Mason packages; it refuses a spec still identical to the template, whose placeholders would go to the package manager. `disable` only edits the list: Mason packages and parsers stay. `add` copies the template into `~/.local/share/bud/user-tools/languages/` (not `~/.config/bud`, which is bud's, D21, and which `bud dev link` swaps out) and refuses a name bud ships. `rm` deletes one of yours after asking and drops it from the list; bud's own can only be disabled. Presets only cover bud's own specs (the first dir `bud.lang` reads), so a new spec never switches itself on. |
| D35 | **The startup budget is 50 ms to the first screen**, empty or on a file of any enabled language, and `bud doctor` warns past it. | 2026-10-27 | Measured on an M-series Mac with every language installed: empty start 28 ms; opening a file was 132 ms (Go), 156 ms (Python) and 214 ms (TypeScript). The causes were Neovim's python3 provider probe (60 ms, so every provider is off: bud has no remote plugins), compiling TypeScript's highlight queries (119 ms), tsc's `--version` probe when its server is enabled (43 ms) and golangci-lint's version probe when nvim-lint first runs it (57 ms). Tree-sitter highlighting, `vim.lsp.enable` and linting now start on `vim.schedule`, after the first screen, so a file shows at once and the rest arrives a moment later. Every language now opens in 40-50 ms (HTML is the slowest, from Vim's regex syntax before tree-sitter takes over). The doctor check times an empty start and one buffer per enabled language (`setfiletype`), takes the median of three, and only warns, so a slow machine never triggers an update rollback. Not a CI check: shared runners are too noisy to time. |
| D36 | **The project registry is `projects.json`: projects, pins and last-opened times only.** | 2026-10-27 | `internal/projects` reads and writes it (unknown keys kept, atomic save); `bud.projects` reads it from Lua. Recent files per project come from Neovim's own `v:oldfiles` filtered by project root, so Go is the only writer. `bud <dir>` registers the dir, and opening files registers the repo they're in when a `.git` was found (a loose file's folder isn't a project); bare `bud` registers nothing (D18). Paths are stored with symlinks resolved, and names are the dir's name, with parent dirs added while that's taken (`api`, `work/api`). A project whose dir is gone stays registered and shows as missing (an unmounted drive comes back); `bud projects rm` forgets it. `bud projects ls / add / rm / pin / unpin` manage the list, and `bud doctor` warns when the file is unreadable. |
| D37 | **The silly dashboard** (JGA-99's choices). | 2026-10-27 | `bud.dashboard` opens on VimEnter for bare `bud` (no files, an empty buffer, not `--headless`) and with `:BudDashboard`. The buddy is the README robot with moods (normal, happy when doctor is clean, update waiting, sleepy after midnight); the menu only lists keys that work today (p f r n L M d u q, plus x with `dashboard.scratchpad`), and `f`, `r` and `L` replay the global leader maps so they follow them. Up to five projects from the registry, pinned then recent, on keys 1-5, with git branch, changed count and ahead/behind; missing ones are hidden. Everything slow runs in the background and redraws when it's back: `git status` per project and `bud doctor --quick --json` (no startup timing) for the banners, through `BUD_BIN`, which `bud` sets to itself when it launches nvim. The health line's startup time runs from the top of `init.lua` to VimEnter. The tip of the day is a leader mapping with a description, a different one each day. Nerd Font glyphs accent the menu, projects, git status, banners and health line (`bud.icons`, long-standing Font Awesome / Octicons / Devicons codepoints only, one cell wide); with the robot they're bud's visual motif in the editor, while the README stays plain ASCII because GitHub has no Nerd Font. The default extras each have a toggle, all on: `dashboard.git`, `dashboard.health`, `dashboard.banners` and `dashboard.tip` (turning one off also skips its background job). Optional, off by default: `dashboard.scratchpad` (`x`, one notes file in the data dir) and `dashboard.sessions` (on exit, a registered project with files open saves a session; opening it from the dashboard restores it, otherwise the file picker opens). Opening a project from the dashboard records it as opened through `bud projects add --opened` (D39). |
| D38 | **`t` switches dashboard style for the session only.** | 2026-10-27 | `dashboard.style` (`silly` or `minimal`) in `config.json` is the default each time the dashboard opens; `t` flips it until bud closes. Keeping the toggle would mean Lua writing `config.json`, which only the Go CLI writes (D13); `bud config set dashboard.style minimal` (JGA-93) will be the lasting way. The minimal style shows the one-line robot face and `bud`, short key labels four to a row, the projects on one line and the worst doctor banner as a single icon. |
| D39 | **The two-key open is `bud.open`, shared by the dashboard and the leader keys.** | 2026-10-27 | `p` / `<leader>p` is an fzf-lua picker over registered projects (pinned, then recent), then up to ten repos found from recent files that aren't registered, then "browse for a directory…" (`vim.ui.input` with directory completion). Picking one runs `bud projects add --opened` (the CLI stays the only writer of `projects.json`, and this records it as opened), changes to it, and restores its session when `dashboard.sessions` is on, otherwise opens the file picker there. Layouts don't exist yet (milestone 6), so "opens in the default layout" waits for them. `f` / `<leader>f` lists recent files on the dashboard and the current project's files elsewhere (the registered project holding the buffer's file, else its git repo, else the cwd); typing an absolute or `~` path to an existing file opens it, read from fzf-lua's `last_query`. |
| D40 | **`<leader>M` is Mason; `<leader>m` toggles a file's pin and `<leader>0` is the unpin menu.** | 2026-10-27 | Mason gets the same letter in the editor as on the dashboard (`M`), so the pinned-files unpin menu moved off `<leader>M`: pressing `<leader>m` on a pinned file unpins it, and `<leader>0` (next to the slot keys `<leader>1`-`4`) lists the pins. `<leader>d` returns to the dashboard. `<leader>,` waits for the settings UI (JGA-94); `<leader>c` and `<leader>h` come with chat. |
| D41 | **The release key is a new, dedicated Ed25519 key, which Jason creates.** | 2026-10-27 | Answers open question 6 (JGA-103). Signature checks (Go and `install.sh`) and the signing workflow are built and tested against a throwaway test key; signing stays off, and nothing is verified against a pinned key, until the real key's public half is pinned and its private half is an Actions secret. Claude never creates or uploads the secret. |
| D42 | **Nightly only notifies; stable auto-applies.** | 2026-10-27 | Answers open question 4 (JGA-101) and narrows D9: on `stable`, a newer release is downloaded, verified and swapped in the background; on `nightly`, the boot check only raises a dashboard banner, and `u` or `bud update` applies it, since a nightly can be broken. |
| D43 | **Until the release key is pinned, self-update checks the SHA-256 only.** | 2026-10-27 | Interim, pending Jason's review (overnight run). `bud update` downloads `checksums.txt`, checks the archive against it and swaps; that's the same trust install.sh gives today (HTTPS from GitHub). Once `updater.ReleaseKey` is set, `checksums.txt.sig` is required and a missing or bad signature stops the update. |

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
│   # package bud: //go:embed for nvim/ and languages/ (later layouts/, tools/, migrations/), see D5
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
│   ├── atomicfile/
│   │   # temp-file-then-rename writes for the JSON files bud keeps
│   ├── lang/
│   │   # reads language specs through headless nvim (D33)
│   ├── installer/
│   │   # system deps: nvim, git, ripgrep, fd, fzf, tree-sitter-cli, a C compiler, fonts, plus each language's (go, python3, node)
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
│   │   # tiny: set the leader, load bud.core and bud.pack
│   ├── nvim-pack-lock.json
│   │   # COMMITTED plugin lockfile (reproducibility)
│   └── lua/bud/
│       ├── core/
│       │   # options, keymaps, autocmds, the extension loader
│       ├── plugins/
│       │   # vim.pack specs grouped by area (bud.pack loads them)
│       │   ├── editor.lua  nav.lua  ui.lua  git.lua  lang.lua  ai.lua  dashboard.lua
│       ├── lang/
│       │   # the language engine: reads the synced languages/*, says what plugins/lang.lua wires up (D30)
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
| Project registry (recent files come from Neovim's oldfiles) | `~/.local/state/bud/projects.json`                       |
| Pinned files (`bud.marks`, per project)                | `~/.local/state/bud/marks.json`                          |
| Chat history                                           | Claude Code's own session store (D23)                    |
| Secrets (MCP keys)                                     | env vars or OS keychain (`secret-tool` / macOS Keychain) |

`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` override the defaults when set.

`bud` resolves all of these through `internal/paths`, so nothing else hardcodes a path.

`~/.config/bud` holds a `.bud-manifest.json` listing every file the last sync wrote. Sync overwrites those files, deletes ones a newer release dropped, and never touches anything else in the directory. For working on bud itself, `bud dev link` replaces `~/.config/bud` with a symlink to a checkout's `nvim/` so Lua edits show up right away. Sync sees the symlink and leaves it alone until `bud dev unlink`.

### Key architectural choices (and why)

- **The binary is the release (D5).** Shipping config inside `bud` means the CLI and the editor config can never drift apart, installs need no `git`, and an update is one verified download. The cost is that a config change needs a release, which GoReleaser makes a tag push.
- **Languages are data, not code.** Each `languages/<lang>.lua` returns a table. The Lua engine wires it up, and the Go CLI can read the same file by running `nvim --headless` to dump it as JSON (so there's no second format to keep in sync).
- **Mason installs language tooling, Go installs system tooling.** Mason handles LSP servers, linters, formatters and debuggers per language spec. `bud install` handles Neovim itself, git, ripgrep, fd, a Nerd Font, and runtimes like node/python when a language needs them.
- **Plugin stack (D29):** `vim.pack` (D27), native `vim.lsp.config` + nvim-lspconfig, mason.nvim, conform.nvim (format), nvim-lint (lint), nvim-treesitter, blink.cmp (completion), fzf-lua (finding things, D28), mini.nvim (jump2d, statusline), oil.nvim (file editing as a buffer), neo-tree (file tree pane), aerial.nvim (symbols outline), which-key, gitsigns + diffview-plus, catppuccin. AI is Claude Code through `bud.ai.claude` (D26), not a chat plugin.
- **The pane layout lives inside Neovim**, not in the terminal. That way it works identically in any terminal and over SSH. An optional tmux/zellij wrapper gives persistent sessions on remote boxes.

---

## 2. Language specs (the extensibility contract)

A language file answers six questions: what files, what parser, what LSP, what linter, what formatter, what it needs installed.

```lua
-- languages/go.lua (illustrative; languages/_template.lua documents every field)
return {
  filetypes  = { "go", "gomod", "gowork", "gotmpl" },
  treesitter = { "go", "gomod", "gowork", "gosum" },
  lsp        = { gopls = { settings = { gopls = { gofumpt = true } } } },
  lint       = { "golangcilint" },        -- nvim-lint names
  format     = { "goimports", "gofumpt" }, -- conform names
  mason      = { "gopls", "golangci-lint", "goimports", "gofumpt" },
  system     = { "go" },                -- things `bud install` makes sure exist
  root       = { "go.work", "go.mod", ".git" },
  minimal    = false,                   -- true: in the minimal profile's preset (D30)
  dap        = "delve",                 -- optional debugger, not wired yet (JGA-108)
}
```

Adding a language is: `bud lang add <name>` copies `_template.lua` into `~/.local/share/bud/user-tools/languages/`, you fill it in, then `bud lang enable <name>` installs its tools (or let the AI draft it, see section 4). Bud's own specs are in every preset; yours are only on when enabled by name. Removing is `bud lang disable <name>` (or `bud lang rm <name>` for one of yours). Enabled languages per machine are a list in `config.json`, so a small SSH box can run only `bash` and `python` while your laptop runs everything (D34).

---

## 3. Navigation (what "easy navigability" means here)

| Need                                | Tool                       | Default key idea                      |
| ----------------------------------- | -------------------------- | ------------------------------------- |
| Find file / grep / buffers / recent | fzf-lua                    | `<leader>f`, `<leader>/`, `<leader>b` |
| Jump anywhere on screen             | mini.jump2d                | `gw` (every word), `s` (by character) |
| Pin hot files                       | `bud.marks` (bud's own)    | `<leader>1..4`                        |
| Symbols in file / workspace         | aerial + LSP               | `<leader>o`, `<leader>S`              |
| Go to def/refs/impl                 | LSP                        | `gd`, `gr`, `gi`                      |
| File tree vs edit-dir-as-buffer     | neo-tree (pane) + oil.nvim | `<leader>e`, `-`                      |
| Diagnostics list                    | fzf-lua diagnostics picker | `<leader>x`                           |
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
4. It installs system deps via the OS package manager, syncs the embedded `nvim/` into `~/.config/bud`, then runs a headless `nvim` to install plugins from the lockfile and Mason packages for enabled languages. No clone. `git` is only there for `vim.pack`.
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
          ___
         [o.o]        hey bud.
         /[_]\        "what are we breaking today?"
          ] [

   p   Open project        L   Plugins
   f   Open file           M   Mason
   r   Recent files        d   Doctor
   n   New file            u   Update
   x   Scratchpad          q   Quit

    Projects
   1   bud           main  3 1
   2   dollarwise    main 

    1 doctor warning, press d
    24 ms ·  18 plugins ·  claude ready
    tip: <Space>o  Symbols outline
```

Nerd Font glyphs are bud's accent in the editor, alongside the robot (D37); they're shown here as the editor draws them, so they need a Nerd Font to read. The buddy is the README's robot (D37), and its face changes with state: `[o.o]` normal, `[^.^]` once doctor comes back clean, `[O.O]` when an update is waiting, `[-.-] z` after midnight. The tagline under it rotates from `nvim/lua/bud/dashboard/taglines.lua`. Chat (`c`, `h`), the tool generator (`g`) and settings (`s`) join the menu, and a recent-chats column joins the projects, once those exist.

#### Minimal style

```text
  [o.o] bud

  p  project    f  file       r  recent     n  new
  L  plugins    M  mason      d  doctor     u  update
  t  silly      q  quit

  1 bud  2 dollarwise                             
```

Only the essentials: no art, tagline, tips or stats. Doctor's banners shrink to the worst one's icon at the end of the bottom line. `dashboard.style` in `config.json` picks the default; `t` switches between the two for the session (D38).

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
| `<leader>L` (`L` on dashboard) | Plugin list (`vim.pack`)       |
| `<leader>M` (`M` on dashboard) | Mason tool installer UI        |
| `<leader>c`                    | Full-screen chat (section 5.5) |
| `<leader>h`                    | Toggle the chat history panel  |
| `<leader>d`                    | Back to the dashboard          |
| `<leader>,`                    | Settings (once the settings UI exists, JGA-94) |

`<leader>m` pins the current file, or unpins it when it's already pinned; `<leader>0` lists the pins to remove one (D40).

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
  3. run new migrations/ (each once, recorded in state; a fresh install marks them all done)
  4. nvim headless: `bud.pack.restore()` (lockfile) + Mason install for new language specs
  5. bud kit sync (bring Claude Code's config in line with the kit)
  6. bud doctor; on failure → swap bud.prev back, re-exec it to sync its config, report
```

With no newer release, `bud update` skips step 1 and just re-syncs, which also repairs a config someone edited by hand. Because plugin versions come from `nvim-pack-lock.json`, and that file ships in the binary, "update" means "move every machine to the same known-good snapshot", not "pull the latest of 40 plugins and hope".

### 5.8 Extending, day to day

| I want to…                | Do this                                                                                                                                |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Add a language            | `bud lang add zig`, fill in the spec, `bud lang enable zig` (the AI drafting comes with the tool generator)                            |
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
2. **Editor core:** `vim.pack` + lockfile, navigation set, language engine with 3–4 languages.
3. **Dashboard (both styles) + project registry + the two-key open.**
4. **Updater** (release check, verified download, binary swap + re-exec, config sync, lockfile restore, rollback, `bud dev link`).
5. **One-liner + releases** (GoReleaser and `install.sh` exist; still to do: signing, the nightly workflow, `--profile`/`--yes`, `bootstrap`).
6. **Launcher + layouts + bundles** (macOS then Linux).
7. **The per-project Claude Code session and `bud.ai.claude` (4.2)**, then **`bud kit sync`** with Context7, then **full-screen chat + history panel**, then the side-pane chat, then the **tool generator** with lifecycle commands.
8. Polish: settings UI, more languages, docs.

---

## 7. Open questions for you

Answered questions are now decisions in the table above (dashboard extras: D37; release key: D41; nightly: D42). What's left:

1. **AI kit contents.** Beyond Context7, which tools from the candidates table in 4.1, plus anything not listed? (To discuss.)
2. **Lua in the language preset?** D10 leaves out Lua. Do you want it anyway, for editing bud's own config and tools?
3. **First chat UI.** Both run on the 4.2 session, so this is only about order: build the native buffer UI first, or ship `:BudClaude term` (Claude Code's own TUI) first and the native UI after?
4. **Sessions when switching projects.** Keep the previous project's Claude Code session running so switching back is instant (costs memory, no tokens), or stop it when you leave? I'd keep the last two or three.
5. **Several bud windows on one project.** Two Neovim instances on the same project each get their own Claude Code process. Fine as is, or should the second one attach to the first one's session?
