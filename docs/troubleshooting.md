# Troubleshooting

Start with `bud doctor`. It checks Neovim, the tools bud needs, every enabled language, the synced config, the startup time, Claude Code and the project registry, and each problem comes with the command that fixes it.

## Common problems

**bud is slow to start.** `bud doctor` times the first screen, empty and on a file of each language; the budget is 50 ms. A language's tooling that's slow the first time runs in the background, so a slow start is usually a plugin or a tool you added. `bud tools disable <name>` rules out tools, one at a time.

**A language has no highlighting, LSP or formatting.** `bud lang ls` says whether it's on and whether its spec is broken. `bud update` builds parsers and installs Mason packages; `:Mason` shows what's installed. Formatting on save only happens in projects with the spec's `format_markers`, when it has them.

**Claude Code isn't there in the editor.** `bud claude status` says whether it's installed and logged in; `bud claude setup` fixes either. In the editor, `:BudClaude status` shows the session and the IDE bridge, and `<leader>ar` restarts a session that crashed.

**Claude Code runs on Bedrock, Vertex or Foundry.** Bud shares `~/.claude` by default, so it sees the provider settings in `~/.claude/settings.json`. With its own config (`ai.claude.config = bud`) it can't, so `bud claude status` says "not logged in here"; `bud claude setup` offers to share `~/.claude` again, and claude.ai's login isn't needed. The first `bud kit sync` into `~/.claude` lists what it would add and leaves anything you already have alone; `bud kit disable NAME` keeps one out, and `bud kit unsync` takes them all out again. Settings IT manages in `/Library/Application Support/ClaudeCode/` work either way. When the AWS login behind Bedrock runs out, the status line says "AWS login expired" and a notification names the command to log in again (`awsAuthRefresh` from `settings.json`, else `aws sso login --profile <AWS_PROFILE>`); the next message starts a new session that reads the new credentials. There are no claude.ai connectors on these providers, so Linear (for filing issues) has to be an MCP server in Claude Code's own config; log in to it once with `bud agent`, then `/mcp`. Headroom can't compress Bedrock traffic (Claude Code goes to AWS directly, past its proxy), so bud doesn't offer it there, and `bud doctor` warns if `ai.claude.command` still runs it.

**Claude can't edit anything.** Edits come as diffs to accept with `ga` or `:w`; a prompt that was closed counts as no. Allowing something for the rest of the session (`a`) only lasts until you quit.

**bud's sessions have fewer tools than you expect.** Since bud shares `~/.claude`, its MCP servers and the `core.md` rule come from Claude Code's own config, which clod sets up. `bud doctor` (the full run, not `--quick`) has a `claude setup` check that names any of `kit.json`'s `expected_mcp` servers missing from `claude mcp list` (a claude.ai connector counts) and whether the `core.md` rule is there. Run clod's install, or add them yourself.

**claude.ai answers differently from Claude Code.** claude.ai's personal preferences hold a pasted copy of bud-plugins' `core.md`. When it changes, `bud kit sync` (and so `bud update`) and `bud doctor` say so: run `bud kit core | pbcopy`, paste it into Settings, Profile, personal preferences (on Bedrock, Desktop's organization instructions come from clod), then `bud kit core --pasted`.

**A kit item didn't sync.** `bud kit status` says why: usually something with the same name you set up yourself, which bud never touches. MCP servers aren't in the kit any more: they come from Claude Code's own config (`claude mcp list`), and the first sync after updating takes out the ones an older bud added.

**An update went wrong.** bud checks itself after an update and goes back to the previous version if a check that passed before fails, saying why. `~/.cache/bud/update.log` has the background updates. `bud update` (or `:BudUpdate` in the editor) runs one by hand.

**The app doesn't open.** `bud bundle` builds it again with your current `PATH` and bud; `bud launch` from a shell shows any error.

## Starting over

`bud uninstall` lists everything bud put on the machine and removes it after asking (`--keep-config` keeps `config.json` and your tools). With a shared `~/.claude` that includes what the kit added there, the same as `bud kit unsync`; everything of yours stays. Then reinstall with the one-liner from the README.
