# Overnight note for Jason (2026-11-13)

Delete this file before merging the branch. It's here so the note exists even if the session stopped mid-step.

Branch: `bud/upbeat-wright-g2fwv6`, every commit pushed. `just lint`, `go test ./...`, the whole Lua suite and `just smoke` pass at the last commit.

## Read this first

- **The default backend is now OpenClaw.** Until your Gateway is set up, `bud config set ai.backend claude` keeps today's Claude Code behaviour exactly; every Claude Code test runs that way. The setup guide is the new [docs/openclaw.md](docs/openclaw.md): the server half and the per-machine half.
- **Linear, Basic Memory, Context7 and Exa all failed to connect this session** (502 from the connector proxy). I couldn't read your restructured 1.1/1.2 milestones or file anything there, so the plan lives in BASE.md (4.4, section 6, section 7). The OpenCode issues JGA-262 to JGA-276 are out of date: re-aim or close them when Linear is back.

## What I decided, and why (BASE.md D131-D139)

- **D131 Backend-agnostic over ACP.** bud talks the Agent Client Protocol: JSON lines on stdio, which Lua handles with a job. OpenClaw (`openclaw acp`), Claude Code (`claude-agent-acp`), Codex (`codex-acp`), OpenCode (`opencode acp`) and Gemini all speak it, so a backend is a command line. I rejected OpenClaw's own Gateway WebSocket protocol, which needs Ed25519 device signing and pairing (too much to carry in Lua), and adapters per harness.
- **D132 OpenClaw is the default:** a Gateway on your home server, reached over Tailscale. It has the web Control UI, the apps, `openclaw tui`, Claude Code through its `claude-cli` runtime, and ChatGPT Plus through Codex OAuth.
- **D133 Neutral names:** `bud.ai.agent`, `ai.backend`, `ai.agent.*`, `bud ai status|setup`.
- **D134 Edit review:** before the write when the backend sends a diff (Claude, Codex), afterwards when it doesn't. OpenClaw writes edits without asking and reports the patch after.
- **D135 Claude Code stays usable** as a backend, so the kit stays. D129 (no cloud work) and D130 (no clod setup) stand.
- **D136 Locked panes, D137 chat pane or full screen:** built.
- **D138 bud's own chat UI**, drawn from ACP events in your colours and keys, is 1.2. Until then the pane runs the backend's TUI.
- **D139 One OpenClaw session per project.** bud saves the ACP session id and reattaches with `session/load`, history and all. I first used a named `--session` key, but a real-Gateway run showed those sessions run tools in the agent's workspace instead of the project, so I switched.

## Done (newest last)

1. BASE.md: the decisions, 4.4 rewritten, the 1.1 and 1.2 build order rewritten (with what's built marked in each task), and section 7's open questions. CLAUDE.md's AI paragraph updated.
2. **Locked panes** (`bud.lock`): the explorer, git pane, AI pane and terminal close only by their toggles. `:q`, `:close` or `:only` puts the pane straight back with an error naming the key, and a file opened while a pane has the cursor goes to the editor window, with an error. New `<leader>gs` toggles the git pane. Session restore and the layout close panes quietly.
3. **Chat full screen:** `<leader>aF` moves the AI chat into a tab of its own and back. `ai.agent.chat_layout = "full"` makes `<leader>ac` open it that way.
4. **Settings:** `ai.backend`, `ai.agent.chat_layout`, `ai.openclaw.url`, `ai.openclaw.token_file`, `ai.openclaw.agent`, `ai.custom.command`. The settings screen has an AI section.
5. **`bud.ai.acp`**, the ACP client, plus a fake ACP agent (a headless nvim script) for tests.
6. **`bud.ai.agent`**, the session layer with `bud.ai.claude`'s calls and events, and **`bud.ai.session`**, which picks the configured backend's session. The commit message, the generator, issues and work go through it.
7. **Checked against a real OpenClaw Gateway**, again at the end with bud's own chat across two Neovim runs (the same session reattached, history shown, files landed in the project). OpenClaw 2026.11.3 ran on loopback with a scripted local model: no real provider, nothing of yours. bud's own client drove the real `openclaw acp`, and prompts, streaming, cancel, edits, exec and approvals all work. The streams are now test fixtures replayed through the whole stack.
8. **The AI pane, chat, `bud agent` and `bud chat`** run `openclaw tui` on the project's Gateway session, with the token passed in the environment. I checked against the real Gateway that the TUI shows the thread bud's calls wrote. Inline actions type into the TUI, and `<leader>as` shares a selection.
9. **Permission questions** from any ACP backend use bud's own prompt (`y` / `a` / `n`) and the diff review.
10. **`bud ai status [--json]` / `bud ai setup`**, the doctor's "ai backend" check, and the dashboard's banner, health line and `c` key all follow the backend.
11. **docs/openclaw.md** (on the site's sidebar), plus `docs/internals/acp.md` for working on it.
12. **bud's own chat (1.2, D138), a first version:** `<leader>av`, or `<leader>ac` with `ai.agent.chat_ui = "bud"`. It's a markdown conversation in Neovim with streamed replies, a line per tool call with its state (`○ ◐ ✓ ✗`), OpenClaw's patch shown as a diff, a prompt line (`<CR>` sends, `<C-c>` stops), and the history replayed when you reopen. It runs as a pane or full screen and works the same on every backend. This is the "looks exactly how I want" surface: colours, layout and keys are all bud's to change.
13. `:q` in the last file window now takes the locked panes with it, so it still quits bud.
14. `bud install` on OpenClaw offers `bud ai setup` instead of Claude Code and clod. The doctor's Claude check no longer complains on other backends.
15. **`:BudAgent [status|restart|stop|option]`**: the backend's session at a glance, plus its settings (OpenClaw's `thought_level`, `fast_mode` and the rest), checked against the real Gateway. Errors now carry OpenClaw's own reason instead of a bare "Internal error".
16. **bud's chat is themeable:** `BudChatYou`, `BudChatAgent`, `BudChatTool`, `BudChatToolDone`, `BudChatToolFailed`, `BudChatThinking` and `BudChatPrompt` are highlight groups with default links. It also shows thinking as one line, plans as a checklist, and completes slash commands with `<C-x><C-o>`.
17. **A second real backend checked:** last night's OpenCode 2.2.7 through `opencode acp`, driven by bud's chat with the same scripted model. Session, streaming and an edit in the project all work, and bud's chat now shows the standard ACP diff OpenCode sends. With OpenCode set to ask before edits, its permission request carried the diff, so the review-before-write path (D134) is proven on a real backend too. The protocol layer isn't tied to OpenClaw.
18. `bud ai status` checks a Gateway on the laptop too (the recommended setup): with no URL set, it reads OpenClaw's own config and dials the local Gateway. Checked against a running one.
19. The diff review moved out of the IDE bridge into `bud.ai.review`, so it outlives the bridge if task 6 removes it.
20. The README and troubleshooting page mention the backend. `just check` (everything CI runs, smoke included) passed after item 14.

## Decisions waiting on you

0. **Where the Gateway runs (the big one).** OpenClaw runs tools on the Gateway's own machine, so a Gateway only on your home server would edit the server's files, not your laptop's project. Options are in BASE.md 7.1 and docs/openclaw.md:
   - **A Gateway on each laptop:** bud has been checked against this end to end. The home server's Gateway still serves the apps and the web UI.
   - **One Gateway at home with the laptop paired as a node:** this doesn't work for editing. OpenClaw's docs say node-hosted sessions run in a managed workspace (an empty folder or a fresh checkout), never your open project.

   So I recommend a Gateway on the laptop, signed in to Claude Code and ChatGPT Plus, with the home server's Gateway kept for the phone and the web UI.
1. **Where the Gateway token lives** (BASE.md 7.1). I recommend a mode-600 file named by `ai.openclaw.token_file`, which is what bud does now. The keychain could come later.
2. **Should OpenClaw ask before running commands** (7.2)? It's the Gateway's `tools.exec.mode`, and it applies to every surface. I recommend `ask` (`openclaw config set tools.exec.mode ask` on the server), which is how bud has worked so far.
3. **Claude subscription through OpenClaw.** Reports from April 2026 say Anthropic stopped third-party tools drawing on the Claude subscription bucket, which conflicts with OpenClaw's docs on `claude-cli` reuse. Check Anthropic's current terms before relying on it. ChatGPT Plus through Codex OAuth looks solid.
4. **The error on a redirected buffer.** You asked for an error when something tries to change a pane. I raise the error *and* open the file in the editor window, so nothing you picked is lost. Say if you'd rather it only refused.
6. **Cloud work** (BASE.md 7.4): D129 removed it when Claude Code was leaving altogether. Now that Claude Code stays as a backend (D135), I recommend keeping it for as long as `claude` is installed, so I didn't remove it. Your call.
5. **One OpenClaw session per project** (D139): I decided this with my recommendation, once it was verified. Say if you'd rather have one shared session.

## Left in 1.1 (BASE.md section 6)

- Task 6: move the `claude` backend onto `claude-agent-acp` and remove the stream-json transport and the IDE bridge. Not started; the `claude` backend still runs on the old path.
- Task 9's move of `ai.claude.*` to `ai.agent.*` (41 references across 17 files, mostly on the Claude path task 6 rewrites, so best done with it). The ACP backends already have `ai.agent.autostart` and `ai.agent.idle_minutes`.
- Task 11: removing clod's setup (D130: 91 references across 17 files, including the kit, which syncs after clod) which is decided, but large and tangled enough to deserve a focused session you can review. Cloud work (D129) is now an open question instead (decision 6). `bud claude` itself stays while the `claude` backend uses it (D135).
- Task 13: an end-to-end run with your real home Gateway, Claude Code and ChatGPT Plus. This needs you.
- Task 14 (the user docs beyond docs/openclaw.md) and task 15 (release 1.1.0).

## Where it stopped

At the last commit, everything on the branch is pushed and `just check` passes in full (lint, Go, the whole Lua suite, install, rotate, release-plan, site and smoke).

**Recommended first steps for you:**

1. Read decisions 0 to 5 above. 0 (where the Gateway runs) shapes the rest.
2. On your laptop: `npm i -g openclaw` (needs Node 24.16+), `openclaw onboard`, then `bud ai status`.
3. Try bud's own chat with `<leader>av` and the OpenClaw TUI pane with `<leader>ac`, and tell me how you want the chat to look.
4. When Linear is back, re-aim or close JGA-262 to JGA-276, and file the 1.1 tasks from BASE.md section 6.
