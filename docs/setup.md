# Setting up

From nothing to a working bud, in order. Every step says what it does, what it asks and how to redo it, so you can stop anywhere and pick up later. The short version is one command, and `bud install` walks you through the rest:

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
```

bud is a personal tool that happens to be public: it's built around one person's way of working, and it says so where that shows. [Making it yours](#making-it-yours) lists those places and how to change each.

## What you need first

| | Needed for | Notes |
| --- | --- | --- |
| macOS or Linux, amd64 or arm64 | everything | Homebrew on macOS; pacman, apt or dnf on Linux. |
| A terminal with a [Nerd Font](https://www.nerdfonts.com) | the icons | Any terminal runs `bud`. `bud launch` and the desktop app open Kitty or Ghostty. |
| An AI backend | the AI features | Your choice (`ai.backend`, see [AI backends](backends.md)): Claude Code by default, which the steps below set up, or OpenClaw, Codex, OpenCode or another ACP agent. bud never stores a model key. |
| `gh`, the GitHub CLI | the dashboard's open pull requests | Optional. On the full profile `bud install` installs it and offers `gh auth login`. |
| Linear, connected in Claude Code | filing issues from the editor | Optional. See [Filing issues](issues.md). |

Everything else (Neovim 0.12+, git, ripgrep, fd, fzf, tree-sitter-cli, a C compiler, each language's toolchain) is installed for you in step 2, after you've seen the command.

The editor works without Claude: say no at step 3 and step 3 is skipped. `bud claude setup` picks them up later.

## 1. Install the binary

```sh
curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
```

The script downloads the release for your platform, checks its signature and checksum, and puts `bud` in `~/.local/bin`. Then it offers to run `bud install`, which is step 2. To read it first, or pass options (`--profile minimal`, `--yes`, `--no-install`), see the [README](https://github.com/jgarner9/bud#install).

If `bud` isn't found afterwards, `~/.local/bin` isn't on your `PATH`: add `export PATH="$HOME/.local/bin:$PATH"` to your shell's rc file and open a new shell.

## 2. `bud install`

It asks a few questions, then does the work in this order. `--yes` takes every default; running it again later is safe and asks again.

1. **Profile:** `full` (a desktop: every language, the desktop app) or `minimal` (a server over SSH: Python and Bash to start with, no app).
2. **Terminal:** Kitty or Ghostty, for `bud launch` and the app.
3. **Languages:** which of the eight to enable. Change it any time with `bud lang enable <name>` and `bud lang disable <name>`.
4. **System packages:** it prints the package manager command for whatever is missing and runs it once you agree. On Linux, when the distro's Neovim or tree-sitter-cli is missing or too old, bud downloads a pinned, checksum-verified build into its own data dir instead, and touches nothing under `/usr`.
5. **The config:** the Neovim config inside the binary is synced to `~/.config/bud` and the pinned plugins are restored. This is its own Neovim (`NVIM_APPNAME=bud`); `~/.config/nvim` is never touched.
6. **The desktop app** (full profile): `Bud.app` on macOS, `bud.desktop` on Linux. `bud bundle` builds it again.
7. **Claude Code:** step 3 below.
8. **`bud doctor`:** the health check, with the fix for anything that's wrong.

## 3. The AI backend

bud's AI is a choice of [backend](backends.md); `bud ai setup` checks the one you have set and installs its command, and `bud config set ai.backend <name>` picks another (OpenClaw, Codex, OpenCode or your own). The default is [Claude Code](claude-code.md), installed with Anthropic's own installer and logged in with Claude Code's own login. `bud install` offers both; by hand it's:

```sh
bud claude setup     # install it, then log in
bud claude status    # installed? logged in?
```

bud shares `~/.claude` with the `claude` you run yourself, so one login and one set of settings covers both. Its MCP servers, settings and rules are yours to keep in `~/.claude` however you like (bud doesn't set them up), so bringing your own Claude Code setup is putting it there. On Bedrock, Vertex or Foundry there is nothing to log in to: bud sees the provider settings already in `~/.claude/settings.json`.

On the full profile, `bud install` also offers to run Claude Code through Headroom when it's installed, which trims tool output to save tokens. It's optional; `bud config set ai.claude.command` changes it later.

## 4. Check it

```sh
bud doctor
```

Every line should be OK. A warning or failure comes with the command that fixes it; [Troubleshooting](troubleshooting.md) covers the ones that need more than a command.

## 5. First run

```sh
bud                 # the dashboard
bud ~/code/thing    # open a project; this registers it too
```

Leader is Space: press it and wait, and a panel shows what follows. `<leader>ac` opens Claude Code beside your code, `<leader>,` is settings, and `<leader>qq` quits. [Keys](keys.md) has the rest and [Claude Code in bud](claude-code.md) explains the AI side.

Shell completion, if you want it: add `eval "$(bud completion zsh)"` (or `bash`, `fish`) to your shell's rc file.

## Another machine

For a server you reach over SSH, from a machine that already has bud:

```sh
bud bootstrap user@host            # this version of bud, minimal profile
bud bootstrap --config user@host   # and copy your config.json across first
```

For a second desktop, run step 1 there, then set up Claude Code's own config (`~/.claude`: MCP servers, settings and rules) the same way you did here, by copying it across or with whatever tool you keep it in. Your projects and editor settings (`config.json`) are per machine.

## Keeping it current, and taking it out

- **Updates** install themselves in the background when a stable release is out (`update.auto`). `bud update` does it by hand: the new binary, the config, plugins, language tools, then `bud doctor`. If a check that passed before fails afterwards, it rolls back.
- **`bud uninstall`** lists everything bud put on the machine and removes it after asking. Claude Code itself stays, since it's yours; [Troubleshooting](troubleshooting.md#starting-over) says how to remove those too.

## Making it yours

The places where bud is Jason's, and what to do about each:

- **Where issues go:** issues filed about bud itself default to Linear team `JGA`, project `Bud IDE`. `bud config set ai.issues.bud.team <yours>` (and `ai.issues.bud.project`) points them at a tracker of your own. Issues about your projects go wherever `bud issue set` says; [Filing issues](issues.md) has the detail.
- **Editor settings:** `<leader>,` or `bud config`. Every key and its default is in [Config](config.md).
- **Languages and tools of your own:** [Languages](languages.md) and [Tools](tools.md). Yours live in `~/.local/share/bud/user-tools/` and survive updates. Each project can pick its own [layout](layouts.md).

Changes to the editor itself ship as releases, so a different keymap or plugin set means a fork: `bud dev link <checkout>` points your bud at one.
