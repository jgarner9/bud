# Bud IDE: BASE plan

> Status: planning draft, nothing built yet. Names like `bud` are placeholders until you pick one.
> Date: 2026-10-24

## 0. The one-paragraph version

One GitHub repo (`jgarner9/bud`) holds everything: a small **Go CLI** (`bud`) that installs, launches, updates and extends the environment, a **Neovim config** that runs in its own isolated namespace (`NVIM_APPNAME=bud`, so it never touches an existing `~/.config/nvim`), declarative **language specs** that drive LSP / lint / format / treesitter, an **AI layer** (chat, inline help, and a generator that writes new tools on the fly), a **dashboard** you land on when you run `bud` with no arguments, and **app bundles** for macOS and Linux that open your terminal of choice with a custom pane layout. A one-line `curl | sh` grabs the Go binary, and the binary does the rest. Updates are `git pull` plus a lockfile restore, either manually from settings or automatically at boot.

### Guiding principles

1. **Fast first.** Lazy-load everything, keep startup under ~50 ms, no blocking work at boot (update checks are async).
2. **One source of truth per concern.** A language is defined in exactly one file. A layout is defined in exactly one file.
3. **Reproducible.** Plugin versions are pinned by a committed lockfile, so every machine gets the same thing.
4. **Layered config.** Repo defaults < machine-local overrides < generated/user extras. Secrets never live in the repo.
5. **Go does the system stuff, Lua does the editor stuff.** The CLI installs, launches and updates. Neovim configures itself.

## Decisions made

| #   | Decision                                                                                                                     | Date       | Notes                                                                                                                                                                                                                                                                                                                                                                                                                       |
| --- | ---------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | **The Go CLI uses the standard library only**: no cobra, no third-party modules at all.                                      | 2026-10-24 | Subcommands are a switch on `os.Args[1]` with one `flag.FlagSet` per command. Flags always come before positionals (see D4), so plain `flag` parsing is enough. Help text is written by hand. Shell completions are small hand-written scripts in `completions/`, added later. Self-update uses `net/http`, `crypto/sha256`, `archive/tar` and `compress/gzip`. The AI client calls the provider API over plain `net/http`. |
| D2  | **All config is JSON** (`config.json`, tool `manifest.json`, `projects.json`, chat sessions).                                | 2026-10-24 | `encoding/json`, so it follows from D1. JSON has no comments, so the defaults are documented in `docs/config.md` and `bud config get/set` keeps you from hand-editing most of the time. Unknown keys are kept on rewrite, so a newer config never gets wiped by an older binary.                                                                                                                                            |
| D3  | **The AI kit is harness-neutral.** One `kit/` in the repo, one adapter per harness (Claude Code, OpenCode, Hermes to start). | 2026-10-24 | New harnesses are one adapter file each. The Claude subscription only works in Claude Code; other harnesses use API keys. See 4.1.                                                                                                                                                                                                                                                                                          |
| D4  | **Flags go before positional arguments.** The shape is `bud [global flags] <command> [command flags] [args]`.                | 2026-10-25 | This is plain stdlib `flag` behavior, with no interspersed parsing and no argv rewriting. `bud --layout focus .`, not `bud . --layout focus`. `bud kit adopt --from opencode context7`, not `bud kit adopt context7 --from opencode`. Anything after the first positional is treated as positional, so a flag there is passed through as an argument. `--` still works to end flags explicitly.                             |

---

## 1. Repo structure

```text
bud/
├── install.sh
│   # the one-liner target: detect OS/arch, fetch bud binary, run `bud install`
├── README.md
├── go.mod
│   # no third-party deps, so no go.sum to speak of
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
│   │   # reads/writes ~/.config/bud/config.json, merges defaults
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
│   │       # ghostty.go kitty.go wezterm.go alacritty.go iterm2.go ...
│   ├── projects/
│   │   # project registry (add/remove/list/recent), backs the dashboard
│   ├── updater/
│   │   # git fetch/pull, channel logic, binary self-replace, migrations
│   ├── bundle/
│   │   # builds the .app and .desktop / AppImage
│   ├── ai/
│   │   # provider client over plain net/http (no SDK) + tool generator prompts (shared with nvim via RPC/CLI)
│   ├── kit/
│   │   # bud kit: neutral kit reader, sync, ledger, adopt
│   │   └── adapters/
│   │       # claudecode.go opencode.go hermes.go (one per harness)
│   └── doctor/
│       # health checks
│
├── nvim/
│   # the Neovim config, symlinked to ~/.config/bud
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
│   ├── go.lua  python.lua  typescript.lua  lua.lua  rust.lua  bash.lua  ...
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
│   # harness-neutral AI kit: MCP servers, skills, agents, commands, hooks (see 4.1)
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

| What                                                           | Linux                                                    | macOS                                           |
| -------------------------------------------------------------- | -------------------------------------------------------- | ----------------------------------------------- |
| Repo clone (the source of truth)                               | `~/.local/share/bud/repo`                                | `~/Library/Application Support/bud/repo`        |
| Nvim config (symlink to `repo/nvim`)                           | `~/.config/bud`                                          | `~/.config/bud`                                 |
| Machine config (terminal, AI provider, channel, enabled langs) | `~/.config/bud/local/config.json`                        | same                                            |
| Personal generated tools (not in the repo yet)                 | `~/.local/share/bud/user-tools/`                         | `~/Library/Application Support/bud/user-tools/` |
| Bud-managed binaries + Mason packages                          | `~/.local/share/bud/bin`, `.../mason`                    | Application Support equivalents                 |
| Project registry + recent files                                | `~/.local/state/bud/projects.json`                       | same pattern                                    |
| Chat sessions (history panel)                                  | `~/.local/state/bud/chats/*.json`                        | same pattern                                    |
| Secrets (API keys)                                             | env vars or OS keychain (`secret-tool` / macOS Keychain) | Keychain                                        |

`bud` resolves all of these through `internal/paths`, so nothing else hardcodes a path.

### Key architectural choices (and why)

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

Three parts:

1. **Assistance:** chat pane (part of the default layout), "explain / refactor / write tests for selection", commit-message drafting, and optional inline completion. Provider is configurable (Claude by default, others pluggable). Keys come from env or keychain.
2. **Tool generator (`:BudGen` in nvim, `bud tools gen` in the CLI):** you describe what you want ("a command that opens the test file for the current file", "add Zig support"), the AI writes either a **tool** (`manifest.json` + `init.lua`) or a **language spec**, shows you a diff, and on accept it lands in `user-tools/` and loads immediately, no restart.
3. **Lifecycle commands** so generated stuff never rots: list, enable, disable, remove, and **promote** (move from your machine-local `user-tools/` into the repo's `tools/`, commit, push, so every machine gets it).

Guardrails: generated code runs through a headless nvim load test before it's activated, a broken tool is auto-disabled with a notification, and nothing is promoted to the repo without your explicit accept.

### 4.1 AI harness kit (`bud kit`): harness-neutral

Bud tracks one **AI kit** in the repo: the MCP servers, skills, subagents, slash commands, hooks and global instructions you want on every machine. The kit doesn't belong to any one agent tool ("harness"). Each harness gets a small **adapter** that translates the kit into that harness's own config format. `bud kit sync` runs every enabled adapter, so Claude Code, OpenCode, Hermes, or whatever comes next all get the same tools. Context7 is the first tool in the kit, and the kit grows from there.

**In the repo:**

```text
kit/
├── kit.json
│   # the index: what's in the kit, per-profile and per-harness on/off
├── mcp/
│   # one neutral JSON file per MCP server
│   └── context7.json
├── skills/
│   # SKILL.md folders (the open Agent Skills format several harnesses read)
├── agents/
│   # subagent definitions (*.md with frontmatter)
├── commands/
│   # slash commands (*.md)
├── hooks/
│   # hook scripts + a neutral hooks.json (only some harnesses support hooks)
└── INSTRUCTIONS.md
    # your global instructions ("You are Bud. Be a bud.")
```

A neutral MCP entry never holds a secret. It names the env vars it needs, and each adapter writes them in that harness's own syntax:

```json
// kit/mcp/context7.json (illustrative)
{
  "name": "context7",
  "description": "Up-to-date library docs",
  "transport": "http",
  "url": "https://mcp.context7.com/mcp",
  "headers": { "CONTEXT7_API_KEY": "$secret:CONTEXT7_API_KEY" },
  "profiles": ["full", "minimal"],
  "harnesses": ["*"]
}
```

**Adapters** live in `internal/kit/adapters/`, one Go file each, and all implement the same small interface: detect, install, sync, status and remove. What each one writes:

| Kit piece         | Claude Code                                | OpenCode                                       | Hermes Agent                                                           |
| ----------------- | ------------------------------------------ | ---------------------------------------------- | ---------------------------------------------------------------------- |
| Config written    | via `claude mcp add-json --scope user`     | `~/.config/opencode/opencode.json` (`mcp` key) | `~/.hermes/config.yaml` (`mcp_servers` key)                            |
| Secret syntax     | `${VAR}` (expanded at runtime)             | `{env:VAR}` (expanded at runtime)              | none, so the adapter writes the resolved value (file kept `chmod 600`) |
| Skills            | symlink → `~/.claude/skills/`              | symlink → `~/.config/opencode/skills/`         | symlink → Hermes skills dir                                            |
| Agents / commands | `~/.claude/agents/`, `~/.claude/commands/` | `~/.config/opencode/agents/`, `.../commands/`  | skipped until confirmed (not in its MCP docs)                          |
| Hooks             | merged into `~/.claude/settings.json`      | skipped / plugin later                         | skipped                                                                |
| Instructions      | `~/.claude/CLAUDE.md`                      | `~/.config/opencode/AGENTS.md`                 | Hermes equivalent                                                      |

Paths and key names come from each tool's current docs and get re-checked when an adapter is built. The interface is the important part: **adding a fourth harness means writing one adapter file**, not touching the kit. Pieces a harness can't use are skipped, and `bud kit status` shows that as "n/a" instead of an error.

**Sync rules (same for every adapter):**

1. Bud only touches what it owns. It keeps a ledger (`~/.local/state/bud/kit-ledger.json`) of everything it wrote per harness. When something is removed from the repo, the next sync removes it from every harness. Anything you set up by hand isn't in the ledger, so it's never touched.
2. JSON configs (Claude Code, OpenCode) are merged key by key with `encoding/json`, keeping unknown keys. Where the harness has its own CLI for adding MCP servers, Bud uses that CLI instead of editing files.
3. **YAML (Hermes):** the standard library has no YAML parser (D1), so Bud writes a **fenced block** (`# >>> bud managed >>>` … `# <<< bud managed <<<`) under `mcp_servers` and never edits anything outside it. If Hermes has a CLI for adding MCP servers, the adapter uses that instead.
4. **Secrets** are resolved from env vars or the OS keychain. If a secret is missing, sync warns and skips that tool instead of failing.

**Which harness, and how you pay for Claude.** `config.json` has `ai.harnesses` (which ones to sync) and `ai.default` (the one the editor pane and `bud agent` open). One thing matters for the choice: since February 2026, Anthropic doesn't allow a Claude Pro/Max **subscription** login in third-party harnesses. Your subscription works in Claude Code. OpenCode and Hermes use Claude through an **API key (billed per token)**, or through Bedrock, Vertex or OpenRouter, or with other models entirely. The kit works the same either way.

**Commands:**

```text
bud kit sync [--harness <name>] [--dry-run]   # write the kit into each enabled harness (also runs in bud update)
bud kit status                                 # per item × harness: synced / drifted / n/a / missing secret
bud kit ls
bud kit add mcp [--url … | --cmd …] <name>     # scaffold kit/mcp/<name>.json
bud kit add skill|agent|command <name>         # scaffold (AI can draft it, like :BudGen)
bud kit rm <name>                              # remove from the kit (next sync uninstalls it everywhere)
bud kit adopt --from <harness> <name>          # pull a hand-made tool from one harness INTO the neutral kit
bud kit enable|disable [--harness …] [--profile …] <name>

bud harness ls                                 # known adapters, installed?, version, auth ok?
bud harness install <name>                     # install Claude Code / OpenCode / Hermes
bud harness default <name>
bud agent [path]                               # open the default harness in the current project
```

**In the editor:** the default layout can have an agent pane that runs `ai.default` in a terminal split, and `<leader>a` cycles through the installed harnesses. Deeper editor integration depends on the harness. Claude Code has an IDE bridge plugin (e.g. `claudecode.nvim`) that shares your open file and selection and shows diffs in Neovim. Others get the plain terminal pane until a bridge exists.

**Candidate tools to grow the kit** (only Context7 is decided, so pick from these):

| Tool                   | Kind       | What it's for                                                                       |
| ---------------------- | ---------- | ----------------------------------------------------------------------------------- |
| ✅ Context7            | MCP (http) | current library/framework docs                                                      |
| GitHub                 | MCP        | PRs, issues, code search                                                            |
| Playwright / browser   | MCP        | drive a browser to test web UIs                                                     |
| Web search (e.g. Exa)  | MCP        | fresh research beyond docs                                                          |
| Linear / issue tracker | MCP        | tickets, if you use one                                                             |
| Basic Memory           | MCP        | your notes knowledge base (you already use it)                                      |
| `bud-dev` skill        | skill      | teaches any agent how _this_ repo works: add a language spec, a tool, a layout      |
| code-reviewer          | subagent   | a second pass over a diff before you commit                                         |
| `/commit`, `/pr`       | commands   | your house style for commit messages and PRs                                        |
| format-on-edit         | hook       | run the language formatter after the agent edits a file (Claude Code only, for now) |

---

## 5. How the program is used

### 5.1 CLI surface

```text
bud                         # open the dashboard
bud <path>                  # open a directory as a project (registers it) or a file
bud <file1> <file2> ...     # open files
bud --layout focus .        # open with a specific layout (flags before the path)
bud chat [--resume <title>] # jump straight into full-screen chat

bud install [--profile full|minimal] [--yes]   # system deps + nvim + plugins + language tooling
bud doctor                  # health checks: versions, missing tools, broken specs, keys present
bud update [--check] [--channel stable|edge]   # pull repo, restore lockfile, migrate, swap binary
bud uninstall [--keep-config]

bud projects ls | add <path> | rm <name> | pin <name>
bud lang ls | add <name> | rm <name> | enable <name> | disable <name>
bud tools ls | gen "<description>" | enable | disable | rm | promote <name>
bud kit sync | status | ls | add | rm | adopt | enable | disable
bud harness ls | install <name> | default <name>
bud agent [path]            # open the default AI harness in a project
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
2. The script detects OS + arch, downloads the matching `bud` release binary, verifies its checksum, drops it in `~/.local/bin`, and runs `bud install`.
3. `bud install` asks (or reads flags):
   - profile: **full** (desktop: everything, app bundle, fonts) or **minimal** (SSH/server: no bundle, no fonts, fewer languages)
   - terminal emulator (auto-detects what's installed)
   - which languages to enable (defaults from a preset)
   - AI provider + where the key lives (skippable)
   - AI harnesses: which to install (Claude Code / OpenCode / Hermes) and which is the default. Then it syncs the kit (Context7 etc.), prompting once for each missing secret and storing it in the keychain.
4. It clones the repo, symlinks `nvim/`, installs system deps via the OS package manager, runs a headless `nvim` to install plugins from the lockfile and Mason packages for enabled languages.
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
- ✅ **Health line:** startup time, plugin count, whether the AI provider key works.
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
- **Storage:** one JSON file per session in `~/.local/state/bud/chats/`. That's machine-local, private, and never committed. Titles are auto-generated from the first message, and you can rename them.
- **Context on demand:** `@file`, `@selection`, `@project` and `@diff` pull code into the conversation, and a "send to editor" action turns a code block from the chat into a buffer or a patch.
- **Quick chat:** besides the full-screen mode, the default layout keeps a slim side-pane chat for in-flow questions. It shares the same history store.
- **Model switcher:** pick a model/provider per chat from the header. The default comes from config.

### 5.6 App bundle + layout

- **macOS:** `Bud.app` is a tiny bundle whose executable runs `bud launch`. The bundle always opens on the dashboard, unless you dropped a folder or file onto the icon, in which case it opens that directly. `bud launch` opens your configured terminal (e.g. `ghostty -e`, `kitty --start-as`, `wezterm start`, iTerm2 via AppleScript) running `nvim` with the layout applied. Dragging a folder onto the dock icon opens it as a project.
- **Linux:** `bud.desktop` with `Exec=bud launch %F`, plus an icon. AppImage is optional and probably unnecessary since `bud` is already a single binary.
- **Layout files** describe panes in Neovim terms, e.g. default = file tree (left, 30 cols) | editor (center) | AI chat (right, 60 cols), terminal (bottom, 12 rows, hidden until toggled). Layouts are chosen per project or per launch (`--layout`).

### 5.7 Update flow

```text
boot (async, at most once per day, skippable with --no-update)
  └─ git fetch → compare to channel (stable = latest tag, edge = main)
       ├─ nothing new → silent
       └─ new version → dashboard banner (or auto-apply if config says so)

bud update  /  Settings → Update  /  [u] on dashboard
  1. refuse if the repo clone has uncommitted local changes (offer stash)
  2. git pull --ff-only to the target tag/commit
  3. if a newer bud binary is required → download release, verify, swap in place
  4. run new migrations/
  5. nvim headless: Lazy restore (lockfile) + Mason install for new language specs
  6. bud kit sync (bring every enabled harness in line with the kit)
  7. bud doctor; on failure → roll back to previous tag and report
```

Because plugin versions come from `lazy-lock.json`, "update" means "move every machine to the same known-good snapshot", not "pull the latest of 40 plugins and hope".

### 5.8 Extending, day to day

| I want to…                | Do this                                                                                                                                |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Add a language            | `bud lang add zig` (AI drafts the spec) or copy `_template.lua` → `bud install` picks up its Mason deps                                |
| Add a quick tool          | `:BudGen "…"` in the editor → review diff → active now, lives in `user-tools/`                                                         |
| Keep a tool everywhere    | `bud tools promote <name>` → moved into repo `tools/`, committed, pushed                                                               |
| Drop something            | `bud tools rm` / `bud lang disable`, or delete the file                                                                                |
| Change a layout           | edit `layouts/*.lua`, `:BudLayout reload`                                                                                              |
| Add an AI tool everywhere | `bud kit add mcp <name>` (or `bud kit adopt --from opencode <name>`) → commit → every machine and every harness gets it on next update |
| Try a new agent harness   | write one adapter in `internal/kit/adapters/`, add it to `ai.harnesses`                                                                |
| Per-machine tweak         | `bud config set …` (never committed)                                                                                                   |

---

## 6. Suggested build order

1. **Skeleton:** repo layout, `bud` with `open`, `install` (one OS), `doctor`, isolated `NVIM_APPNAME`.
2. **Editor core:** lazy.nvim + lockfile, navigation set, language engine with 3–4 languages.
3. **Dashboard (both styles) + project registry + the two-key open.**
4. **Updater** (pull, lockfile restore, binary swap, rollback).
5. **One-liner + releases** (GoReleaser, `install.sh`, minimal profile, `bootstrap`).
6. **Launcher + layouts + bundles** (macOS then Linux).
7. **AI kit + Claude Code adapter** (`bud kit sync` with Context7), then the OpenCode and Hermes adapters, then **full-screen chat + history panel**, then the side-pane chat, then the **tool generator** with lifecycle commands.
8. Polish: settings UI, more languages, docs.

---

## 7. Open questions for you

1. **Name.** Is the binary/app actually `bud`, and is the repo `jgarner9/bud`?
2. **Languages day one.** Which languages do you use now? (I assumed Go, Lua, Python, TypeScript, Bash, Rust as a starting preset.)
3. **Terminal(s).** Which terminal emulator(s) should the launcher support first? Ghostty, kitty, WezTerm, Alacritty, iTerm2?
4. **Dashboard home.** Inside Neovim (one process, fastest, my recommendation) or a separate Go TUI (Bubble Tea) that launches nvim after you pick?
5. **Layout engine.** Panes inside Neovim (my recommendation, works over SSH) or terminal-native splits / tmux / zellij?
6. **AI provider(s).** Claude only, or pluggable with others too? Do you want inline ghost-text completion, or just chat + actions?
7. **Generated tools.** Should they stay machine-local until you promote them (my recommendation), or auto-commit to the repo?
8. **Auto-update behavior.** Notify only, or auto-apply at boot? Stable tags or tracking `main`?
9. **Linux distros.** Which package managers matter: apt, dnf, pacman, all three? Is Homebrew-on-Linux acceptable as a fallback?
10. **Existing setup.** Is there a current Neovim config or dotfiles repo you want to fold in or migrate from?
11. **Secrets.** OK with env vars + OS keychain for API keys, or do you use a password manager CLI (1Password `op`, Bitwarden) you'd like wired in?
12. **Install trust.** Is `curl | sh` acceptable for you, or do you want a "download and inspect" variant and checksum/signature verification shown up front?
13. **Leader key.** Is `<Space>` OK as `<leader>`? All the global keys above assume it.
14. **Chat sync.** Should chat history stay per machine (my assumption), or sync between machines somehow (a private git repo, or a sync folder)?
15. **Dashboard extras.** Which of the "what else to show" ideas in 5.4 do you want, and any taglines you'd like in the rotation?
16. **AI kit contents.** Beyond Context7, which tools from the candidates table in 4.1 do you want, plus anything not listed?
17. **Default harness + chat backend.** Which harness is `ai.default` (Claude Code if you're on a subscription, OpenCode/Hermes if you're fine with API billing or want other models)? And should the full-screen chat run through that harness, or a direct API client (lighter, works with no harness installed)?
18. **Existing setups.** Do you already have skills, MCP servers or instructions in `~/.claude`, OpenCode or Hermes that you want `adopt`ed into the kit on day one?
