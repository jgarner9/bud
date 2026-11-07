# Troubleshooting

Start with `bud doctor`. It checks Neovim, the tools bud needs, every enabled language, the synced config, the startup time, Claude Code and the project registry, and each problem comes with the command that fixes it.

## Common problems

**bud is slow to start.** `bud doctor` times the first screen, empty and on a file of each language; the budget is 50 ms. The dashboard's health line shows the same thing for the start you just made, counted from when `bud` started. A language's tooling that's slow the first time runs in the background, so a slow start is usually a plugin or a tool you added. `bud tools disable <name>` rules out tools, one at a time.

**A language has no highlighting, LSP or formatting.** `bud lang ls` says whether it's on and whether its spec is broken. `bud update` builds parsers and installs Mason packages; `:Mason` shows what's installed. Formatting on save only happens in projects with the spec's `format_markers`, when it has them.

**A file opens without tree-sitter, its language server or completion.** It's a big file: over 1.5 MB, or with lines averaging over 1000 characters (minified code). bud opens those as filetype `bigfile` and colours them with Vim's own syntax for their language, because tree-sitter, a language server and completion would each freeze the editor on them; a notification says so when it happens. A markdown file over 250 KB shows as text rather than rendered, for the same reason.

**Pasting is slow over SSH.** It shouldn't be: over SSH (`SSH_CONNECTION` set) bud doesn't tie `p` and `y` to the system clipboard, since there that clipboard goes through your terminal (OSC 52), and many terminals never answer a paste request. `"+y` copies to your own machine's clipboard through the terminal; `"+p` pastes only where the terminal allows it.

**Claude Code isn't there in the editor.** `bud claude status` says whether it's installed and logged in; `bud claude setup` fixes either. In the editor, `:BudClaude status` shows the session and the IDE bridge, and `<leader>ar` restarts a session that crashed.

**Claude Code runs on Bedrock, Vertex or Foundry.** Bud shares `~/.claude` by default, so it sees the provider settings in `~/.claude/settings.json`. With its own config (`ai.claude.config = bud`) it can't, so `bud claude status` says "not logged in here"; `bud claude setup` offers to share `~/.claude` again, and claude.ai's login isn't needed. The first `bud kit sync` into `~/.claude` lists what it would add and leaves anything you already have alone; `bud kit disable NAME` keeps one out, and `bud kit unsync` takes them all out again. Settings IT manages in `/Library/Application Support/ClaudeCode/` work either way. When the AWS login behind Bedrock runs out, the status line says "AWS login expired" and a notification names the command to log in again (`awsAuthRefresh` from `settings.json`, else `aws sso login --profile <AWS_PROFILE>`); the next message starts a new session that reads the new credentials. There are no claude.ai connectors on these providers, so Linear (for filing issues) has to be an MCP server in Claude Code's own config; log in to it once with `bud agent`, then `/mcp`. Headroom can't compress Bedrock traffic (Claude Code goes to AWS directly, past its proxy), so bud doesn't offer it there, and `bud doctor` warns if `ai.claude.command` still runs it.

**Claude can't edit anything.** Edits come as diffs to accept with `ga` or `:w`; a prompt that was closed counts as no. Allowing something for the rest of the session (`a`) only lasts until you quit.

**bud's sessions have fewer tools than you expect.** Since bud shares `~/.claude`, its MCP servers and the `core.md` rule come from Claude Code's own config, which clod sets up. `bud doctor` (the full run, not `--quick`) has a `claude setup` check that names any of `kit.json`'s `expected_mcp` servers missing from `claude mcp list` (a claude.ai connector counts) and whether the `core.md` rule is there. `bud claude setup` sets clod up if it's missing; otherwise add them yourself. A server that's listed but doesn't connect is named too, with the status `claude mcp list` gave it. "Failed to connect" means its command or URL is broken: `claude mcp get NAME` shows the command, and running that by hand shows the error. "Needs authentication" means it wants a login, which you do once with `bud agent`, then `/mcp`. When the check adds "defined in more than one scope", `claude mcp list` found the server in more than one place (user, project or local), and the one that wins may be the broken one; the list's diagnostics name both, with the `claude mcp remove NAME -s SCOPE` that takes one out. A server that connects is fine wherever it's defined, and so is one whose claude.ai connector connects.

**claude.ai answers differently from Claude Code.** claude.ai's personal preferences hold a pasted copy of bud-plugins' `core.md`. When it changes, `bud kit sync` (and so `bud update`) and `bud doctor` say so: run `bud kit core | pbcopy`, paste it into Settings, Profile, personal preferences (on Bedrock, Desktop's organization instructions come from clod), then `bud kit core --pasted`.

**A kit item didn't sync.** `bud kit status` says why: usually something with the same name you set up yourself, which bud never touches. MCP servers aren't in the kit any more: they come from Claude Code's own config (`claude mcp list`), and the first sync after updating takes out the ones an older bud added.

**An update went wrong.** bud checks itself after an update and goes back to the previous version if a check that passed before fails, saying why. `~/.cache/bud/update.log` has the background updates. `bud update` (or `:BudUpdate` in the editor) runs one by hand, then syncs the Claude Code kit and runs the full `bud doctor`, listing anything that isn't right with its fix. A failing check makes `bud update` exit 1, as `bud doctor` does (warnings don't); when a release went in, the error starts "updated to X.Y.Z, but", and the new release stays. `bud update --no-doctor` skips the check. A kit you turned down, or took out with `bud kit unsync`, isn't offered again: `bud kit sync` adds it, and so does `bud install`.

**The command line or messages look wrong.** Typing a command (`;`), a search or an input opens a float in the middle of the screen, and it goes back to the bottom line when you're done; Neovim's own ui2 draws both, and it's still experimental in Neovim 0.12. If the float sits in the wrong place or completion opens away from it, `bud config set ui.cmdline_float false` keeps the command line at the bottom; if messages misbehave too, `bud config set ui.messages false` turns ui2 off. Both take effect when bud next starts. `g<` shows a message that was cut short.

**The app doesn't open.** `bud bundle` builds it again with your current `PATH` and bud; `bud launch` from a shell shows any error.

## Starting over

`bud uninstall` lists everything bud put on the machine and removes it after asking (`--keep-config` keeps `config.json` and your tools). With a shared `~/.claude` that includes what the kit added there, the same as `bud kit unsync`; everything of yours stays. Your clod checkout (`~/clod`) stays too; `~/clod/install.sh uninstall` takes its setup out of Claude Code first if you want that gone. Then reinstall with the one-liner from the README.

## A skill from bud-plugins is missing in Claude Code

When claude.ai syncs a bud-plugins plugin (`bud`, `bud-ide`, `superpowers`) and another copy of it is also installed, Claude Code loads that other copy, so skills added since it was installed don't show up. `bud doctor` warns about this under `synced plugins`, and `bud kit sync` names the copy. bud leaves a copy it didn't install alone, so take it out yourself and restart Claude Code:

```sh
claude plugin uninstall --scope user bud@bud-plugins
```
