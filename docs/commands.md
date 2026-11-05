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

## Opening things

| Command | What it does |
| --- | --- |
| `bud [path ...]` | Same as `bud open`. No path opens the dashboard. |
| `bud open [path ...]` | One directory opens as a project (and registers it). Files open from the root of the git repo they're in, else from the first file's directory. |
| `bud launch [--terminal kitty\|ghostty] [--layout name] [path ...]` | Opens bud in a new terminal window. `terminal` in `config.json` picks the default, else the first one installed. |
| `bud bundle [--macos\|--linux] [--dest dir]` | Builds the desktop app: `Bud.app` on macOS, `bud.desktop` on Linux. `bud install` does this on the full profile. |

## Setting up and staying healthy

| Command | What it does |
| --- | --- |
| `bud install [--yes]` | Asks for the profile, terminal and languages, shows the package manager command for anything missing and runs it once you agree, syncs the config, restores plugins, then runs `bud doctor`. `--yes` takes every default. |
| `bud update` | Installs a newer release (only for a binary `install.sh` installed), syncs the config, restores plugins, parsers and language tools, syncs the Claude Code kit, then runs the full `bud doctor` and exits 1 on a failure. Rolls back by itself if a check that passed before now fails. |
| `bud update --check [--channel stable\|nightly]` | Only says whether a newer release is out. |
| `bud update --no-doctor` | Skips the doctor at the end, for scripts. |
| `bud doctor [--quick] [--json]` | Health checks with the fix for each problem; exits 1 when something is broken. `--quick` skips the startup timing. |
| `bud uninstall [--keep-config] [--yes]` | Lists everything bud put on the machine and removes it after asking. `--keep-config` keeps `config.json` and your tools. |
| `bud bootstrap [--config] user@host` | Installs this version of bud on another machine over SSH, minimal profile by default. `--config` copies your `config.json` across first. |
| `bud version` | Prints the version. |
| `bud completion bash\|zsh\|fish` | Prints shell completion. Add `eval "$(bud completion zsh)"` to your shell rc. |

## Languages, layouts and projects

| Command | What it does |
| --- | --- |
| `bud lang ls` | Every language spec, whether it's on, and why a broken one is broken. |
| `bud lang enable [--yes] <name ...>` | Turns languages on and installs their system packages and tools. |
| `bud lang disable <name ...>` | Turns languages off. Their tools stay installed until the next `bud update` trims them. |
| `bud lang add <name>` / `rm <name>` | Starts a spec of your own from the template, or removes one. See [languages](languages.md). |
| `bud layout ls` | The layouts, marking the default and this project's. |
| `bud layout set-default <name>` | The layout projects open in. |
| `bud layout set <name> [project]` / `unset [project]` | One project's own layout, or back to the default. See [layouts](layouts.md). |
| `bud projects ls` | Registered projects, pinned first. Opening a directory registers it. |
| `bud projects add` / `rm` / `pin` / `unpin` | Manage the registry. `rm` forgets a project; its files are untouched. |

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

## Claude Code

| Command | What it does |
| --- | --- |
| `bud claude status` | Whether Claude Code is installed and logged in. |
| `bud claude setup [--yes]` | Installs Claude Code with Anthropic's installer if it's missing, then runs its login. |
| `bud agent [path]` | Claude Code's own TUI in a project, from the shell. |
| `bud chat [--resume]` | Claude Code in chat mode, outside any project. `--resume` picks an earlier chat. |
| `bud kit sync [--dry-run]` | Installs bud's Claude Code plugins and their marketplace into Claude Code. |
| `bud kit status` / `ls` | Per item: synced, not synced, changed or skipped and why; or what the kit holds. |
| `bud kit unsync` | Takes out everything the kit added, leaving what's yours. |
| `bud kit enable` / `disable <name>` | Keeps one kit entry in or out on this machine. |
| `bud kit core [--pasted]` | Prints bud-plugins' `core.md` to paste into claude.ai's preferences; `--pasted` records that you did. |
| `bud tools ls` | Your tools and bud's, and whether each is on. |
| `bud tools gen "<description>"` | Has Claude write a tool or language spec, in the editor. See [tools](tools.md). |
| `bud tools enable` / `disable [--reason]` / `rm` / `promote <name>` | Manage tools. `promote` moves one of yours into a bud checkout. |
| `bud issue target [--bud] [dir]` | Where an issue filed from the editor goes in Linear. |
| `bud issue set --team T [--project P] [--global] [project]` / `unset [project]` | Sets or clears it. See [filing issues](issues.md). |

How the pieces fit together is in [Claude Code in bud](claude-code.md).

## Working on bud

| Command | What it does |
| --- | --- |
| `bud dev link [path]` | Points `~/.config/bud` at a checkout with symlinks, so edits show up on the next start. |
| `bud dev unlink` | Goes back to the config this bud ships. |
| `bud kit add` / `rm` | Edits the kit in a bud checkout. |
