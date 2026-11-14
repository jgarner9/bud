# Troubleshooting

Start with `bud doctor`. It checks Neovim, the tools bud needs, every enabled language, the synced config, the startup time, Claude Code and the project registry, and each problem comes with the command that fixes it.

## Common problems

**bud is slow to start.** `bud doctor` times the first screen, empty and on a file of each language; the budget is 50 ms. The dashboard's health line shows the same thing for the start you just made, counted from when `bud` started. A language's tooling that's slow the first time runs in the background, so a slow start is usually a plugin or a tool you added. `bud tools disable <name>` rules out tools, one at a time.

**A language has no highlighting, LSP or formatting.** `bud lang ls` says whether it's on and whether its spec is broken. `bud update` builds parsers and installs Mason packages; `:Mason` shows what's installed. Formatting on save only happens in projects with the spec's `format_markers`, when it has them.

**A file opens without tree-sitter, its language server or completion.** It's a big file: over 1.5 MB, or with lines averaging over 1000 characters (minified code). bud opens those as filetype `bigfile` and colours them with Vim's own syntax for their language, because tree-sitter, a language server and completion would each freeze the editor on them; a notification says so when it happens. A markdown file over 250 KB shows as text rather than rendered, for the same reason.

**Pasting is slow over SSH.** It shouldn't be: over SSH (`SSH_CONNECTION` set) bud doesn't tie `p` and `y` to the system clipboard, since there that clipboard goes through your terminal (OSC 52), and many terminals never answer a paste request. `"+y` copies to your own machine's clipboard through the terminal; `"+p` pastes only where the terminal allows it.

**The AI backend isn't answering** (OpenClaw, Codex, OpenCode). `bud ai status` says whether its command is installed and, for OpenClaw, whether the Gateway answers, with the fix for each problem. In the editor, `:BudAgent` shows the project's session and why it stopped; `:BudAgent restart` starts it again. [OpenClaw in bud](openclaw.md) covers the setup.

**Claude Code isn't there in the editor.** `bud claude status` says whether it's installed and logged in; `bud claude setup` fixes either. In the editor, `:BudClaude status` shows the session and the IDE bridge, and `<leader>ar` restarts a session that crashed.

**Claude Code runs on Bedrock, Vertex or Foundry.** Bud shares `~/.claude` by default, so it sees the provider settings in `~/.claude/settings.json`. With its own config (`ai.claude.config = bud`) it can't, so `bud claude status` says "not logged in here"; `bud claude setup` offers to share `~/.claude` again, and claude.ai's login isn't needed. Settings IT manages in `/Library/Application Support/ClaudeCode/` work either way. When the AWS login behind Bedrock runs out, the status line says "AWS login expired" and a notification names the command to log in again (`awsAuthRefresh` from `settings.json`, else `aws sso login --profile <AWS_PROFILE>`); the next message starts a new session that reads the new credentials. There are no claude.ai connectors on these providers, so Linear (for filing issues) has to be an MCP server in Claude Code's own config; log in to it once with `bud agent`, then `/mcp`. Headroom can't compress Bedrock traffic (Claude Code goes to AWS directly, past its proxy), so bud doesn't offer it there, and `bud doctor` warns if `ai.claude.command` still runs it.

**Claude can't edit anything.** Edits come as diffs to accept with `ga` or `:w`; a prompt that was closed counts as no. Allowing something for the rest of the session (`a`) only lasts until you quit.

**bud's sessions have fewer tools than you expect.** Since bud shares `~/.claude`, its MCP servers, skills and rules come from Claude Code's own config, which you keep: add servers with `claude mcp add`, or connect them in claude.ai. `claude mcp list` shows what Claude Code has and whether each connects; "Needs authentication" wants a login, which you do once with `bud agent`, then `/mcp`.

**An update went wrong.** bud checks itself after an update and goes back to the previous version if a check that passed before fails, saying why. `~/.cache/bud/update.log` has the background updates. `bud update` (or `:BudUpdate` in the editor) runs one by hand, then runs the full `bud doctor`, listing anything that isn't right with its fix. A failing check makes `bud update` exit 1, as `bud doctor` does (warnings don't); when a release went in, the error starts "updated to X.Y.Z, but", and the new release stays. `bud update --no-doctor` skips the check.

**The command line or messages look wrong.** Typing a command (`;`), a search or an input opens a float in the middle of the screen, and it goes back to the bottom line when you're done; Neovim's own ui2 draws both, and it's still experimental in Neovim 0.12. If the float sits in the wrong place or completion opens away from it, `bud config set ui.cmdline_float false` keeps the command line at the bottom; if messages misbehave too, `bud config set ui.messages false` turns ui2 off. Both take effect when bud next starts. `g<` shows a message that was cut short.

**The app doesn't open.** `bud bundle` builds it again with your current `PATH` and bud; `bud launch` from a shell shows any error.

**Bud.app is out of date with kitty.** On macOS with Kitty, Bud.app is a copy of kitty.app, so it doesn't follow kitty's updates. `bud update` copies it again by itself; between updates, `bud doctor` warns (`Bud app`) when kitty has moved on, and `bud bundle` copies it again.

## Starting over

`bud uninstall` lists everything bud put on the machine and removes it after asking (`--keep-config` keeps `config.json` and your tools). Claude Code itself stays installed (`~/.local/bin/claude`, `~/.local/share/claude`, `~/.claude`, `~/.claude.json`): `bud install` set it up through Anthropic's installer, but it's a separate product, and `~/.claude` may hold your own sessions, so remove it with Anthropic's instructions if you want it gone. A `~/clod` checkout left by a bud before 2.0 stays too; bud no longer reads it, so delete it when you like (its own `install.sh uninstall` first, if you want its setup out of Claude Code). Then reinstall with the one-liner from the README.

## If you ran `bud kit sync` before 2.0

bud 2.0 no longer installs plugins into Claude Code, so it can't take out what an older `bud kit sync` added, and `bud uninstall` leaves it too. Everything it installed is ordinary Claude Code plugins and a plugin marketplace, so Claude Code can show and remove them:

```sh
claude plugin list                                  # what's installed, as name@marketplace
claude plugin uninstall --scope user NAME@MARKETPLACE
claude plugin marketplace list
claude plugin marketplace remove MARKETPLACE        # once none of its plugins are in use
```

Nothing there is needed for bud to work, and the `bud-ide` plugin that the editor uses now comes inside bud. If an old `bud-ide@...` copy is installed, uninstall it so Claude Code doesn't load it twice. An old `kit` entry in `config.json` is ignored and can stay or go.
