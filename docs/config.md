# Config reference

bud's machine config lives in `~/.local/state/bud/config.json` (`bud config path` prints it). It only holds what you changed: anything it leaves out keeps the default below. JSON has no comments, so this page is where the defaults are written down.

You rarely need to edit it by hand:

```sh
bud config ls                 # everything, defaults filled in
bud config ls --changed       # only what config.json sets
bud config get update.channel
bud config set update.channel nightly
bud config set languages '["go","lua"]'   # values are JSON, or plain strings
bud config unset update.channel           # back to the default
bud config edit                           # open it in bud's editor, then check it
```

`set` refuses a key bud doesn't know (a typo) unless you pass `--force`, and refuses a value of the wrong type or outside the allowed ones. Keys bud doesn't know are kept on every write, so a newer config is never wiped by an older bud. The settings screen (`<leader>,`, or `s` on the dashboard) changes most of these too.

## General

| Key | Default | What it does |
| --- | --- | --- |
| `profile` | `"full"` | `"full"` (desktop: every language, the app) or `"minimal"` (SSH and servers: no app, only Python and Bash to start with). |
| `terminal` | `"kitty"` | What `bud launch` and the app open: `"kitty"` or `"ghostty"`. Unset, the first one installed. |
| `layout` | `"default"` | The pane layout projects open in, a file in `~/.config/bud/layouts/`. One project can have its own (`bud layout set`). |
| `languages` | unset | The enabled language specs, by name. Unset means the profile's preset: all of bud's for `full`, the `minimal = true` ones for `minimal`. `[]` enables none. `bud lang enable/disable` edits it. |
| `format_on_save` | `true` | Format a buffer of an enabled language when it's written. `<leader>cf` formats either way. |

## Editor

| Key | Default | What it does |
| --- | --- | --- |
| `ui.messages` | `true` | The command line and messages in floats instead of the bottom line (Neovim's built-in `ui2`, still experimental in 0.12). `g<` shows messages that were cut short. Turn it off if it misbehaves; takes effect on the next start. |
| `ui.cmdline_float` | `true` | While you type a command (`;`), a search or an input, the command line is a centered float titled with what it is, and completion opens under it; it goes back to the bottom line when you're done, where the command and its messages show as before. Needs `ui.messages`. Turn it off to keep the command line at the bottom; takes effect on the next start. |
| `ui.breadcrumbs` | `true` | Path and symbol breadcrumbs at the top of file windows (dropbar.nvim); `<leader>;` picks one and opens its menu. |
| `ui.markdown` | `true` | Markdown files open rendered: headings, lists, code blocks, tables and checkboxes drawn in place, the cursor line shown as written. `false` opens them as text. Either way `<leader>um` switches one file, a file open in a diff shows its text everywhere until the diff closes, and one over 250 KB always opens as text. Applies to files opened after the change. |

## Updates

| Key | Default | What it does |
| --- | --- | --- |
| `update.channel` | `"stable"` | `"stable"` (releases) or `"nightly"` (a rolling build of main). |
| `update.auto` | `true` | On stable, install a newer release in the background at launch. `false`, or nightly, only shows a banner on the dashboard; `bud update` installs it. |

## Dashboard

| Key | Default | What it does |
| --- | --- | --- |
| `dashboard.style` | `"silly"` | `"silly"` (the robot, icons, projects with git) or `"minimal"`. `t` on the dashboard switches for now. |
| `dashboard.git` | `true` | Branch, changes and ahead/behind for each listed project. |
| `dashboard.health` | `true` | The line with the startup time (from `bud` starting to the first screen), plugins and Claude Code. |
| `dashboard.banners` | `true` | Doctor problems and updates, each with a key to fix it. |
| `dashboard.scratchpad` | `false` | `x` opens one notes file in bud's data dir. |
| `dashboard.prs` | `false` | Open PRs per project, and how many ask you for a review, through `gh`. |
| `dashboard.todos` | `false` | How many TODO and FIXME comments each project has, through `rg`. |
| `dashboard.clock` | `false` | The date and time under the robot. |
| `dashboard.animate` | `true` | The robot plays his moves one after another (wave, backflip, juggling, fireworks and more), in a random order that then repeats; after midnight he only snores. |
| `dashboard.sessions` | `false` | Save a project's open files on exit and restore them when you open it from the dashboard. |

## Claude Code

| Key | Default | What it does |
| --- | --- | --- |
| `ai.claude.autostart` | `true` | Start a Claude Code session when a project opens. Off, bud only starts one when you use an AI feature. |
| `ai.claude.resume` | `"new"` | Reopening a project starts a fresh conversation, on the model your settings choose. `"continue"` carries on bud's last conversation there instead (kept in `~/.local/state/bud/claude-sessions.json`, so never one you started yourself, or an issue, gen or chat conversation) and keeps the model that conversation was on. `bud agent` follows the same rule. |
| `ai.claude.idle_minutes` | `30` | Stop a session nothing has used for this long; the next use starts it again on the same conversation. `0` never stops one. |
| `ai.claude.args` | `[]` | Extra flags for every session, e.g. `["--model", "opus"]`. |
| `ai.claude.command` | unset | What runs Claude Code when it isn't `claude` itself: a wrapper that passes the rest on, e.g. `["headroom", "wrap", "claude", "--code-memory", "none", "--"]`. `bud install` offers exactly that when Headroom is installed, and `bud doctor` warns if the wrapper isn't on PATH. |
| `ai.claude.config` | `"home"` | Whose Claude Code config bud uses. `"home"` shares `~/.claude` with the `claude` you run yourself, so bud's sessions get its MCP servers, settings, rules and login. `"bud"` is its own, in `~/.local/share/bud/claude` (set as `CLAUDE_CONFIG_DIR` for everything bud starts): the kit, plugins, settings, history and login stay apart, so it needs its own login (`bud claude setup`), and it can't see a cloud provider set up in `~/.claude/settings.json` (then `bud claude setup` and `bud install` offer to share). When this changes, `bud kit sync` offers to take what the kit added out of the old one. The first kit sync into `~/.claude` lists what it adds and asks first, and `bud kit unsync` takes the kit back out. A `CLAUDE_CONFIG_DIR` you set yourself wins. |
| `ai.issues.team` | unset | The Linear team issues filed with `<leader>ai` go to, for projects without their own (`bud issue set --team T [--project P] [project]`). `bud issue set --global` sets it. |
| `ai.issues.project` | unset | The Linear project in that team, if any. |
| `ai.issues.bud.team` | `"JGA"` | Where issues about bud itself (`<leader>aB`) go: bud's own tracker. |
| `ai.issues.bud.project` | `"Bud IDE"` | The Linear project in that team. |
| `ai.issues.bud.checkout` | unset | The bud checkout Claude reads and works in for issues about bud. `<leader>aB` asks for it once and saves it with `bud issue checkout <path>`; it is checked each time, and asked for again if the dir was moved or removed. A `bud dev link` checkout takes precedence. |
| `ai.work.where` | `"local"` | Where `<leader>aw` has Claude fix an issue: `local` is a git worktree beside the repo; `cloud` starts a Claude Code cloud session that clones the repo from GitHub, works the issue and pushes a branch (the repo needs a GitHub `origin` with the base branch pushed). `:BudIssue work ID cloud` or `local` picks per issue. See [filing issues](issues.md#in-the-cloud). |
| `clod.repo` | unset | Your clod, the private repo with your Claude Code setup, as `owner/name`. Unset, it's `<your gh login>/clod`. Set it when gh is logged in to another account (a work one) or your repo has another name; `bud bootstrap --config` copies it across. |

## Tools and the kit

| Key | Default | What it does |
| --- | --- | --- |
| `tools.disabled` | unset | Tools turned off on this machine. `bud tools enable/disable` edits it; a tool that fails to load is added here with the reason (`bud tools ls` shows it). |
| `kit.disabled` | unset | Kit entries turned off on this machine, by name (`bud-ide@bud-plugins`, `bud-plugins`); a plugin's entry also covers the same plugin from another marketplace, so one stays off when the kit moves it. `bud kit sync` removes what bud added of them from Claude Code. `bud kit enable/disable` edits it, and enable clears an entry under an old marketplace too. |

## Environment

| Variable | What it does |
| --- | --- |
| `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` | Where bud's dirs go, on macOS too. |
| `NO_COLOR` | Plain output, no colour. |
| `CLAUDE_CONFIG_DIR` | Where Claude Code's own config is (default `~/.claude`). bud's kit and IDE bridge follow it. |
