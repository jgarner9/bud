# AI backends

The AI in bud is not tied to one product. A **backend** is an agent that speaks the [Agent Client Protocol](https://agentclientprotocol.com) (ACP), which bud starts as a process and talks to over its standard input and output. The AI pane, bud's own chat, the commit message, the generator and issues all go through whichever backend you choose, and bud stores no model keys: each backend signs in the way it always does.

Claude Code is the default (`claude`) and is documented in [Claude Code in bud](claude-code.md). On that backend bud still drives Claude Code directly rather than over ACP; moving it onto ACP is planned.

## Choosing one

```sh
bud ai status                       # the backend that's set, whether it's ready, and what to fix
bud ai setup                        # install its command if missing; for OpenClaw, ask for the Gateway
bud config set ai.backend codex     # claude, openclaw, codex, opencode or custom
```

After changing `ai.backend`, restart the project's agent (`:BudAgent restart`) or reopen the project, and check `bud ai status`. `bud ai setup` asks before installing anything, and it never signs you in: that is the backend's own step. `bud doctor` has an "ai backend" check that reports the same problems as `bud ai status`, as warnings only, since bud works without an AI.

## The backends

| `ai.backend` | What bud runs | You need | It signs in with |
| --- | --- | --- | --- |
| `claude` (default) | `claude` | Claude Code (`bud claude setup`) | Claude Code's own login and `~/.claude` |
| `openclaw` | `openclaw acp`, with `--url` and `--token-file` from `ai.openclaw.*` when set | The `openclaw` CLI (`npm install -g openclaw`, Node 24.16+) and a Gateway to reach: [OpenClaw](openclaw.md) | The Gateway token; the models' logins live on the Gateway's machine |
| `codex` | `codex-acp` | `npm install -g @zed-industries/codex-acp`, and the `codex` CLI too: the AI pane, `bud agent` and `bud chat` run it, while `bud ai status` checks only `codex-acp` | Codex's own login (ChatGPT Plus) |
| `opencode` | `opencode acp` | OpenCode; `bud ai setup` installs it with npm, and if that fails, install it yourself | OpenCode's own providers |
| `custom` | `ai.custom.command`, e.g. `["gemini", "--experimental-acp"]` | Any ACP agent on your `PATH`, and `ai.agent.chat_ui = "bud"`, since bud knows no TUI for it | Whatever it does |

`bud ai setup` installs the command for `openclaw`, `codex` and `opencode` with npm. For `claude` it points you at `bud claude setup`, and for `custom` the command is yours to install.

## What changes with the backend

- **Reviewing edits.** Where a backend sends a diff with its permission request, bud opens it for you to accept or reject before the file is written. Claude Code does this, and OpenCode does when its edit permission is set to ask. OpenClaw sends no diff and writes edits straight away, so the git pane and the session's changed files are your review. Codex through `codex-acp` is not checked yet.
- **Running commands.** Whether a command asks first is the backend's own setting. When it asks, the question comes up in bud (`y` allow once, `a` allow for the session, `n` deny).
- **The chat.** The AI pane runs the backend's own terminal UI by default: Claude Code's, `openclaw tui`, `codex` or `opencode`. On any backend but `claude`, `ai.agent.chat_ui = "bud"` swaps in bud's own chat, drawn in Neovim from the agent's replies. A `custom` backend has no TUI of its own, so use bud's chat.
- **Sessions.** Each project has one session. On the ACP backends bud saves its id and reattaches to it when the backend supports loading sessions; on `claude`, `ai.claude.resume` decides whether a project reopens fresh or continues. `ai.agent.autostart` and `ai.agent.idle_minutes` control when a project's agent starts and stops; the `claude` backend keeps `ai.claude.autostart` and `ai.claude.idle_minutes`.

[Config](config.md) lists every setting, and [Troubleshooting](troubleshooting.md) covers a backend that won't start.
