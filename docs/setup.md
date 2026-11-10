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
| A Claude account | the AI features | A claude.ai subscription, an API key, or a provider (Bedrock, Vertex, Foundry) already set up for Claude Code. bud never stores a key. |
| A GitHub account and `gh` | clod (step 4) | `bud install` installs `gh` and runs `gh auth login` if you say yes. |
| Linear, connected in Claude Code | filing issues from the editor | Optional. See [Filing issues](issues.md). |

Everything else (Neovim 0.12+, git, ripgrep, fd, fzf, tree-sitter-cli, a C compiler, each language's toolchain) is installed for you in step 2, after you've seen the command.

The editor works without Claude: say no at step 3 and steps 3 to 6 are skipped. `bud claude setup` picks them up later.

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
7. **Claude Code, clod and the kit:** steps 3 to 5 below.
8. **`bud doctor`:** the health check, with the fix for anything that's wrong.

## 3. Claude Code

bud's AI is [Claude Code](claude-code.md), installed with Anthropic's own installer and logged in with Claude Code's own login. `bud install` offers both; by hand it's:

```sh
bud claude setup     # install it, log in, then offer clod (step 4)
bud claude status    # installed? logged in? clod set up?
```

bud shares `~/.claude` with the `claude` you run yourself, so one login and one set of settings covers both. On Bedrock, Vertex or Foundry there is nothing to log in to: bud sees the provider settings already in `~/.claude/settings.json`.

On the full profile, `bud install` also offers to run Claude Code through Headroom when it's installed, which trims tool output to save tokens. It's optional; `bud config set ai.claude.command` changes it later.

## 4. clod: your Claude Code setup, in a repo of your own

Claude Code needs more than a login to be useful: MCP servers, settings, rules. **clod** is where yours live, in a private GitHub repo, so every machine gets the same setup from one place. bud looks for `<your GitHub login>/clod` (or the repo `clod.repo` names) and, when it isn't set up on this machine, offers to:

1. install `gh`, `jq` and `bash` if they're missing, and log in to GitHub (`gh auth login`);
2. create the repo if you don't have one, private, from the [`clod-template`](https://github.com/jgarner9/clod-template) starter. This is asked separately and only a typed `y` does it: Enter, `--yes` and `bud bootstrap` never create a repo;
3. clone it into `~/clod` and run its `install.sh`, which copies your rules, skills, agents and commands into `~/.claude`, merges your settings and adds your MCP servers.

Afterwards you edit `~/clod` (the template's README says what goes where), push, and run `~/clod/install.sh` again here or on another machine. Never commit a secret to it: MCP servers read keys from the environment.

Say no and bud remembers, so `bud update` stops asking; `bud claude setup` offers it again. Without clod everything still works, with whatever is already in your `~/.claude`.

## 5. The kit: bud's Claude Code plugins

```sh
bud kit sync         # what bud install runs
bud kit status       # what's installed, and why anything was skipped
```

The kit installs two plugins into Claude Code from the public [`bud-plugins`](https://github.com/jgarner9/bud-plugins) marketplace:

- **`bud-ide`** is what the editor's features run on: the skill that files issues, the agent bud's chat starts, and notes that tell Claude it's inside bud. Keep this one.
- **`bud`** is how Jason wants Claude to work: code style, planning, tool use, debugging, releases. It's opinionated on purpose. If it isn't for you, `bud kit disable bud@bud-plugins` keeps it out.

`bud install` syncs the kit only once Claude Code is logged in; logged out, it tells you to run `bud kit sync` later. Sync never overwrites something you set up yourself, and `bud kit unsync` takes out everything it added.

## 6. claude.ai (optional)

Only if you want claude.ai, the phone app and Claude Desktop to behave like Claude Code in bud. None of bud needs it.

1. On claude.ai: Customize, Plugins, Add marketplace, `jgarner9/bud-plugins`, and turn on **Sync automatically**. Add `bud` from it (not `bud-ide`).
2. Paste `core.md`, the short always-on instructions, into Settings, Profile, personal preferences:

   ```sh
   bud kit core | pbcopy      # macOS; on Linux pipe to wl-copy or xclip
   bud kit core --pasted      # record that you did, so bud can say when it changes
   ```

When `core.md` changes upstream, `bud kit sync` and `bud doctor` tell you to paste it again.

## 7. Check it

```sh
bud doctor
```

Every line should be OK. A warning or failure comes with the command that fixes it; [Troubleshooting](troubleshooting.md) covers the ones that need more than a command. The full run also checks that Claude Code has the MCP servers bud's sessions expect and that your plugins are the current ones.

## 8. First run

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

For a second desktop, run step 1 there. Either way your clod repo is found by name and cloned, so Claude Code comes up with the same servers, settings and rules; your projects and editor settings (`config.json`) are per machine.

## Keeping it current, and taking it out

- **Updates** install themselves in the background when a stable release is out (`update.auto`). `bud update` does it by hand: the new binary, the config, plugins, language tools and the kit, then `bud doctor`. If a check that passed before fails afterwards, it rolls back.
- **`bud uninstall`** lists everything bud put on the machine and removes it after asking. Claude Code itself and `~/clod` stay, since they're yours; [Troubleshooting](troubleshooting.md#starting-over) says how to remove those too.

## How the pieces fit

| Piece | Where it lives | What it is | Who changes it |
| --- | --- | --- | --- |
| **bud** | [`jgarner9/bud`](https://github.com/jgarner9/bud), public | The CLI and the whole Neovim config, shipped as one signed binary. | Releases; `bud update` brings them. |
| **bud-plugins** | [`jgarner9/bud-plugins`](https://github.com/jgarner9/bud-plugins), public | A Claude Code plugin marketplace: `bud-ide` for the editor, `bud` for how Claude works. | Jason; `bud kit sync` and claude.ai pick up changes. |
| **clod** | `<you>/clod`, private | Your Claude Code setup: MCP servers, settings, rules, skills. | You. |
| **clod-template** | [`jgarner9/clod-template`](https://github.com/jgarner9/clod-template), public | The starter your clod is made from, once. | Nobody, after that. |

bud itself holds no Claude content and no secrets: how Claude behaves comes from the plugins, and how your machine is wired comes from your clod.

## Making it yours

The places where bud is Jason's, and what to do about each:

- **The `bud` plugin** (code style, planning, tools): `bud kit disable bud@bud-plugins`, or fork `bud-plugins` and add your fork with `bud kit add marketplace` in a checkout.
- **Where issues go:** issues filed about bud itself default to Linear team `JGA`, project `Bud IDE`. `bud config set ai.issues.bud.team <yours>` (and `ai.issues.bud.project`) points them at a tracker of your own. Issues about your projects go wherever `bud issue set` says; [Filing issues](issues.md) has the detail.
- **The MCP servers bud expects** (Basic Memory, Context7, Exa): `bud doctor` warns when Claude Code lacks one. They're what the `bud` plugin's skills reach for; add them to your clod, or ignore the warning if you turned that plugin off.
- **Editor settings:** `<leader>,` or `bud config`. Every key and its default is in [Config](config.md).
- **Languages and tools of your own:** [Languages](languages.md) and [Tools](tools.md). Yours live in `~/.local/share/bud/user-tools/` and survive updates. Each project can pick its own [layout](layouts.md).

Changes to the editor itself ship as releases, so a different keymap or plugin set means a fork: `bud dev link <checkout>` points your bud at one.
