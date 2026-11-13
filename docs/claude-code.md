# Claude Code in bud

This page is the `claude` backend (`bud config set ai.backend claude`), which is how bud worked through 1.0. From 1.1 the AI backend is a setting, with an OpenClaw Gateway as the default: see [OpenClaw in bud](openclaw.md). On the `claude` backend, Neovim starts and owns one Claude Code session per project, and every AI feature in the editor goes through it, so they all share one conversation. bud stores no keys: Claude Code's own login is the only one.

## Setting it up

```sh
bud claude status    # installed? logged in? clod set up?
bud claude setup     # install it with Anthropic's installer, log in, then set up clod
bud kit sync         # bud's Claude Code plugins (bud, bud-ide)
```

`bud install` does all three. By default bud shares `~/.claude` with the `claude` you run yourself, so its sessions get your MCP servers, settings, rules and login; `ai.claude.config = "bud"` gives bud a config dir of its own instead. A cloud provider (Bedrock, Vertex, Foundry) set up in `~/.claude/settings.json` works when the dir is shared. [Troubleshooting](troubleshooting.md) covers the details.

**clod** is your Claude Code setup, kept in a private GitHub repo of your own: the MCP servers, settings and rules bud's sessions use. It's `<your gh login>/clod` unless `clod.repo` names another. When it isn't set up here, `bud install`, `bud update` and `bud claude setup` offer to (Enter is yes):

1. install `gh`, `jq` and `bash` if they're missing, and log in to GitHub with `gh auth login`;
2. if your repo doesn't exist yet, create it, private, from the [`jgarner9/clod-template`](https://github.com/jgarner9/clod-template) starter (asked separately, and only a typed `y` makes it: Enter, `--yes` and `bud bootstrap` never create a repo);
3. clone it into `~/clod` and run its `install.sh`, which asks its own questions.

`--yes` answers bud's other questions and passes `--yes` to `install.sh`. A no is remembered, so `bud update` stops asking (it also leaves clod alone while `ai.claude.autostart` is off, as `bud install` does); `bud install` and `bud claude setup` ask again. `bud uninstall` keeps `~/clod`, since it's your repo: `~/clod/install.sh uninstall` takes its setup out of Claude Code. Any repo works as long as it has an `install.sh` at its root that takes `--yes`; start from the template to see the shape.

## The project session

Opening a project (`bud ~/code/thing`) starts its session in the background on a fresh conversation (`ai.claude.resume = "continue"` carries on bud's last one there instead). The statusline shows its state. Sessions nothing has used for 30 minutes stop, and the next message starts them again on the same conversation. At most three run at once.

- `:BudClaude status` shows the session and the IDE bridge; `:BudClaude restart` (`<leader>ar`) and `:BudClaude stop` do what they say.
- `ai.claude.autostart = false` starts one only when you use an AI feature; `ai.claude.resume = "continue"` carries on bud's last conversation in a project instead of starting fresh.

## Which model it runs

Opus sessions cost the most, and changing model or effort partway through a conversation makes Claude Code process all of it again at full price (the prompt cache is keyed on both), which `opusplan` does every time you enter or leave plan mode. So clod's settings default to Sonnet at medium effort, bud's sessions follow them while they share `~/.claude` (`ai.claude.config = "home"`, the default), and bud's agents (`code-reviewer`, `chat`) name Sonnet themselves. A resumed conversation keeps the model it was last on, and that outranks these settings, so with `ai.claude.resume = "continue"` reopening a project lands on Opus if its last conversation ran there; the default, `"new"`, starts every open on the model your settings choose. Use Opus in a separate planning session you start on purpose (`bud agent`, then `/model opus`), not by switching in a long one. With `ai.claude.config = "bud"` its own dir has none of clod's settings, so sessions use Claude Code's default model. To set one for bud's sessions in either case, without touching `~/.claude`, put flags in `ai.claude.args`, e.g. `["--model", "sonnet", "--effort", "medium"]`. On a provider such as Bedrock, clod leaves your model alone.

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

When Claude wants to change a file, the change opens as a diff: `ga`, `:w`, `:wq`, `:x` or `ZZ` in the proposal accepts it, and `q` rejects it, as does closing either side of the diff any other way (`:q`, `<C-w>o`, `:tabclose`). A `:wa` from another tab leaves an edited proposal unsaved rather than accepting it. Anything else it wants to run asks in a small prompt: `y` allow, `a` allow for the rest of the session, `n` deny, `r` deny and say why.

## More

- [Filing issues](issues.md): turn a note into Linear issues, and have Claude fix one in a worktree.
- [Writing a tool](tools.md): `:BudGen` has Claude write an editor extension and load-tests it.
- [Building a Claude TUI](claude-tui.md): the Lua module the features above are built on.
- [Config](config.md#claude-code): every `ai.claude.*` setting.
