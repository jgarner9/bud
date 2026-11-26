# State, 2026-11-22

Where Bud stands at the handoff from Claude Code to OpenCode. Linear (team JGA, project "Bud Suite", milestone Bud 2.0) is the source of truth for status; this file says how the pieces fit and what to do next.

## Main

Working tree, 2026-11-26: `internal/mail`'s IMAP client is now the go-imap library behind a thin adapter (JGA-399, Suite D138, PR in review). `wire.go`, `utf7.go` and the hand-written transcripts are gone, and the package's tests run against go-imap's own in-process server. It is bud's second third-party Go module, after tidwall/jsonc. Nothing about the mail tools' behaviour changes; the one check it can't make is the first live run against iCloud.

Working tree: a selected overlay fragment can supply a 1Password reference for a rendered OpenCode provider's `{env:VAR}` key. `bud agent` uses the synced reference file at launch; explicit registry references cannot be redirected.

Interactive `bud agent` now gives OpenCode a resizing terminal inside `op run`. The Linux resize check used the real OpenCode TUI with a pipe-producing stand-in for `op`; native macOS verification is pending.

The shared OpenCode shell rules now allow exact workspace inspection commands and common pipeline filters, including `rg --files`, `git branch --show-current`, `git worktree list`, `wc -l` and `sort -u`.

Laptop spend: `opencode stats --cost` shows $0.00 for every Go model by design, not a bug. The Go endpoint reports `cost: 0` (with `billing_source: go-plus`) in its usage object, and OpenCode v2.3.2 treats an endpoint-reported zero as authoritative, so the registry prices rendered into `opencode.jsonc` never enter the sum (verified 2026-11-25 against the endpoint with a free-model probe and the v2.3.2 source; the render itself is correct per the docs and was probed via `hungerg-free`). The real dollar views are the Go console and, for traffic through the router, `ops_spend`; laptop-direct traffic is invisible to Bud, which is JGA-393's argument.

- **Code on main:** everything through PR #116 (see below). `VERSION` is still 1.1.0 (the last tag is v1.1.0); no release has been cut since the Suite work began, and the home server runs bud built from main. A release is a run of the `release` skill (`content/skills/release`) in an OpenCode session, or Jason's call.
- **Content:** format 9 since PR #116 (an older bud refuses it). Edge content is cut on every push to main that changes `content/`; the home server is on stable 2026.11.22.1 and needs the newer edge cut promoted during its rollout (revisit item 22).
- **Diagrams:** `docs/architecture.md` (six Mermaid levels, PR #111). Update it in the same change as the code it draws.

## Built on 2026-11-22

| Issue | What | PR | Decisions |
| --- | --- | --- | --- |
| JGA-377 | Hermes's sandboxed terminal runs alone, with a short ask list | #106 | D114 |
| JGA-378 | One prompt per gateway write: gateway-reviewed servers trusted in Hermes | #108 | D115 |
| JGA-380 | Privacy rule packs: secrets from gitleaks, PII with checksums | #110 | D118 |
| JGA-381 | A calendar or mail call in the history keeps the conversation P0 | #112 | D119 |
| JGA-379 | Approval and tier rules can look at a call's arguments (`when`) | #113 | D116 |
| JGA-382 | Private chats start at P1; the router keeps a raised conversation P0 (sticky, by `x-opencode-session` hash, 30 days) | #114 | D117, D121 |
| JGA-321 | `bud improve`: proposals, measurements, model scan, weekly digest | #109 | D125 to D129 |
| JGA-311, JGA-312 | bud-kb page format, `bud curator`, `bud kb-site` and the Hugo layouts | #115 | D122 to D124 |
| JGA-388 | Architecture diagrams | #111 | |
| JGA-389 | Hermes reaches memory at P0; a recalled P0 record keeps the chat local | #116 | D130 |
| | This handoff folder | #117 | |
| JGA-391 | Spend views: model prices in `opencode.jsonc` for `opencode stats --cost`; `ops_spend` adds tokens and 5-hour and 7-day windows against Go's limits (2026-11-24) | this PR | |

Earlier in the same run (2026-11-21 and the night after): R6b approvals at Bud-owned leaves (#71 to #75), the router and its tiers, spend cap and fallback (#66, #68, #70, #76, #79), mail (#83, #84), memory for the runtimes (#86 to #90), the planning OpenCode (#92, #93), the unattended-deny plugin (#94), registry privacy rules (#95, #96), memory review on Signal (#104) and start lines (#105). `design/build-plan.md` has the roadmap (R1 to R9) with each PR.

## Built on 2026-11-24

| Issue | What | PR | Decisions |
| --- | --- | --- | --- |
| JGA-394 | `bud resume`, the `/handoff` command and the timer in `server/resume/`: a session that hands off queues its resume prompt, and the home server starts the next orchestrator in tmux | this PR | D132 |

Not set up on the home server yet: revisit item 31.

## Built on 2026-11-25

| Issue | What | PR | Decisions |
| --- | --- | --- | --- |
| JGA-398 | JSONC is parsed with github.com/tidwall/jsonc, D1's one stdlib exception; the homegrown stripper is a one-line wrapper over its offset-preserving `ToJSON` | this PR | D153 (BASE) |
| | glm-5.3's Go Plus allowance corrected from 120 to 400 dollars a month (only entry that drifted; opencode.ai/console/go, checked 2026-11-25), so the spend views report the real headroom | this PR | |
| | The syncer tests' git commits no longer inherit the machine's commit signing (one `-c commit.gpgsign=false` in the test helper), so the golden tests run anywhere | this PR | |
| | The 2026-11-19 approval-hook investigation lands in docs/internals: Hermes v0.23.2 and OpenCode 2.3.2 hook and permission boundaries, identity, cancellation and the one-call enforcement gaps, groundwork for the approval and audit-log follow-ups (JGA-374, JGA-376, D134) | this PR | |
| | glm-5.2 removed from the registry and the coding pool: Go's model list no longer has it (`bud registry refresh`, 2026-11-24). Once revisit item 21's improve scan runs, it proposes this class of change itself | this PR | |
| JGA-400 | The release skill is a written review checklist an OpenCode session walks (correctness, tests, docs, secrets, shell and file safety, signing and install paths), so a release no longer needs Claude Code; AGENTS.md and handoff/README.md say so | this PR | D139 a |
| JGA-400 to JGA-408 | Plan to make bud OpenCode-only: nine tasks that delete Claude Code, the backend switch, the bud-ide plugin and cloud work, then reword the docs. Suite D139, BASE D154; plan in `design/opencode-only-plan.md`. JGA-400 to JGA-406 are built (rows above); JGA-407 (user docs and site) and JGA-408 (below) finish it | this PR | D139 |

## Built on 2026-11-26

| Issue | What | PR | Decisions |
| --- | --- | --- | --- |
| JGA-401 | The IDE's Lua talks only to OpenCode: `bud.ai.agent` always starts `opencode acp` and never reads `ai.backend`, `bud.ai.session` is `bud.ai.agent`, and the pane, chat, dashboard, settings screen and `<leader>ac` take the OpenCode path unconditionally. The OpenClaw, Codex, custom and Claude Code backends, their fixtures and `term_backend_test.lua` are gone; `docs/internals/acp.md` describes only the OpenCode transport. `bud.ai.claude` is untouched and still calls a one-line `agent.backend()` stub; JGA-402 deletes both. Follow-ups: the dashboard now reads the Go doctor's check by the name `opencode` (JGA-403 renames the `ai backend` check to match), and the tests that still drive `bud.ai.claude` (`gen`, `issue`, `work`, `claude_*`, `open`) route the session to it through `lib/claude_backend.lua` until JGA-402 ports or deletes them | #137 | D139 |
| JGA-402 | `bud.ai.claude` and the IDE bridge (`bud.ai.bridge`) are deleted with their tests and `lib/claude_backend.lua`, and the `agent.backend()` stub goes. `bud.ai.agent` takes over what `claude.setup` did (the AI keys, `:BudChat`, the `actions`, `issue` and `view` setups) and gains `agent.opened(root)`; `:BudClaude` and the settings screen's Claude Code section are gone, the dashboard no longer special-cases the Go doctor's Claude Code check (it shows until JGA-404 removes it), and `issue.lua` asks for the synced `file-issue` skill instead of checking for the plugin. The `gen`, `issue` and `work` tests now run on the fake ACP agent, which gained `FAKE_ACP_REPLIES` (a reply per prompt) and `FAKE_ACP_RUN` (a command run in the session's directory). `docs/internals/claude-session.md` is now `session.md`. Left for JGA-406: the cloud half of `work.lua` and its test, and the Claude wording in `issue.lua` and `work.lua` | #140 | D139 |
| JGA-403 | The Go side knows only OpenCode: `internal/agent` has `Argv()` (`opencode acp`), `TUI(dir)` and `Check(quick)`, and `config.Backends`, `ai.backend`, `ai.openclaw.*`, `ai.custom.command` and the OpenClaw Gateway probe are deleted (a `config.json` that still has them loads and keeps them on save). `bud ai status` and `bud ai setup` check and install OpenCode only, `bud agent` and `bud chat` take the OpenCode path unconditionally, `bud install` offers OpenCode (`installOpenCode`), and the doctor check is named `opencode`, which the dashboard reads. The Claude Code code and `ai.claude.*` keys are untouched, for JGA-404. The `ai.backend`, OpenClaw and custom rows leave `docs/config.md` and the backend lines leave `docs/commands.md` | this PR | D139 |
| JGA-404 | Claude Code support is gone from the binary: `internal/claude`, `bud claude`, `claudedir.go` and `cli.UseClaudeConfig` (`UseManagedBin` stays, in `managedbin.go`), the Claude Code and Headroom steps of `bud install`, the `claude code` and `claude command` doctor checks, `paths.ClaudeConfig` and the `ai.claude.*` keys (a `config.json` that still has them loads and keeps them on save), plus the dead resume helpers in `agent.go` and `chat.go`. `bud chat` runs `opencode --agent=chat <dir>` in `paths.Chat`, so it needs the synced `chat` agent (`bud sync`). `docs/internals/claude-code.md` and its AGENTS.md row are deleted, the keys' rows and `bud claude` leave `docs/config.md` and `docs/commands.md`, and `docs/setup.md`, `docs/troubleshooting.md` and `docs/issues.md` stop naming `bud claude`. BASE D154 now also supersedes D60, D73 and D76 | this PR | D139, D154 |
| JGA-405 | The embedded `bud-ide` Claude Code plugin is deleted: `ide/plugins/`, `internal/plugin`, `bud plugin`, `bud.Plugin()` and `paths.Plugins`. What the IDE still relied on moved into synced content: `content/skills/file-issue` regained the `bud-issues` and `bud-filed` blocks `bud.ai.issue` parses, and `content/skills/bud-dev` gained the notes on working inside bud's editor and dropped its `ide/plugins/` mention | this PR | D139 (supersedes BASE D142, D145) |
| JGA-406 | Working an issue is local only: `ai.work.where` is gone from `internal/config` and `docs/config.md`, and `bud.ai.work` loses the cloud path (`claude --cloud`, `start_cloud`, `cloud_prompt`, the `claude -p` Linear helper, the saved `cloud-runs.json`, the origin watcher and the `claude/` branch rule) with its tests. The work and issue prompts, notifications and command descriptions say OpenCode and point at `AGENTS.md`; `:BudIssue work ID` takes no `local`/`cloud` argument. The Linear policies in `issue.lua` and `work.lua` matched Claude's `mcp__<server>__<tool>` names, which OpenCode never sends, so they never fired; they now match OpenCode's `linear_<tool>` names (unconfirmed against a live ACP permission request). Left for JGA-407: the user-facing text in `docs/issues.md` still describes cloud work | this PR | D139 |
| JGA-407 | The user docs and site describe OpenCode only: `docs/backends.md`, `openclaw.md`, `claude-code.md` and `claude-tui.md` and their nav lines are deleted, and `keys.md`, `issues.md` (cloud work gone), `troubleshooting.md`, `layouts.md`, `tools.md`, `languages.md`, `README.md`, `site/index.html`, the internals pages and the Linux desktop entry say OpenCode. `git grep -i -E 'claude\|openclaw\|codex'` over `docs`, `site`, `README.md` and `ide/bundles` prints nothing (worklog excepted) | this PR | D139 |
| JGA-408 | The living docs say OpenCode only: the root `CLAUDE.md` is deleted (Jason confirmed all the D139 deletions on 2026-11-26), `AGENTS.md`, `HANDOFF.md` and the handoff files drop the Claude Code wording except history sentences, revisit item 8 is closed, BASE.md marks section 4's intro, 4.2, 4.3 and the decisions D154 supersedes, and the code comments that named Claude are reworded. The Linear policy rename in PR #143 still hasn't met a live ACP permission request (revisit item 33) | this PR | D139 |
| JGA-390 | A delegated Hermes subagent always runs at P0 (D135): the router serves `/p0/v1/...` as P0 whatever `X-Bud-Privacy` says, and the private profile's Hermes config sends `delegation.base_url` there. Whether Hermes 0.23.2 honours `delegation.*` is unconfirmed (revisit item 35) | this PR | D135 |
| JGA-396 | A reviewer-only `retier` action in the memory service (`POST /retier {id, privacy, from, body_hash}`) and `bud memory-review retier <id> <tier>`, which asks on Signal with the whole record and moves it only on his yes. The reviewer token's policy needs `retier` and `export` (revisit item 36) | this PR | D136 |
| JGA-395 | The shared audit log, step 1 of 3 (D140): `internal/audit` (`Open`, `Write`, `Read`; one `O_APPEND` write per line, 0600, events refused over 32 KB or with no source or kind), `paths.Audit` and `docs/internals/audit.md`. Nothing writes to it yet; the leaves follow in the next PR | #149 | D140 |

## The home server

Fully set up and verified by Jason as of 2026-11-22 08:20 UTC: gateway, router, Hermes (both profiles), signal-cli and bud-signal, embeddings, the memory service with memory-mcp through the gateway (Hermes's own memory off), calendar, contacts, mail, and the planning OpenCode as the `bud-planning` user.

None of the 2026-11-22 builds are rolled out there yet. **Jason hasn't done revisit items 21, 22, 23, 24, 27 and 29**; [revisit.md](revisit.md) has them in order, and the overlay's `handoff/home-server.md` has the exact commands. The P1 hold (item 24, `qwen-35b` first in the overlay's `roles.manager.pool`) must go in before item 22's sync and stays until item 29 is done.

## Next, in order

1. **Help Jason through the rollouts** in [revisit.md](revisit.md) when he's at the home server: give paste blocks, read his output, and fix what breaks.
2. **Then the rest of Bud 2.0** (below). Most need the home server, the Qwen box, Herdr or a design session with him, so recommend one and ask.

## Open in Bud 2.0 (Linear)

| Issue | Status | What | Needs |
| --- | --- | --- | --- |
| JGA-321 | In Review | Self-improvement loop | Rollout (item 21), then Done |
| JGA-395 | Backlog | Consolidate the fragmented audit logs into one shared store | JGA-348's narrow follow-up (D134), filed 2026-11-24; JGA-348 is closed as superseded by D97 |
| JGA-399 | In Review | Replace the homegrown IMAP client in `internal/mail` with go-imap | Review and merge; then the first live iCloud run (revisit) |
| JGA-397 | Backlog | Fix the mail-to-self rule's address in the overlay | Jason: the correct send-as is in 1Password |
| JGA-376 | Backlog | Audit Signal approvals | Largely done by D114 to D121; close or narrow with Jason |
| JGA-374 | Backlog | Review final-dispatch approvals, plan isolated home-server verification | Jason |
| JGA-349 | Backlog | Signal reactions and replies to approval requests | Likely stale (built in R6b, D101); confirm and close |
| JGA-301 | In Progress | Ported search tools against a real private search service | A SearXNG instance |
| JGA-368 | In Progress | SQLite memory packaging on the home server | Hard-kill and backup-restore checks, on the home server |
| JGA-317 | Backlog | Isolate untrusted mail ingestion | Jason's mail `--record` output, then the summarizer shape (item 12) |
| JGA-318 | Backlog | A compact personal digest | After JGA-317 |
| JGA-347 | Backlog | Queue private work and wake the Qwen box | Wake-on-LAN isn't enabled on the Qwen box |
| JGA-345 | Backlog | Lemonade behind the router (D98) | The Qwen box |
| JGA-352 | Backlog | Local-first autocomplete (Qwen FIM, MiMo fallback, never on P0) | The Qwen box |
| JGA-295 | Backlog | Measure token use, evaluate Headroom | |
| JGA-291 | Backlog | Verify Hermes, OpenCode and provider contracts | |
| JGA-393 | Backlog | Whether laptop OpenCode goes through the router (D70) | Jason, later (chose 2026-11-24) |
| JGA-322 | Backlog | Back up Suite state and prove recovery | The home server |
| JGA-292, JGA-354 | Backlog | Herdr desktop and mobile | Herdr |
| JGA-375 | Backlog | Custom TUI for phone use | Design session with Jason first; end of 2.0 |
| JGA-353 | Backlog | LSP CLI tools for symbol lookup | Exploration |
| JGA-330 | Backlog | Validate daily workflows and release Bud 2.0 | Last |

Outside the milestone: JGA-356 (self-hostable Bud, exploration), JGA-362 and JGA-363 (Dollarwise, after 2.0), JGA-372 (Bend 2 proofs).

## Working in OpenCode

- OpenCode reads only `AGENTS.md`, which points here. It reaches Linear and Bud memory through the home server's MCP gateway, and GitHub over SSH and `gh`.
- Jason's OpenCode plan is Go Plus, with a monthly token allowance (20% per 5 hours, 50% per week). Prefer cheaper models for mechanical work and keep contexts small: one task per session.
- Sessions on the laptop can't reach `/mnt/project-files` (the Claude project's folder); everything from it is in this folder or the overlay's `handoff/`.
