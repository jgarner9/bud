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

`-y` is short for `--yes` on `install`, `uninstall`, `claude setup`, `lang enable`, `lang rm` and `kit unsync`.

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
| `bud update` | Installs a newer release (only for a binary `install.sh` installed), syncs the config, restores plugins, parsers and language tools, syncs the Claude Code kit, rebuilds the desktop app if you've run `bud bundle` and it's out of date (another bud built it, kitty updated, or it moved; not while it's open), then runs the full `bud doctor` and exits 1 on a failure. Rolls back by itself if a check that passed before now fails. |
| `bud update --check [--channel stable\|nightly]` | Only says whether a newer release is out. |
| `bud update --no-doctor` | Skips the app rebuild and the doctor at the end, for scripts. |
| `bud update --background` | What bud starts by itself at launch, detached and at most once a day, so launching never waits on the network. It installs a newer stable release when `update.auto` is on, else only notes it for the dashboard. `--no-update` skips it. You don't normally run it. |
| `bud doctor [--quick] [--json]` | Health checks with the fix for each problem; exits 1 when something is broken. `--quick` skips the slow ones: the startup timing, and Claude Code's MCP servers and synced plugins (`claude setup`, `synced plugins`), which the dashboard leaves out. |
| `bud uninstall [--keep-config] [--yes]` | Lists everything bud put on the machine and removes it after asking. `--keep-config` keeps `config.json` and your tools. Claude Code and your clod checkout stay (see Troubleshooting). |
| `bud bootstrap [--config] user@host` | Installs this version of bud on another machine over SSH, minimal profile by default. `--config` copies your `config.json` across first. |
| `bud version` | Prints the version. |
| `bud completion bash\|zsh\|fish` | Prints shell completion. Add `eval "$(bud completion zsh)"` to your shell rc. |

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

bud is moving to talk to its AI over the Agent Client Protocol (ACP), with the backend a setting (`ai.backend`). The default, `claude`, still runs Claude Code directly (below); `openclaw`, `codex`, `opencode` and `custom` go over ACP.

| Command | What it does |
| --- | --- |
| `bud ai status [--json]` | Which backend is set, the command it runs, whether that's installed, and for OpenClaw whether the Gateway at `ai.openclaw.url` answers, with a fix for each problem. |
| `bud agent [path]` · `bud chat` | On any backend but `claude`, open the backend's own TUI from the shell: OpenClaw's `openclaw tui` on the project's Gateway session (the one the editor uses), or on its one chat session for `bud chat`. With `ai.backend = "claude"` they're Claude Code's, below. |
| `bud ai setup [--yes]` | On the `claude` backend, points you at `bud claude setup`. On the others, installs the backend's command with npm if it's missing (asking first), then for OpenClaw asks for the Gateway's URL and the file holding its token, and saves them. Models sign in on the Gateway host (`openclaw onboard` there). |

## Claude Code

| Command | What it does |
| --- | --- |
| `bud claude status` | Whether Claude Code is installed and logged in, and whether clod is set up. |
| `bud claude setup [--yes]` | Installs Claude Code with Anthropic's installer if it's missing, runs its login, then sets up clod if it's missing (gh login, your repo made from the template if it doesn't exist, a clone into `~/clod`, its installer). See [Claude Code in bud](claude-code.md#setting-it-up). |
| `bud agent [path]` | Claude Code's own TUI in a project, from the shell. |
| `bud chat [--resume]` | Claude Code in chat mode, outside any project. `--resume` picks an earlier chat. |
| `bud kit sync [--dry-run]` | Installs bud's Claude Code plugins and their marketplace into Claude Code. |
| `bud kit status` / `ls` | Per item: synced, not synced, changed or skipped and why; or what the kit holds. |
| `bud kit unsync [--dry-run] [--yes]` | Takes out everything the kit added, leaving what's yours. |
| `bud kit enable` / `disable <name>` | Keeps one kit entry in or out on this machine. |
| `bud kit core [--pasted]` | Prints bud-plugins' `core.md` to paste into claude.ai's preferences; `--pasted` records that you did. |
| `bud tools ls` | Your tools and bud's, and whether each is on. |
| `bud tools gen "<description>"` | Has Claude write a tool or language spec, in the editor. See [tools](tools.md). |
| `bud tools enable` / `disable [--reason] <name>` | Turns a tool on or off. |
| `bud tools rm [--yes] <name>` | Deletes one of your tools, after asking. |
| `bud tools promote [--repo dir] [--yes] <name>` | Moves one of yours into a bud checkout's `tools/`, commits and pushes it, after asking. `--repo` defaults to the `bud dev link` checkout. |
| `bud issue target [--bud] [--json] [dir]` | Where an issue filed from the editor goes in Linear. `--json` prints it for the editor. |
| `bud issue set --team T [--project P] [--global] [project]` / `unset [project]` | Sets or clears it. See [filing issues](issues.md). |
| `bud issue checkout [path]` / `--unset` | Saves the bud checkout `<leader>aB` works in (checked to still be a bud git checkout), forgets it, or with neither says what is saved. See [filing issues](issues.md). |

How the pieces fit together is in [Claude Code in bud](claude-code.md).

## Working on bud

| Command | What it does |
| --- | --- |
| `bud dev link [path]` | Points `~/.config/bud` at a checkout with symlinks, so edits show up on the next start. |
| `bud dev unlink` | Goes back to the config this bud ships. |
| `bud kit add [--repo dir] plugin <name@marketplace>` | Lists a plugin in a checkout's kit (`add marketplace <name> <source>` for a marketplace). |
| `bud kit rm [--repo dir] <name>` | Takes an entry out of the kit; the next sync removes it from Claude Code. |
