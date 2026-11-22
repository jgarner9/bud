# Bud Suite build plan

> **Historical status (handoff note, 2026-11-22).** The status line below is from 2026-11-15. Every phase 1 PR it lists is merged, and the pinned OpenCode install landed in the 2026-11-18 to 20 work. Current status is the "Roadmap from 2026-11-20" section at the end and `handoff/state.md`.

Status: **approved by Jason, 2026-11-15. Phase 1 merged 2026-11-15 13:24Z (PRs #3-#9, #11-#15); the pinned OpenCode install from 1.7 is still to do.** Built so far, as open PRs on jgarner9/bud: 1.1, 1.2, 1.3, 1.5, 1.6, 1.7 and the leak-check half of 1.10 (PR #7). Refines section 11 of `bud-suite-architecture.html` into phases of small, shippable tasks. Tracked in Linear under Jason's Bud 2.0 milestone (project Bud Suite): each task heading names its issue, and phases 2 to 7 map onto the phase parents JGA-335 to JGA-341. Decisions are cited by number from that doc's log (D1 to D90).

## How this differs from section 11

Section 11 put "runtime on the server" first. This plan starts with the installer and sync spine instead, because every later phase installs and configures itself through it (D12, D46, D60), and all of it can be built and tested on a laptop with no home server, no Qwen box and no credentials. The server runtime moves to phase 2.

Each task is one PR against `jgarner9/bud` with its own tests, and `just check` must pass. Tasks name their files and the check that says they're done.

## Phase 1: installer and sync spine (laptop only, no credentials)

Goal: on a clean machine, `bud install --device <name>` picks the device's components, `bud sync` pulls a signed content bundle, layers the overlay on top and renders OpenCode's config from the registry, and `bud doctor` reports drift. Test: delete `~/.config/opencode` and `~/.local/share/bud/content`, run `bud sync`, and get the same files back byte for byte; tamper with one and `bud doctor` names it.

### Task 1.1: monorepo skeleton (D22, D56) (PR #3) (JGA-343)
Move today's IDE tree (`nvim/`, `languages/`, `layouts/`, `tools/`, `migrations/`, `bundles/`, `plugins/`) under `ide/`. `cmd/bud` and `internal/` already sit at the root, so they stay. Add `content/`, `profiles/`, `server/`, `app/` with a short README each saying what goes there and which phase fills it, and an `AGENTS.md` (OpenCode reads only that, D17). Update `embed.go`, the justfile, CI, the smoke and Lua test scripts, and the docs that name the old paths.
Files: `embed.go`, `justfile`, `.github/workflows/*.yml`, `scripts/*.sh`, `CLAUDE.md`, `AGENTS.md`, `docs/internals/*.md`, new dirs.
Done when: `just check` passes and `bud update` on an existing install produces the same `~/.config/bud` (the synced tree's paths don't change, only where they come from in the repo).

### Task 1.2: the registry and its schema (D46, D58) (PR #4) (JGA-298)
`content/env/registry.jsonc` with providers, models and the nine roles (manager, coding, planning, classifier, fim, embedding, fallback, curator, vision), and `internal/registry` that parses JSONC with the standard library (comments and trailing commas stripped, then `encoding/json`), validates it and merges an overlay registry over it by ID. Validation: every role names a model that exists, every model names a provider that exists, every key is an `op://` reference or `{env:VAR}`, privacy tiers are P0/P1/P2, fallbacks don't loop. The public file uses placeholder hosts only (`qwen.invalid`), with real ones left to the overlay. A `docs/registry.md` documents the format, one file per format.
Done when: tests cover each validation error with a message that says what to fix; `bud registry check [file]` exits 1 on a bad file.

### Task 1.3: device profiles (D18, D57, D61) (PR #5) (JGA-298)
`internal/profile` and `profiles/{dev-machine,server,model-box}.json` examples. A profile names `components` (`ide`, `harness`, `app`, `server`, `qwen`), `channel` (`stable` or `edge`), and `content` globs (skills, agents, commands, plugins, env fragments). Validation rejects unknown components and channels. Config key `device` remembers the chosen profile.
Done when: tests cover the examples and each error; `docs/profiles.md` documents the format.

### Task 1.4: component selection in `bud install` (PR #11) (JGA-320)
`bud install --device <name>` (asked on the terminal when missing) loads the profile and runs only its components. `ide` is today's install flow, unchanged. `harness` installs OpenCode at the pinned version (task 1.7). `server`, `app` and `qwen` fail with "not built yet: phase N" rather than exiting 0. Existing installs with no `device` behave as a dev machine with only `ide`, so nothing changes for them.
Done when: tests with recorder components show the right ones run for each example profile.

### Task 1.5: content bundles, `channel.json` and signatures (D60, D61) (PR #6) (JGA-320)
`internal/content`: build `content-<version>.tar.gz` plus `content.json` (each file's path, sha256, size and kind) and `channel.json` (newest code and content versions per channel), signed with the existing release key in a separate SSH signature namespace (`bud-content`), so a content signature can never pass as a code signature or the reverse. Verify on the device: signature first, then every hash, and refuse a version older than or equal to the installed one unless `--allow-downgrade` names it (rollback goes through the ledger, not a download). A `bud content pack` subcommand is what CI runs. Versions are `YYYY.MM.DD.N` (section 8).
Done when: tests with a throwaway key cover a good bundle, a bad signature, a wrong namespace, a hash mismatch, a downgrade and a path that escapes the bundle (`../`, absolute, symlink).

### Task 1.6: overlay layering (D21, D59) (PR #8) (JGA-297)
`internal/overlay`: clone or fast-forward the overlay over git (`--overlay <git url>`, `BUD_OVERLAY`, or config `overlay.repo`) into `~/.local/share/bud/overlay`; never force, stop and report on divergence. Merge it over the public content by path and ID (overlay wins), and record which layer each file came from. Optional: no overlay means public content only.
Done when: tests with real git in temp dirs cover clone, fast-forward, divergence, an overlay skill replacing a public one, an overlay profile replacing an example, and env fragments applying last.

### Task 1.7: OpenCode adapter (D12, D15, D17, D46) (PR #9) (JGA-298)
`internal/adapter/opencode`: render one `opencode.jsonc` from the registry, the profile's env fragments and the active content dir: providers, the `coding` role as the default model, agents and commands inlined, `skills`/`instructions`/`plugins` pointing at `content/current`, permission rules for the D16/D41/D43 tiers, and `"autoupdate": false` so `bud update` is the only thing that moves the pin. Secrets stay as `{env:VAR}`. Also the pinned OpenCode version and its install (download, checksum check).
Done when: golden-file tests (with `-update`) for each example profile; rendering is deterministic.

### Task 1.8: `bud sync` (JGA-297)
Ties 1.2 to 1.7: read the profile, fetch `channel.json` (GitHub release, or a mirror URL in config for phase 6), download and verify the bundle, unpack into `~/.local/share/bud/content/<version>/`, apply the overlay, switch the `current` link atomically, render each adapter's config, and write a ledger (`~/.local/state/bud/sync.json`: versions, overlay commit, every written file's hash and layer). Files Jason edited by hand are skipped and named, never overwritten (bud's manifest rule). `--dry-run` lists what would change. `bud sync --rollback` switches back to the previous version still on disk.
Done when: an end-to-end test against an `httptest` server and a throwaway key goes from nothing to a rendered config, and a second run changes nothing.

### Task 1.9: `bud doctor` drift checks (D46) (JGA-297)
New checks: the ledger's files still match their hashes (drift names the file and the fix, `bud sync`); a re-render matches what's on disk; the registry and profile validate; the content version is the channel's newest (warn, not fail); OpenCode is at the pinned version. Endpoint pings for the profile's roles run only with `bud doctor --net`, so doctor stays offline by default.
Done when: tests for each check's OK, Warn and Fail.

### Task 1.10: CI leak check and the content release (D21, D60) (JGA-343, JGA-320)
`scripts/leak-check.sh` fails when a public file names a tailnet host (`*.ts.net`), a private address, an `op://` path outside the examples' placeholder vault, or a home-directory path, with an allowlist file for deliberate examples. A workflow cuts a content release on a push to main that touches `content/`, after the checks pass: `bud content pack`, sign, publish `content-<version>` as a GitHub release, update `channel.json` for edge. Promoting to stable is a manual workflow run.
Done when: `just leak-test` covers each pattern and the allowlist; the workflow is in place (it signs only once the secret is available, like `release.yml`).

### Task 1.11: first real content (JGA-296)
Port the generic parts of today's `plugins/bud-ide/` skills into `content/skills/`, write `content/AGENTS.md` (generic; the persona goes in the overlay), and the base and coding env fragments. Nothing personal. bud-brain's tools and skills come in later phases.
Done when: the content release builds, and `bud sync` on a dev machine gives OpenCode skills it can list.

## Phase 2: home-server runtime (needs the home server)
Hermes adapter (render `~/.hermes/config.yaml` from the registry with the four D48 changes), the planning `opencode serve` (D49) as systemd units in `server/`, `bud install --device server` builds both. Test: a chat from the laptop answered by the manager and by a P1 model, and a "do alone" vs "approve" action takes the right path.

MCP servers and tools were decided 2026-11-16 (JGA-355, D72-D90; the full map by profile is `tool-map.md`). Phase 2 builds, in order:
- The Bud MCP gateway on the home server (D72) (JGA-357): tailnet only, keys through `op run`, every call to the audit log (JGA-348), the P0 tool rule (D81).
- Rendering the MCP servers and the built-in tool settings into OpenCode and Hermes configs (JGA-358) from content `env/` and the overlay's `env/personal.jsonc`: Linear and Exa through the gateway (D75, D77), GitHub on the server (D76), Context7 (D78), Serena, cocoindex-code and Playwright on `dev` (D79, D86, D87), Hermes `web` on Exa and SearXNG with `keyless_fallback: false` (D77, D85).
- SearXNG as a container (D85, JGA-301), the CalDAV calendar and reminders server (D82, JGA-359), the CardDAV contacts server (D88, JGA-360) and the Bud ops MCP (D83, JGA-361).
- Low priority (noted on JGA-314): `op://` references inside env fragments become `op run` variables like registry keys (only needed if a local-direct server ever takes a key).
Headroom's MCP (D89, JGA-295) waits until Go allowance use is measured. The Dollarwise connector (D90, JGA-362) waits until after 2.0, and JGA-363 reviews the whole tool set then.

## Phase 3: Qwen box and the router (needs the Qwen box)

(2026-11-15, D70) The router is built as Bud's model gateway in Go: one OpenAI-compatible endpoint on the home server that every runtime uses as its only provider, with virtual role models (`bud/coding`, `bud/auto`), the privacy gate first, then a pick from the role's pool (D64) by live allowance, spend and grade, plus the D69 availability cache. Adapters for more providers (Q25) come in a later milestone. D71: each API style is one adapter behind one Go interface (list models, send a chat request, read usage) plus a model-list parser; the task also updates content/skills/add-provider for the gateway step.
Runs on Jason's separate desktop rig (Arch Linux, RX 6700 XT), installed with `bud install --device model-box`; joining the tailnet, BIOS Wake-on-LAN and GPU tuning are walked through by hand. llama.cpp is a pinned, checksum-checked download (D63). llama.cpp units and the tuning deferred by D55; Wake-on-LAN (D26); the router's rule pre-tagging and Qwen classifier (D23, D24). Test: a P0 prompt never leaves the house, including with the box asleep or off.

## Phase 4: the IDE as a client
opencode.nvim pinned (D44, D45), the server switch, remove the Claude path, minuet FIM with the D32/D38 fallback, `/check`, `/metrics`, `/bench`. Test: the daily loop works with Claude Code uninstalled.

## Phase 5: memory and the KB
SQLite working memory with FTS5 and sqlite-vec, the memory MCP server and Hermes provider (D51, with the 300 ms p95 check; devices connect over the tailnet, D74; it also searches bud-kb, D84), Basic Memory's notes moved into bud-kb (D80), the curator (D9, D33, D39), `bud-kb` and the Hugo site. Test: a chat makes a reviewable memory entry that shows on the site.

## Phase 6: the mirror and banners
The home server mirrors releases and `channel.json` on the tailnet (D60); the IDE dashboard banner; edge-first rollout and revert for self-improvement changes (D47). Test: a skill pushed to main is usable on the laptop without a reinstall, and on the server only after promotion.

## Phase 7: approvals, messaging and the app
Signal via signal-cli (D30) and the reaction plugin, the approval service (D28, D34, D40, D42), mail (D31, D36, D54), then the Tauri app (D6, D50), desktop first, then iOS through TestFlight.

## Open for Jason
1. ~~Approve this plan~~ Approved 2026-11-15; mapped onto the existing Linear issues rather than filed anew.
2. ~~The name clash with `--profile`~~ Decided (D62): `--device <name>` picks the device profile, `--profile minimal|full` stays.
3. Later, before task 1.6 is used for real: create the private `jgarner9/bud-overlay` repo (this session can't create repos).

## Roadmap from 2026-11-20

State on 2026-11-20 (checked against Linear and jgarner9/bud main at e7bf073): phases 1 and 2 are built, plus work Jason did outside Claude in PRs #32 to #62: the Signal pre-approval fix (#32, merged), Hermes config preservation, README refresh, JGA-364 cleanup, ported run/locate/search/fetch, CardDAV contacts, the staged SQLite memory service with hybrid retrieval, fixed project check commands, versioned profiles, OSV advisory query, the pinned OpenCode harness install, the Herdr architecture change (supersedes Tauri, D6), and the approval lifecycle store plus stock-runtime probes (#62). JGA-348's runtime integration is not done (see docs/internals/suite.md "Final-dispatch approval contract" and docs/internals/jga-348-worklog.md).

Each task is one PR, written so a fresh session can do it cold. The thread works them in order, merging after the Copilot review (Jason authorized auto-merge for this run).

| # | Task | Issue | Needs Jason |
| --- | --- | --- | --- |
| R1 | Housekeeping: JGA-348 back to In Progress (merge auto-closed it), close draft PR #46 (its patch landed in #62), refresh HANDOFF | JGA-348 | no |
| R2 ✓ PR #63 | `bud doctor --net` checks the SearXNG endpoint in `BUD_SEARXNG_URL`: reachable, JSON format enabled; skipped when unset | JGA-301 | no |
| R3 | Run the memory packaging probe on the home server in a throwaway directory (tests only, no service changes) and record the result | JGA-368 | no (tests on the home server allowed 2026-11-20) |
| R4 ✓ code, PRs #86-#90 | JGA-313 memory for the runtimes: embedding server unit (#86), re-embed path (#87), Go memory client (#88), gateway forwards the tier (#89, Suite D108), `bud memory-mcp` (#90). Left for Jason on the home server: five 1Password tokens and the unit install, overlay `mcp.memory` gateway:true, whether to turn off Hermes's built-in memory, cross-runtime check | JGA-313, JGA-294 | no (D108 decided 2026-11-21) |
| R5 | IDE chat: adopt pinned opencode.nvim (JGA-350), then the local/home-server switch (JGA-293). Decided 2026-11-21: opencode.nvim (BASE D150, Suite D96; docs in PR #64). JGA-350 keys, review and permissions PR #91 merged (BASE D151); left for Jason: old chat code removal, default backend, a live run. R5b ✓ PR #92 (planning OpenCode as system user bud-planning, Suite D109) and PR #93 (JGA-293 `<leader>aP`/`:BudPlan`, Suite D110, BASE D152). Left for Jason: the home server install steps in side-notes-2026-11-21.md. D99 server gap closed by PR #94 (Suite D111, bud-unattended-deny plugin) | JGA-350, JGA-293 | no |
| R6 | Approvals at Bud-owned leaves (Suite D97) with native prompts kept where Jason is present (D99): R6a verify and document that unattended OpenCode runs and Hermes cron jobs refuse approval-tier actions; R6b wire the approval store into the gateway, ops and Signal plugin | JGA-348, JGA-349, JGA-374 | no. R6b built 2026-11-21 (design D103, PRs #71-#75: client, plugin reviewer, gateway, ops leaf, rendering and cutover); a home server needs bud update then bud sync before restarting the gateway, and overlay-only gateway servers need approval or read_only. R6a PR #69: opencode run and Hermes cron refuse; an OpenCode server with no client waits forever on an ask (gap, matters once the planning server unit exists) |
| R7 | iCloud Mail tool with approved sends and isolated ingestion | JGA-315, JGA-317 | no: decided 2026-11-21, Suite D106 (`bud mail-mcp` behind the gateway, stdlib IMAP subset plus net/smtp). Next: spec, then build |
| R8 | Model router on the home server (D70/D71): one OpenAI-compatible endpoint that pools every provider's models, filtered by privacy tier and allowance; Jason asked for it 2026-11-21 ("like Lemonade") | JGA-346, JGA-345 | no: decided 2026-11-21, Suite D98 (Bud router in front, Lemonade behind it on the Qwen box). R8a router PR #66 and R8b tiers PR #68 merged (header trust D100); R8c spend cap PR #70 merged (role requests move to the next model when allowance is spent; Jason confirmed); R8d health fallback and P0 approval override PR #76 (Suite D104); Hermes private profile through the router PR #77 |
| R8e ✓ PRs #95, #96 | JGA-346: D23 privacy rules in the registry, raise only (Suite D112), applied by the router (#95) and by the MCP gateway to tool-call arguments (#96). Left: the D67 classifier (needs Gemma on the home server) | JGA-346 | no |
| R9 | A custom Bud TUI made to Jason's taste, for use from his phone and other devices over Herdr. One of the last 2.0 items; don't start early | JGA-375 | yes: design session with Jason before any code |

Hardware-bound and later: JGA-294 embeddings, JGA-345/347/352 Qwen box and FIM, JGA-292/354 Herdr, JGA-322 backups, JGA-330 release.
