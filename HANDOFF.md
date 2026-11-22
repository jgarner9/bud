# Handoff — Bud Suite 2.0, 2026-11-22

Written at the end of an overnight OpenCode run that continued a Claude Code
session which paused when its usage ran low. For whoever resumes (Claude Code,
or OpenCode again). Linear (team JGA) is the source of truth for status; each
ticket below carries its own `Now`, `Behaviour` and `Done when`.

## What this run did

- Connected OpenCode to Linear and to Bud memory through the home server's MCP
  gateway, and to GitHub over SSH, then read every in-flight and remaining
  ticket so the plan below is reconstructed even where the earlier session's
  notes are not on disk.
- It did **not** write feature code. The per-workstream notes and the
  architecture design doc lived in the earlier Claude Code sandbox and are not
  on disk (see "Lost artefacts"). The ticket bodies carry the decisions, so
  work can resume from them, but the security-sensitive pieces should not be
  rewritten from a half-memory of the design.

## State

- bud `main` is clean at `c4d1913`. Merged tonight: #102 (references with spaces
  inside 1Password names), #103 (Hermes memory off), #104 (memory-review over
  Signal), #105 (gateway, router and memory-review log their start line without
  `--verbose`). The private overlay merged its planning-profile and
  memory-gateway PRs (#7, #8).
- The home server is up: gateway, memory-mcp, embed and the planning OpenCode
  all active.

## In flight (draft branches, not merged)

### Approvals and privacy

Build in this order, JGA-382 last so nothing private reaches the cloud early.

- **JGA-377** — hermes-gate: sandboxed terminal commands run alone, with a short
  ask list (Suite D114). *In Progress.* Hermes gets its own shell rule set in
  the server fragment: default `allow`, an ask list for effects that leave the
  sandbox (`git push`, `ssh`/`scp`, `npm publish`, `gh release`, host-mounted
  paths), `op` still denied. `$( )`, backticks and redirects ask only when they
  contain an ask-list command; a path-qualified command (`./git`) never matches
  a bare one. Table tests cover the ask list, compound commands, subshells and
  path-qualified commands; `docs/internals/sync.md` and `content/env/server.jsonc`
  describe the new default; coding-device rules are unchanged.
- **JGA-378** — one prompt per gateway write: trust gateway-enforced servers in
  Hermes (D115). Linear, calendar and mail are `trust: untrusted` in Hermes
  today, so Hermes asks and then the gateway asks again. `bud sync` renders any
  server with `gateway: true` and an `approval` block as trusted; GitHub, which
  does not go through the gateway, stays untrusted. `bud doctor` fails when a
  server Hermes trusts has no `approval` block at the gateway.
- **JGA-380** — privacy rule packs: secrets ported from gitleaks, PII with
  checksums (D118). A generator turns a curated subset of gitleaks' MIT
  `config/gitleaks.toml` into JSONC rules under `content/env/`, plus named Go
  validators for card numbers (Luhn), IBANs (mod-97), US SSNs and phone
  numbers, plus a personal deny-list that lives only in the overlay's
  `registry.jsonc`. Every rule only raises the tier, at both the router and the
  gateway.
- **JGA-381** — router: a calendar or mail result keeps the whole conversation
  P0 (D119). The router reads `tool_calls[].function.name` in the request
  history and raises a call to a P0-only server to P0, marking
  `X-Bud-Privacy-Raised: provenance`. The P0-only servers derive from
  `bud.mcp.*.privacy` so the list is not written twice; names match with and
  without the runtime prefix.
- **JGA-379** — approval rules keyed by tool arguments, at the gateway and in
  hermes-gate (D116). An `approval` or tier entry can carry a `when` matcher
  over argument fields (equals, glob, absent, empty list); a `forbid` rule beats
  `allow`, and an unreadable field means ask. Stdlib matcher, not OPA or Cedar.
  First rules: `calendar_create_*`/`calendar_move_*` with no attendees run
  alone; `mail_send_*` to Jason's own address runs alone; `cronjob_manage` with
  action `list` runs alone.
- **JGA-382** — private Signal chats default to Go models; quick questions get a
  cloud role (D117, D121). Hermes's private profile sends `X-Bud-Privacy: P1`
  to the router for chat (P0 for calendar/mail). The router raises a chat to P0
  on the `#private` marker, the rule packs, or provenance. For P1 the `manager`
  pool puts a Go model first; Qwen serves P0; a new `bud/quick` role on a Go
  flash model handles short questions. Supersedes the D95 "private profile
  lists only P0 models" rule.

### KB curator

- **JGA-311** — curate facts into the git KB and manage stale or conflicting
  entries (parent JGA-338). Promote a fact after two independent occurrences or
  an explicit doc-commit; rewrite supported changes; queue unresolved
  contradictions; commit every edit with its evidence. Flag sections at six
  unused months, archive at twelve; archival is reversible; with Qwen
  unavailable the curator pauses without sending personal memory to cloud.
- **JGA-312** — organise bud-kb and publish its Hugo view on the tailnet (parent
  JGA-338). Project/system page conventions, metadata for reference age and
  evergreen sections, links to Linear for status; render navigation, sources
  and archive/review indicators from the same Markdown; no public exposure.

### Self-improvement

- **JGA-321** — propose self-improvement changes and a weekly model/tool review
  (parent JGA-341). Collect redacted failures, corrections, usage and
  evaluation results; propose content/registry changes as reviewable diffs with
  evidence and success criteria; scan public model/tool developments weekly.
  Nothing silently edits its own models, permissions or locked skills; Jason
  reviews each proposal; personal proposals target the overlay.

## Remaining 2.0

| Ticket | Status | Title |
| --- | --- | --- |
| JGA-348 | In Progress | Build one approval policy and audit log with runtime adapters |
| JGA-301 | In Progress | Connect the ported search tools to a real private search service |
| JGA-374 | Backlog | Review final-dispatch approvals and plan isolated home-server verification |
| JGA-349 | Backlog | Connect Signal reactions and typed replies to approval requests (likely stale) |
| JGA-347 | Backlog | Queue private work and wake the local-model machine on demand |
| JGA-295 | Backlog | Measure token use and evaluate optional Headroom compression |
| JGA-292 | Backlog | Integrate Bud's IDE into the Herdr desktop workspace |
| JGA-354 | Backlog | Connect Herdr mobile to Bud agents and approvals |
| JGA-330 | Backlog | Validate daily workflows and release the unified Bud 2.0 |
| JGA-376 | Backlog | Audit Signal approvals: what can run alone and what must stay gated |

## Lost artefacts

The earlier Claude Code session ran in a sandbox and wrote the per-workstream
notes (`approvals-routing.md`, `kb-curator.md`, `self-improvement.md`) and the
architecture design doc (`architecture/autonomy-and-privacy-design.md`,
referenced by every JGA-377..382 ticket as the design source for D114..D121)
into that sandbox. They are not on disk on the laptop or the home server, and
the memory service was freshly initialised tonight, so they did not survive.

The decisions survive in two places: the ticket bodies (D114..D121, each with
its `Behaviour` and `Done when`) and the decision-number ranges
D114..D121 approvals/evidence-based privacy, D122..D124 curator,
D125..D129 self-improvement. Treat the ticket bodies as authoritative and
re-derive the design doc from them if it is needed; do not guess the design.

## Starting points in the code

- JGA-377: `internal/adapter/hermes/gate.go` (`Tiers.command`, `splitCommand`
  and the shell rules it reads), `content/env/base.jsonc` (the coding-device
  shell rules Hermes currently reuses) and `content/env/server.jsonc` (where
  Hermes's own rule set goes), rendered by `internal/syncer/`.
- JGA-379/380/381: `internal/registry` and the router/gateway rule handling
  (`internal/router`, `internal/mcpgateway`).
- JGA-378/382: `internal/syncer` (Hermes `mcp_servers` trust and the private
  profile's tiers/models).

## Resuming

Paste into Claude Code (which has Basic Memory, Linear and GitHub):

```text
Resume Bud Suite 2.0 from the 2026-11-22 handoff. Read this repo's HANDOFF.md,
then rehydrate from Linear: the in-flight tickets are JGA-377..382 (approvals
and privacy, in the order 377, 378, 380, 381, 379, 382 — 382 last), JGA-311/312
(KB curator) and JGA-321 (self-improvement). The per-workstream notes and the
architecture design doc were lost with the previous Claude Code sandbox; the
ticket bodies carry the decisions, so work from those and re-derive the design
doc only if needed. Reconcile git state first: fetch all remotes, then continue
the workstreams in order, one commit each, In Review, then Done after release.
The draft branches were never pushed, so start them fresh from main at c4d1913
using the gitBranchName on each ticket. Before touching the home server or any
credential, stop and ask about the open items in the previous handoff.
```
