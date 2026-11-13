# Overnight note for Jason (2026-11-13)

Delete this file before merging the branch. It's here so the note exists even if the session stopped mid-step.

Branch: `bud/upbeat-wright-g2fwv6`. Kept up to date as the work goes; the last section says where it stopped.

## Heads-up

- **Linear, Basic Memory, Context7 and Exa all failed to connect this session** (502 from the connector proxy). I couldn't read your restructured 1.1/1.2 milestones in Linear or file issues there, so the plan lives in BASE.md (4.4, section 6, section 7) and the task list below. The OpenCode issues JGA-262 to JGA-276 are now out of date: re-aim or close them when Linear is back.
- The OpenCode facts pass from last night never got committed, apart from D128-D130, which this morning's decisions supersede or keep (see below).

## What I decided, and why (BASE.md D131-D138)

- **D131 Backend-agnostic over ACP.** bud talks the Agent Client Protocol: JSON lines on stdio, which Lua handles with a job. OpenClaw (`openclaw acp`), Claude Code (`claude-agent-acp`), Codex (`codex-acp`), OpenCode (`opencode acp`) and Gemini all speak it, so a backend is a command line. I rejected OpenClaw's own Gateway WebSocket protocol, which needs Ed25519 device signing and pairing (too much to carry in Lua), and adapters per harness.
- **D132 OpenClaw is the default,** a Gateway on your home server reached over Tailscale. It has everything you listed: the web Control UI, the apps, `openclaw tui`, Claude Code through its `claude-cli` runtime, and ChatGPT Plus through Codex OAuth. bud runs `openclaw acp --url ... --token-file ...`.
- **D133 Neutral names:** `bud.ai.agent`, `ai.backend`, `ai.agent.*`, `bud ai status|setup`. This supersedes D128's `opencode` names.
- **D134 Edit review:** before the write when the backend sends a diff (Claude and Codex do), afterwards with git when it doesn't. OpenClaw's bridge sends no diffs.
- **D135 Claude Code stays usable** as a backend, so the kit stays. Only its stream-json transport and the IDE bridge go. D129 (no cloud work) and D130 (no clod setup) stand.
- **D136 Locked panes** and **D137 chat pane or full screen**: built, see below.
- **D138 bud's own chat UI** drawn from ACP events, in your colours and keys, is 1.2. Until then the pane runs the backend's TUI.

## Done

- BASE.md: D131-D138, 4.4 rewritten, the 1.1 and 1.2 build order rewritten, and section 7's open questions replaced. CLAUDE.md's AI paragraph updated.
- **Locked panes** (`bud.lock`): the explorer, git pane, AI pane and terminal close only by their toggles. `:q`, `:close` or `:only` puts the pane straight back with an error naming the key, and a file opened while a pane has the cursor goes to the editor window, with an error. New `<leader>gs` toggles the git pane. Test: `tests/lua/lock_test.lua`.
- **Chat full screen:** `<leader>aF` moves the AI chat into a tab of its own and back. `ai.agent.chat_layout = "full"` makes `<leader>ac` open it that way. Tested in `claude_term_test.lua`.
- **Settings:** `ai.backend` (default `openclaw`), `ai.agent.chat_layout`, `ai.openclaw.url`, `ai.openclaw.token_file` and `ai.custom.command`, with validation and docs.
- **`bud.ai.acp`**, the ACP v1 client, plus a fake ACP agent (a headless nvim script) for tests. Test: `tests/lua/acp_test.lua`. The protocol notes are in `docs/internals/acp.md`.

## Decisions waiting on you

1. **One OpenClaw session per project, or one shared** (BASE.md 7.1). I recommend one per project. For now bud lets `openclaw acp` make its own isolated session each time, because I couldn't confirm against a real Gateway whether `--session-label` creates a missing label.
2. **Where the Gateway token lives** (7.2). I recommend a mode-600 file named by `ai.openclaw.token_file`.
3. **Claude subscription through OpenClaw.** Reports from April 2026 say Anthropic stopped third-party tools drawing on the Claude subscription bucket, which conflicts with OpenClaw's docs on `claude-cli` reuse. Check Anthropic's current terms before relying on it. ChatGPT Plus through Codex OAuth looks solid.
4. **The error on a redirected buffer.** You asked for an error when something tries to change a pane. I raise the error *and* open the file in the editor window, so nothing you picked is lost. Say if you'd rather it only refused.

## Where it stopped

(updated as the work goes)
