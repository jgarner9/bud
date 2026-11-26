# State, 2026-11-22

Where Bud stands at the handoff from Claude Code to OpenCode. Linear (team JGA, project "Bud Suite", milestone Bud 2.0) is the source of truth for status; this file says how the pieces fit and what to do next.

## Main

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
| JGA-400 to JGA-408 | Plan to make bud OpenCode-only: nine tasks that delete Claude Code, the backend switch, the bud-ide plugin and cloud work, then reword the docs. Suite D139, BASE D154; plan in `design/opencode-only-plan.md`. Nothing built yet | this PR | D139 |

## The home server

Fully set up and verified by Jason as of 2026-11-22 08:20 UTC: gateway, router, Hermes (both profiles), signal-cli and bud-signal, embeddings, the memory service with memory-mcp through the gateway (Hermes's own memory off), calendar, contacts, mail, and the planning OpenCode as the `bud-planning` user.

None of the 2026-11-22 builds are rolled out there yet. **Jason hasn't done revisit items 21, 22, 23, 24, 27 and 29**; [revisit.md](revisit.md) has them in order, and the overlay's `handoff/home-server.md` has the exact commands. The P1 hold (item 24, `qwen-35b` first in the overlay's `roles.manager.pool`) must go in before item 22's sync and stays until item 29 is done.

## Next, in order

1. **JGA-390** (D135, decided 2026-11-24: an always-P0 subagent path). A delegated Hermes subagent gets its own conversation id, so P0 data the parent writes into its task prompt isn't held by the router's sticky P0. Jason chose option A: the subagent runs through its own router path that is always P0, whatever the parent's tier. Being built.
2. **Re-tiering approved memory records** (D136, decided 2026-11-24: a reviewer-only `retier` action). Hermes saved every record at P1 until D130; the action is `POST /retier {id, privacy}`, asked on Signal like memory-review, and removing a record is deferred. Being built.
3. **Help Jason through the rollouts** in [revisit.md](revisit.md) when he's at the home server: give paste blocks, read his output, and fix what breaks.
4. **Then the rest of Bud 2.0** (below). Most need the home server, the Qwen box, Herdr or a design session with him, so recommend one and ask.

## Open in Bud 2.0 (Linear)

| Issue | Status | What | Needs |
| --- | --- | --- | --- |
| JGA-390 | In Progress | Subagents stay P0 with a P0 parent | D135: always-P0 subagent path; being built |
| JGA-321 | In Review | Self-improvement loop | Rollout (item 21), then Done |
| JGA-395 | Backlog | Consolidate the fragmented audit logs into one shared store | JGA-348's narrow follow-up (D134), filed 2026-11-24; JGA-348 is closed as superseded by D97 |
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
