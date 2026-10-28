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
| `profile` | `"full"` | `"full"` (desktop: everything, the app, fonts) or `"minimal"` (SSH and servers: no app, fewer languages). |
| `terminal` | `"kitty"` | What `bud launch` and the app open: `"kitty"` or `"ghostty"`. Unset, the first one installed. |
| `layout` | `"default"` | The pane layout projects open in, a file in `~/.config/bud/layouts/`. One project can have its own (`bud layout set`). |
| `languages` | unset | The enabled language specs, by name. Unset means the profile's preset: all of bud's for `full`, the `minimal = true` ones for `minimal`. `[]` enables none. `bud lang enable/disable` edits it. |
| `format_on_save` | `true` | Format a buffer of an enabled language when it's written. `<leader>cf` formats either way. |

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
| `dashboard.health` | `true` | The line with the startup time, plugins and Claude Code. |
| `dashboard.banners` | `true` | Doctor problems and updates, each with a key to fix it. |
| `dashboard.tip` | `true` | A leader mapping a day. |
| `dashboard.scratchpad` | `false` | `x` opens one notes file in bud's data dir. |
| `dashboard.prs` | `false` | Open PRs per project, and how many ask you for a review, through `gh`. |
| `dashboard.todos` | `false` | How many TODO and FIXME comments each project has, through `rg`. |
| `dashboard.chats` | `false` | The last three full-screen chats, on `a`, `b` and `e` to resume. |
| `dashboard.clock` | `false` | The date and time under the robot. |
| `dashboard.sessions` | `false` | Save a project's open files on exit and restore them when you open it from the dashboard. |

## Claude Code

| Key | Default | What it does |
| --- | --- | --- |
| `ai.claude.autostart` | `true` | Start a Claude Code session when a project opens. Off, bud only starts one when you use an AI feature. |
| `ai.claude.resume` | `"continue"` | Reopening a project carries on its last conversation; `"new"` starts fresh each time. |
| `ai.claude.idle_minutes` | `30` | Stop a session nothing has used for this long; the next use starts it again on the same conversation. `0` never stops one. |
| `ai.claude.args` | `[]` | Extra flags for every session, e.g. `["--model", "opus"]`. |
| `ai.claude.command` | unset | What runs Claude Code when it isn't `claude` itself: a wrapper that passes the rest on, e.g. `["headroom", "wrap", "claude", "--code-memory", "none", "--"]`. |
| `ai.chat.pane` | `"native"` | The default layout's chat pane: bud's own chat, or `"term"` for Claude Code's own TUI. |
| `ai.issues.team` | unset | The Linear team issues filed with `<leader>ai` go to, for projects without their own (`bud issue set --team T [--project P] [project]`). `bud issue set --global` sets it. |
| `ai.issues.project` | unset | The Linear project in that team, if any. |
| `ai.issues.bud.team` | `"JGA"` | Where issues about bud itself (`<leader>aB`) go: bud's own tracker. |
| `ai.issues.bud.project` | `"Bud IDE"` | The Linear project in that team. |

## Tools and the kit

| Key | Default | What it does |
| --- | --- | --- |
| `tools.disabled` | unset | Tools turned off on this machine. `bud tools enable/disable` edits it; a tool that fails to load is added here with the reason (`bud tools ls` shows it). |
| `kit.large_files` | `10000` | A project with this many tracked source files counts as large, for kit entries that only load there (Code-Index-MCP). |
| `kit.large_loc` | `1000000` | The same, by lines of tracked source (estimated from bytes). Either threshold is enough. |
| `kit.disabled` | unset | Kit entries turned off on this machine; `bud kit sync` removes them from Claude Code. `bud kit enable/disable` edits it. Entries a claude.ai connector already provides (Context7, ...) are left out by themselves, so you don't need to disable those. |

## Environment

| Variable | What it does |
| --- | --- |
| `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` | Where bud's dirs go, on macOS too. |
| `NO_COLOR` | Plain output, no colour. |
| `CLAUDE_CONFIG_DIR` | Where Claude Code's own config is (default `~/.claude`). bud's kit and IDE bridge follow it. |
| `CONTEXT7_API_KEY`, `GITHUB_PAT`, ... | Secrets the kit's servers use. Set them in your environment, or keep them in the keychain with `bud kit secret set NAME`. |
