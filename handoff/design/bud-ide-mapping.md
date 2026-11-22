# Bud IDE in Bud Suite: what to keep, port, replace or drop

> Status: **proposal**, except where Jason has since decided (D14 to D18 in the architecture doc, 2026-11-15: bud-brain rebuilt as the Suite monorepo, OpenCode local beside the IDE, commands under the action tiers, fully off Claude with skills, plugins and environment syncing; D19 and D20: the public bud repo folds into the monorepo as `ide/` and is archived afterwards, and the KB lives in its own repo; D21: the monorepo is public with fresh history, bud-brain stays a private archive, and personal parts live in a private overlay repo). Where this file says "bud" below, read it as the `ide/` part of the monorepo once the fold lands. Nothing in any repo was changed. Every row is a recommendation for Jason to accept, change or reject. Accepted rows become decisions in the architecture doc's log (D14 onward).
>
> **Superseded in part (2026-11-15).** Later decisions in the architecture doc change this mapping. **D44 and D45:** sudo-tee/opencode.nvim replaces bud's own ACP chat (view, review, permissions), and `:BudGen` stays. **D48:** Hermes Agent runs the home server, so D17 now covers coding only. **D49:** planning chats go to a small OpenCode server at home. **D56 to D61:** the monorepo, sync and overlay are decided. Where a row below disagrees, the architecture doc wins.
> Date: 2026-11-15. Companion to [bud-suite-architecture.html](bud-suite-architecture.html), section 10.
> Read: `jgarner9/bud` at v1.1.0 (BASE.md through D149), `jgarner9/bud-brain` (BASE.md through D28, HANDOFF.md), `jgarner9/bud-plugins`, and OpenCode's current (V2) docs from its repository on 2026-11-14.

## 0. The short version

1. **There are three repos, not one, and one of them is a competing plan.** `bud` is the public IDE. `bud-brain` is a private plan written two days ago (2026-11-13) for the same goal as Bud Suite, but built on OpenClaw, Basic Memory and $20 subscriptions. Several of its decisions contradict Bud Suite's (section 2). Those need settling first, because they decide which repo the "Bud repo" in D12 actually is.
2. **bud already speaks to OpenCode.** bud 1.1 talks to its agent over the Agent Client Protocol (ACP), and `ai.backend = "opencode"` runs `opencode acp`, checked against OpenCode 2.2.7. Bud's own chat window, diff review and permission prompts already work on it. The editor side of the move is mostly deleting the Claude Code path, not building a new one.
3. **The biggest gap is where the work runs.** `opencode acp` always starts a private OpenCode server on the machine the IDE runs on; it cannot attach to the home server. And OpenCode runs tools (edits, shell, tests) on the machine its server runs on. So "the IDE is a thin client of the home server" means the home server edits and tests files on the home server, not on the laptop. Section 5, G1 and G2, has the options.
4. **The slim IDE you described fits in about half of today's AI code.** You asked for inline autocomplete, a chat window and generative tooling, built around planning and brainstorming features and around running scripts, tests and metrics, with you doing the thinking. That keeps the editor core, the ACP chat and review, inline actions and the tool generator, and adds FIM completion and a run-and-measure layer. It drops the Claude Code transport and IDE bridge (about 2,150 lines of Lua), the Claude chat agent, and the autonomous "fix this issue in a worktree" flow.
5. **Content ports cheaply.** bud-brain already moved the skills to the neutral AgentSkills format and the agents to a neutral front matter. OpenCode V2 reads `SKILL.md` directories natively (and even `~/.claude/skills`), agents are Markdown with front matter, commands are Markdown with `$ARGUMENTS`. The one trap: **OpenCode V2 reads `AGENTS.md` only, never `CLAUDE.md`**, and the bud repo has only `CLAUDE.md`.

## 1. What exists today

| Repo | What it is | Size and state |
| --- | --- | --- |
| `jgarner9/bud` (public) | Go CLI (standard library only) plus an isolated Neovim config (`NVIM_APPNAME=bud`), all embedded in one signed binary | v1.1.0. ~11,600 lines of Go (32 commands), ~13,400 lines of Lua, of which ~6,700 are AI. 2.0 in progress: plugin moved in, clod and `bud kit sync` removed |
| `jgarner9/bud-brain` (private) | Jason's skills, agents, `core.md`, `servers.json`, and a Go tool binary `brain` whose subcommands do in code what would cost a model many calls | Built: `brain check`, `run`, `locate`, `search`, `fetch`. Not built: `sync`, `mem`, `mail`, `cal`, `brief`, `update`, the stack |
| `jgarner9/bud-plugins` | The Claude Code marketplace: `bud` plugin (10 skills, 2 agents, 1 command, a session-gate hook) and `core.md` | Being absorbed into bud-brain and bud, then archived (bud D145, bud-brain D1) |

## 2. Reconcile bud-brain with Bud Suite first

bud-brain and Bud Suite answer the same questions differently. Each row needs one answer before porting starts, or the port aims at two targets.

| Topic | bud-brain says | Bud Suite says | Recommendation and why |
| --- | --- | --- | --- |
| Backend | OpenClaw Gateway on the home server (D4). bud defaults are written around it too (bud D131, D132, D139) | OpenCode, bootstrapped (D3, D12) | **Supersede bud-brain D4 with OpenCode.** Suite is the newer and more considered decision, and OpenCode is the one with skills, agents, commands and a server API matching what bud-brain wanted. Keep bud's ACP layer: it is what makes the backend swappable. |
| Which repo is "the Bud repo" (D12) | Two repos: `bud` public and generic, `bud-brain` private and personal (D1) | "One Bud repo" for configuration and content | **Keep the two-repo split and name bud-brain the Suite repo.** bud is public and generic (bud D141); the Suite's routes, skills, server config and memory curator are personal and belong in the private repo. `brain` becomes the Suite installer and doctor that D12 describes. Alternative: rename bud-brain to `bud-suite`; cosmetic, your call. |
| Models | $20 Claude and ChatGPT subscriptions, tier map to Claude ids (`models.json`) | OpenCode Go Plus plus local Qwen (D11) | **Keep the tier idea, change the ids.** `fast` = Qwen, `balanced` = a Go Plus included model, `deep` = Go Plus premium. The tier indirection is good: it is how the router's cost tiers (C0 to C2) reach agent files. |
| Memory | Basic Memory is the one store; `brain mem` adds capture, pruning, a time index (D7, D20) | SQLite working memory plus a curated git Markdown KB (D9, D13) | **Supersede bud-brain D7 and D20.** D13 explicitly ruled out a separate Basic Memory project for working memory. `brain mem` can still be the tool name for the Suite's working memory. |
| Heartbeats | None: scheduled scripts gather, one model call summarises (D13) | "Runs scheduled heartbeats and triage" | **Compatible if heartbeats run on Qwen.** bud-brain's reason was subscription cost; Qwen is free. Worth keeping its pattern anyway (code gathers, the model only summarises), since it is faster and testable. |
| Mail and calendar | Send, accept and delete without asking (D11) | Sends need phone approval (D10) | **Conflict, your call.** D10 is newer; bud-brain D11 is more convenient. A middle path: replies in an existing thread and accepting invites are do-alone, new outbound mail asks. |
| Secrets | Keychain or an uncommitted env file (D14) | 1Password with `op://` references (D7) | **Supersede with D7.** `servers.json` already names secrets as `${VAR}` and never stores them, so switching the resolver to `op run` is small. |
| UI | OpenClaw's Control UI, a custom web app later (D10) | Tauri 2 apps (D6) | **Supersede with D6.** Worth knowing: OpenCode V2 also ships a web UI, a desktop app and `opencode pair` (QR sign-in for its app). That could be a stopgap client before the Tauri app exists. Noted, not proposed. |
| Two-tier skill loading | Mode routers in a small `core.md` (D6) | Not covered | **Keep.** OpenCode lists only skill names and descriptions per step and loads bodies on demand, so the router idea still saves tokens. |

## 3. Your requirements for the slim IDE

From your messages today: inline autocomplete, a chat window, and generative tooling; built around planning and brainstorming new features, and around spawning scripts and tests or running tools that check program metrics. Bud is the assistant; you do the legwork, decisions and thinking, and you want full insight into the architecture.

That last point sets the rule used in every table below: **keep what helps you think and check, drop what acts on its own.** A feature that hands the agent a task and a worktree and comes back with a PR is the opposite of what you described.

| Requirement | Where it runs | Proposed shape | Candidates checked |
| --- | --- | --- | --- |
| Inline autocomplete | Neovim straight to the Qwen box, outside OpenCode. Latency rules out a round trip through an agent loop | Ghost text as you type, plus a blink.cmp source, from a fill-in-the-middle (FIM) model on llama.cpp | **minuet-ai.nvim** (recommended: virtual text and a blink.cmp source, so it fits the blink.cmp bud already uses; `openai_fim_compatible` provider for llama.cpp or Ollama; Neovim 0.10+). **llama.vim** (ggml-org: llama.cpp only, FIM plus instruction edits, reuses context cleverly on weak hardware; a good pick if llama.cpp is the Qwen server, Q7). |
| Chat window | OpenCode, through ACP or OpenCode's HTTP API (depends on G1) | Side pane or full screen, rendered by bud | **bud's own chat** (`bud.ai.view`, already built, tested with OpenCode over ACP, looks how you choose: bud D138). **sudo-tee/opencode.nvim** (HTTP to any OpenCode server including a remote one, V1 and V2, side panel, sessions, session diff review). **NickvanDyke/opencode.nvim** (connects to any server, keeps OpenCode's own TUI). **codecompanion.nvim** (ACP plus direct models, inline transforms; broad but a second framework). |
| Generative tooling | OpenCode | Select code, describe a change, review the diff, accept or reject. Plus `:BudGen` drafting editor tools | **bud's own** `bud.ai.actions` + `bud.ai.review` + `bud.gen` (built; the review already shows OpenCode's diffs before writing when edits are set to ask). The plugins above all have an equivalent if bud's chat is replaced. |
| Planning and brainstorming | OpenCode, ideally on the home server where memory and the KB live | A `plan` agent that can read and search but not edit code, loading the planning skill, writing specs into the project's `docs/` and proposing Linear issues for you to approve | OpenCode's built-in `plan` agent plus the `planning` and `plan-to-linear` skills; Linear's MCP server (already opt-in in `servers.json`) |
| Run scripts, tests and metrics | Wherever the code is (G2) | Commands that run a check and return a compact result: `/check`, `/metrics`, `/bench`. Results land in the chat and the quickfix list | `brain run` (built: runs a command, keeps the full log on disk, returns exit code, failures and a tail) is exactly this. OpenCode commands can embed shell output with `` !`cmd` ``. Metric tools to wire: `go test -cover`, `scc` or `tokei`, `hyperfine`, bud's own startup timer |

### Running checks and the action tiers

D10 lists reads, search, memory and drafts as do-alone, and pushes, merges, spending and infrastructure as approve-first. **Running a local test or script is in neither list.** Proposal, for you to confirm as a new decision:

- **Do alone:** commands inside the project workspace from an allowlist (the test runner, `just check`, `brain run`, linters, metrics tools). They change nothing outside the checkout and can be rerun.
- **Ask in the editor** (OpenCode `ask`, answered in bud's permission float, not the phone): any other shell command in the workspace.
- **Approve first by phone** (unchanged D10): `git push` to main, merges, anything that installs packages system-wide, touches the network outside docs lookups, or runs outside the workspace.

This maps directly onto OpenCode's ordered `permissions` rules (`action: "shell"`, `resource: "go test *"`, `effect: "allow"`). One caution: shell blocks inside OpenCode *commands* run outside the permission flow, so commands should only use fixed, trusted command lines.

## 4. The mapping

Legend: **Keep** as is · **Port** to an OpenCode equivalent · **Replace** with something else · **Drop**. Each row gives the reason.

### 4.1 Go CLI commands (`internal/cli/`)

| Command | Call | Why |
| --- | --- | --- |
| `bud install` | Keep, slim | Profiles, terminals, languages and honest package installs are the IDE's job and work well. Drop its Claude Code and GitHub setup steps. |
| `bud update` (signed releases, background check, rollback on a failed doctor) | Keep | This is the "code update" channel of Suite section 8 already built, with signatures and rollback. Worth copying into `brain update` rather than writing again. |
| `bud doctor` | Keep, extend | Add checks: OpenCode installed at the pinned version, the configured server reachable, the Qwen FIM endpoint answering under a latency budget. Remove the Claude Code check. |
| `bud lang`, `bud layout`, `bud projects`, `bud config`, `bud tools`, `bud completion`, `bud version`, `bud dev link`, `bud uninstall` | Keep | Editor plumbing with no Claude dependency. |
| `bud ai status\|setup` | Port | Already backend-neutral. Make `opencode` the default, and teach it the server URL and the Qwen endpoint. Drop the OpenClaw-specific token-file checks if OpenClaw is dropped. |
| `bud claude`, `claudedir.go`, `bud plugin` (extracting the embedded Claude plugin for `--plugin-dir`) | Drop | Claude Code is no longer the runtime (D2). OpenCode finds project skills in the checkout itself (4.3), so no plugin extraction is needed. |
| `bud agent`, `bud chat` | Replace | They exec a backend's TUI. Replace with `opencode --server <url>` (attach to the home server) and a planning chat on the `plan` agent. Small code, different targets. |
| `bud issue` | Replace | Its job (where filed issues go in Linear) becomes the `file-issue` skill plus Linear MCP on the server. The CLI command is no longer needed. |
| `bud launch` | Keep | Opening bud in Kitty or Ghostty is cheap and you use it. |
| `bud bundle` (macOS and Linux desktop launchers) | Drop, your call | The Tauri app (D6) becomes the desktop presence. The bundles are ~1,000 lines of Go whose only job is to launch a terminal. |
| `bud bootstrap` (install bud on another machine over SSH) | Keep | Directly useful for putting the IDE on the home server if code ends up living there (G2). |
| `github.go` (`gh` for the dashboard's PR panel) | Keep, optional | Small, and useful while you are reviewing. |

### 4.2 Neovim config (`nvim/`)

| Piece | Call | Why |
| --- | --- | --- |
| Editor core: options, keymaps, fzf-lua, oil, neo-tree, gitsigns, diffview, aerial, dropbar, mini.*, conform, nvim-lint, lspconfig, mason, treesitter, blink.cmp, dap, render-markdown, catppuccin; the 50 ms startup budget; the committed `vim.pack` lockfile | Keep | This is the IDE, it is fast, and none of it depends on Claude. The pinned lockfile is the reason every machine matches. |
| Languages (8 specs plus the template) and `bud.lang` | Keep | One file per language is exactly the "one source of truth" principle. |
| Layouts `default`, `focus`, `review` | Keep | `review` suits your "I review, Bud drafts" workflow. |
| Layout `debug` | Keep or drop, your call | Only if you debug with dap often. |
| Settings screen, statusline, marks, sessions, bigfile, markdown, messages | Keep | Small, self-contained editor features. |
| Dashboard and the robot (`dashboard/`, `buddy.lua`, 22 moves, ~1,800 lines) | Your call | It is personality, not function, and it is lazy-loaded so it does not cost startup. Slim option: keep the project list, health line and update banners, drop the animation. |
| `bud.ai.acp` (the ACP protocol, 247 lines) | Keep | The one protocol layer, already tested with OpenCode. |
| `bud.ai.agent` (sessions, one per project, `session/load` reattach) | Port | Make it the only session module; strip the OpenClaw-only paths if OpenClaw goes. |
| `bud.ai.view` (bud's own chat), `bud.ai.review` (diff review), `bud.ai.permissions` | Keep | Chat window, generative-edit review and in-editor permission prompts: three of your requirements, already built and backend-neutral. |
| `bud.ai.actions` (explain, refactor, write tests on a selection; commit messages) | Keep, port | Generative tooling on a selection. Point it at the ACP session instead of typing into a TUI. |
| `bud.gen` and `bud.tools` (`:BudGen` drafts and load-tests editor tools) | Keep | Generative tooling that improves the IDE itself, with a test loop. |
| `bud.ai.term` (the backend's TUI in a pane) | Keep, slim | Useful fallback: `opencode --server <url>` in a pane gives OpenCode's full UI when bud's chat lacks something. |
| `bud.ai.claude` (1,067 lines) and `bud.ai.bridge` (WebSocket server, MCP tools, `~/.claude/ide` lock files, ~1,075 lines) | Drop | Claude Code's stream-json transport and IDE protocol. ACP replaces both. |
| `bud.ai.chat` (Claude Code's TUI as the `bud-ide:chat` agent) | Replace | With bud's own chat on an OpenCode `chat` agent (4.3). |
| `bud.ai.issue` (file a report as Linear issues, 763 lines) | Replace, slim | Keep a small float that sends the report with editor context to the session; the `file-issue` skill does the rest and you approve before anything is filed. Most of the 763 lines parse Claude-specific reply blocks. |
| `bud.ai.work` (have the agent fix an issue in a worktree, 729 lines) | Drop | Autonomous work is the opposite of "my assistant while I do the thinking". Can come back later if you want it. |

### 4.3 Claude content (skills, agents, hooks, commands, instructions)

| Today | OpenCode V2 equivalent | Call | Why and how |
| --- | --- | --- | --- |
| Skills: `build`, `code-style`, `debugging`, `learn`, `planning`, `plan-to-linear`, `release`, `tools`, `verify`, `bootstrap-project` (bud-brain `skills/`) | Skills: `SKILL.md` directories, found in `~/.config/opencode/skills`, `.opencode/skills`, `.agents/skills`, `~/.claude/skills`, or any path or HTTPS catalog in the `skills` config array | Port | Already in the right format. Point the server's config `skills` array at bud-brain's `skills/`. IDs come from the directory name, so the `bud:` prefix is already gone. Edit `release`: it runs Claude Code's `code-review` and `security-review` skills, which OpenCode lacks; replace with a `reviewer` subagent. |
| `core.md` (always-on instruction, ~1.3 KB) | Global `~/.config/opencode/AGENTS.md`, or an `instructions` entry | Port | OpenCode loads it every session and re-reads it when it changes, so content updates apply without restarts. |
| Agents `coder`, `code-reviewer` (neutral front matter: `role`, `tier`, `effort`, `access`) | Agents: Markdown in `.opencode/agents/` or `agents` in config, with `mode`, `model` (`provider/model#variant`), ordered `permissions` | Port | `role` maps to `mode` (`subagent`/`primary`), `tier` to `model` through `models.json`, `effort` to the `#variant`, `access: read` to deny rules for `edit` and `shell`. This is `brain sync`'s job (bud-brain task 13). With your "I do the thinking" focus, `coder` matters less than a `reviewer` and a `runner` (allowlisted shell, reports results). |
| `bud-ide:chat` agent (no edit, no shell) | A `chat` agent, `mode: primary`, deny `edit` and `shell` | Port | Direct translation. |
| `bud-ide:bud-dev` skill | `.opencode/skills/bud-dev/SKILL.md` (or `.agents/skills/`) inside the bud repo | Port | OpenCode finds project skills in the checkout, so it applies only when working on bud. That is what bud D142 and D148 built `--plugin-dir` for; no extraction needed. Its text is stale: it still describes `kit/kit.json` and `bud kit sync`, which D144 removed. |
| `bud-ide:file-issue` skill | Server skill | Port | Keep the "propose first, file only after you say so" rule; it matches D10. |
| `SessionStart` hook printing `inside-bud.md` when `BUD_BIN` is set | No Markdown hook equivalent. Options: an `instructions` entry the IDE's own config adds, or one line of context bud puts in the first prompt | Replace | OpenCode has no shell-command hooks; its hooks live in TypeScript plugins (prompt admission, context, compaction, HTTP). A plugin for one paragraph is too much; the IDE passing context is simplest. The text also needs rewriting: it talks about Claude Code's IDE bridge. |
| Session-gate hook `gate.sh` (refuse a new issue until context is cleared) | A TypeScript plugin `prompt` hook could do it | Drop | bud-brain D24 already dropped it. Its rule stays as text in the `build` skill. |
| Command `/plan-to-linear` | Commands: `.opencode/commands/<name>.md`, `$ARGUMENTS`, `$1`, `` !`shell` ``, optional `agent`, `model`, `subagent` | Port | It is already a skill too; keep the command as a one-line wrapper if you like typing it. Add `/check`, `/metrics`, `/plan` the same way. |
| `CLAUDE.md` in bud (and the `CLAUDE.md` → `AGENTS.md` pointer in bud-brain) | `AGENTS.md`, loaded from the workspace up to the project root plus the global one | Port | **OpenCode V2 ignores `CLAUDE.md` entirely.** bud has no `AGENTS.md`, so today OpenCode working on bud gets none of its architecture notes. Rename it, and keep a one-line `CLAUDE.md` pointer only if a Claude surface still needs it. |
| `servers.json` (MCP servers, `${VAR}` secrets) | `mcp` in OpenCode config (local and remote, headers, OAuth) | Port | Rendered by `brain sync`. Resolve the `${VAR}`s with `op run` at server start (D7). Re-check each server: `serena` and `cocoindex-code` are coding-only, `headroom` may be replaced by `brain run`. |
| `models.json` tiers | `model` on each agent, `default_agent`, the route table | Port | Swap the Claude ids for Qwen and Go Plus models (section 2). |
| Claude marketplace (`bud-plugins/.claude-plugin`) | OpenCode plugins are npm or git packages of TypeScript, a different thing | Drop | Nothing in it is code that needs to be a plugin. Archive per bud D145. |

### 4.4 Update and sync

| Today | Call | Why |
| --- | --- | --- |
| The binary is the release; `nvim/`, languages, layouts, tools embedded and synced into `~/.config/bud` with a manifest | Keep | It is the Suite's "code update" path, already signed and with rollback. |
| `bud kit sync` (Claude plugins and marketplace) | Already gone (D144) | Content now reaches OpenCode on the server instead. |
| Content: skills, instructions, agents, commands | Port to OpenCode's own reload | The server pulls bud-brain; OpenCode reloads changed skills, commands and `AGENTS.md` without a restart. That is Suite section 8's "content updates never need a new app build" for free. An HTTPS skills catalog (`index.json` with a `version`) is the alternative if a client ever needs skills without a checkout. |
| Nightly builds, release signing, `rotate-release-key.sh`, GoReleaser, docs site | Keep | Release machinery with no Claude dependency. The `bud:release` skill is the one piece to port (4.3). |

### 4.5 Kit and layouts

The kit is already removed (D144, D149), so there is nothing to map except the stale mention in the `bud-dev` skill. Layouts are kept (4.2); the AI pane kind becomes "chat" on OpenCode.

## 5. Gaps: where OpenCode has no direct equivalent

| # | Gap | Options | Recommendation |
| --- | --- | --- | --- |
| G1 | **ACP cannot reach the home server.** `opencode acp` starts a private server on the IDE's machine and never connects to a shared one. Only OpenCode's TUI (`--server <url>`), web UI, app and HTTP API can. | A. Keep ACP to a local OpenCode for coding. B. Teach bud's session layer OpenCode's HTTP API (or adopt sudo-tee/opencode.nvim) to reach the server. C. A for coding, B for planning and brainstorming chats. | **C.** It keeps the built, tested ACP path for code, and puts the thinking sessions where memory and the KB live. |
| G2 | **Tools run where the server runs.** A home-server OpenCode edits files and runs tests on the home server, not on the laptop in front of you. bud-brain hit the same wall with OpenClaw and sketched a "paired local worker". | A. Code lives on the home server; you edit there over SSH (bud bootstrap installs the IDE there). B. Coding runs a local OpenCode on the dev machine with the same Bud-rendered config and the same Go Plus key from 1Password; the home server runs the manager. C. A remote worker. | **B.** It is the least new code and runs tests on the machine you are looking at. It does bend principle 4 ("the server is smart, the clients are thin") for coding only, so it needs your decision. |
| G3 | **Inline completion bypasses the router (D4).** FIM requests go straight from Neovim to a model, not through OpenCode or the privacy gate. | A. Qwen only; completion turns off when the Qwen box is off. B. Fall back to a Go Plus model. | **A.** Completion sends your code as you type; with no router in the path, the only safe default is a model inside the house. Fails closed, like the rest of section 6. |
| G4 | **The FIM model is unverified.** Qwen 35B MoE is a general model; FIM needs special tokens, and completion needs a fast first token. | A. The 35B, if it supports FIM. B. A small coder model beside it on the same box (Qwen2.5-Coder 1.5B or 7B are minuet's and llama.vim's documented examples; Qwen3-Coder-30B-A3B if it fits). | **B, then measure.** Unchecked: whether the 35B has FIM tokens. Set a budget (for example p50 under 300 ms) and let `bud doctor` measure it. |
| G5 | **No shell hooks.** Claude Code's `SessionStart`, `PostToolUse` and `UserPromptSubmit` command hooks have no Markdown equivalent; OpenCode hooks are TypeScript plugin code. | Plugin, or move the behaviour elsewhere. | Move it elsewhere (4.3). Only reach for a plugin when a hook enforces something, such as the D10 tiers. |
| G6 | **Phone approvals (D10) are not built in.** OpenCode's `ask` goes to whichever client holds the session. Pushing it to the phone needs a plugin on the permission flow, or the D12 wrapper. | Server plugin on permissions, or wrapper. | Not an IDE problem: in the IDE you answer in the editor. Flagged so the Suite plan accounts for it. |
| G7 | **Claude Code features with no OpenCode twin:** `code-review` and `security-review` skills (used by `bud:release`), `--agent` on the CLI, `@file#L3-4` mentions. | Subagents and skills of your own; bud already names places in words. | Port `release` onto a `reviewer` subagent. |
| G8 | **bud's chat forgets on restart.** The conversation survives closing the pane, not restarting Neovim (bud `docs/internals/acp.md`). | Replay with `session/load`, which OpenCode supports. | Small task in the IDE port. |

## 6. Rough migration order

Each step ships and can be checked on its own.

0. **Decide section 2 and G1, G2.** Test: the decisions log has the new entries, and bud-brain's BASE.md marks its superseded ones.
1. **Content port, no code.** Add `AGENTS.md` to bud, point a local OpenCode at bud-brain's `skills/`, convert the two agents and the chat agent, update `models.json`. Test: `opencode` in the bud checkout lists the skills and loads `bud-dev`.
2. **IDE on local OpenCode.** Set `ai.backend = "opencode"` as the default and edits to `ask`. Test: your daily loop (chat, inline actions, review, commit message, `:BudGen`) works with Claude Code uninstalled.
3. **Remove the Claude path.** `bud.ai.claude`, the bridge, `bud.ai.chat`, `bud claude`, `bud plugin`, `bud.ai.work`, the Claude docs. A breaking bud release. Test: `just check` and the smoke test pass; `rg claude` finds only history.
4. **Inline completion.** minuet-ai.nvim on blink.cmp and virtual text, pointed at the Qwen FIM endpoint, with a doctor check for latency. Test: suggestions appear within the budget, and nothing is sent when the Qwen box is off.
5. **Run and measure.** `/check`, `/metrics`, `/bench` commands and a `runner` agent using `brain run`, with the shell allowlist from section 3. Test: a failing test returns its failure lines and the log path, not the whole log.
6. **Planning and brainstorming on the home server.** A planning chat in the IDE attached to the server (G1, option C), on the `plan` agent with the planning skill and Linear. Test: a brainstorm produces a spec file and a proposed issue list, and nothing is filed until you approve.
7. **Slim the rest.** Bundles, dashboard animation, debug layout: whatever you decide in 4.1 and 4.2.

This replaces step 4 of the Suite build order ("Bud IDE as a client") with steps 1 to 6 here, and steps 1 to 3 can start before the home server exists.

## 7. Questions for you

- **Q8.** Answered: bud-brain, rebuilt as one monorepo (D14, D18). The proposed tree and sync are in the architecture doc, section 8.
- **Q9.** Answered: OpenCode runs locally beside the IDE (D15).
- **Q10.** Answered: running commands is under the action tiers (D16); the allowlist is in the architecture doc, section 9.
- **Q11.** Inline completion: Qwen only and off when the box is off (recommended), or a cloud fallback?
- **Q12.** Chat window: keep bud's own ACP chat (recommended, already built and yours to style), or adopt sudo-tee/opencode.nvim?
- **Q13.** Mail autonomy: bud-brain D11 (act, then report) or Suite D10 (approve sends)?
