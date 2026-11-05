# Claude Code in bud

bud's AI is Claude Code and nothing else. Neovim starts and owns one Claude Code session per project, and every AI feature in the editor goes through it, so they all share one conversation. bud stores no keys: Claude Code's own login is the only one.

## Setting it up

```sh
bud claude status    # installed? logged in? clod set up?
bud claude setup     # install it with Anthropic's installer, log in, then set up clod
bud kit sync         # bud's Claude Code plugins (bud, bud-ide, superpowers)
```

`bud install` does all three. By default bud shares `~/.claude` with the `claude` you run yourself, so its sessions get your MCP servers, settings, rules and login; `ai.claude.config = "bud"` gives bud a config dir of its own instead. A cloud provider (Bedrock, Vertex, Foundry) set up in `~/.claude/settings.json` works when the dir is shared. [Troubleshooting](troubleshooting.md) covers the details.

**clod** is your private Claude Code setup (`jgarner9/clod`): the MCP servers, settings and rules bud's sessions use. When it's missing, `bud install`, `bud update` and `bud claude setup` offer to set it up (Enter is yes): they install `gh` and `jq` if needed, log in to GitHub with `gh auth login` (clod is private), clone it into `~/clod` and run its installer, which asks its own questions. `--yes` answers bud's question and clod's. A no is remembered, so `bud update` stops asking; `bud install` and `bud claude setup` ask again.

## The project session

Opening a project (`bud ~/code/thing`) starts its session in the background, carrying on bud's last conversation there. The statusline shows its state. Sessions nothing has used for 30 minutes stop, and the next message starts them again on the same conversation. At most three run at once.

- `:BudClaude status` shows the session and the IDE bridge; `:BudClaude restart` (`<leader>ar`) and `:BudClaude stop` do what they say.
- `ai.claude.autostart = false` starts one only when you use an AI feature; `ai.claude.resume = "new"` starts fresh each time.

## Ways to talk to it

| | What it is |
| --- | --- |
| **Claude Code's pane** (`<leader>ac`) | Claude Code's own TUI beside the code, on the project's session. It's the `chat` pane in a layout. |
| **Inline actions** (`<leader>ae` / `af` / `at`) | Explain, refactor or write tests for the selection: the request is typed into the pane with the file and lines attached. |
| **Commit messages** (`<leader>am`) | In git's commit buffer, drafts the message from the staged diff and your recent subjects. |
| **Chat** (`<leader>ch`, `c` on the dashboard, `bud chat`) | Claude Code in chat mode, in its own tab, outside any project, with read-only tools. |
| **From the shell** (`bud agent [path]`) | The same TUI in a project, without the editor. |

## The editor is connected

Claude Code sees your open buffers, the selection and diagnostics through bud's IDE bridge, so "this function" means the one under your cursor. `<leader>as` points it at the selected lines.

When Claude wants to change a file, the change opens as a diff: `ga` or `:w` accepts it, `q` rejects it. Anything else it wants to run asks in a small prompt: `y` allow, `a` allow for the rest of the session, `n` deny, `r` deny and say why.

## More

- [Filing issues](issues.md): turn a note into Linear issues, and have Claude fix one in a worktree.
- [Writing a tool](tools.md): `:BudGen` has Claude write an editor extension and load-tests it.
- [Building a Claude TUI](claude-tui.md): the Lua module the features above are built on.
- [Config](config.md#claude-code): every `ai.claude.*` setting.
