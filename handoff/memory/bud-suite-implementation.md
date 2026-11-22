---
name: bud-suite-implementation
description: Where the Bud Suite build stands - the build plan, open PRs on jgarner9/bud, and the handoff file that tracks them
metadata:
  type: project
  modified: 2026-11-15T05:15:46.257Z
---

Implementation started 2026-11-15 in the thread "Build Bud Suite phase 1: installer and sync".

- The plan is /mnt/project-files/architecture/build-plan.md, approved 2026-11-15. It maps onto Jason's existing Linear issues in project Bud Suite (milestone Bud 2.0): 1.1/1.10a JGA-343, 1.2/1.3/1.7 JGA-298, 1.4/1.5/1.10b JGA-320, 1.6/1.8/1.9 JGA-297, 1.11 JGA-296. No duplicate issues filed.
- The live status, open PRs, merge order, next tasks and Needs-Jason list are in /mnt/project-files/architecture/HANDOFF.md. Read that first; it's kept current.
- 2026-11-15 08:00Z: every phase 1 PR so far (#3-#9, #11) is merged; none open. Next: 1.8 `bud sync`. Claude never merges.
- 2026-11-15 08:50Z: PR #12 (1.8 `bud sync`) merged 08:32Z (internal/syncer, docs/internals/sync.md). Next: 1.9 doctor drift checks, then 1.11.
- jgarner9/bud is saved to the project's repos. OpenCode's V2 docs can be read by sparse-cloning github.com/sst/opencode (services/site/src/docs/content), since `gh api` on that repo is blocked.
- D62 (2026-11-15): Jason chose `bud install --device <name>` for the device profile; `--profile minimal|full` stays the IDE install size. Task 1.4 builds it, stacked on PR #5.
- The Qwen box is Jason's separate desktop rig, not the home server (he confirmed 2026-11-15). Phase 3 plan: `bud install --device model-box` does llama.cpp, models and units; Tailscale join, BIOS Wake-on-LAN and GPU tuning are walked through by hand. It runs Arch Linux (confirmed 2026-11-15), so pacman and systemd apply. D63: llama.cpp comes as a pinned, checksum-checked download, not from the AUR.
- 2026-11-15 12:26Z: PR #13 (1.9 doctor drift checks) merged. Next: 1.10b content release workflow, then 1.11.
- 2026-11-15 13:24Z: PR #14 (1.10b) and PR #15 (1.11) merged: phase 1 is built. Left over: pinned OpenCode install (1.7) and doctor OpenCode-version check. Next: phase 2 (needs the home server).
- 2026-11-15 17:47Z: BUD_LEAK_PATTERNS secret is set. First CI run with it (CI run 43718853938, attempt 2) failed with 423 hits: 272 are the deliberately public module path/site domain and GitHub username, 151 look like ordinary prose (likely an over-broad pattern). Jason narrowed the secret; the 17:51Z re-run (attempt 3) passed with the secret applied ("leak-check: ok", not shapes-only).

Related: [[bud-suite-architecture-doc]], [[bud-ide-mapping]]

2026-11-15 21:36Z: Linear milestone "Bud for everyone" (project Bud Suite, after Bud 2.0) holds JGA-356, the exploration epic for a self-hostable Bud anyone can set up (bootstrap, ground-up guide, Windows/WSL2, single-machine). Exploration only; Windows needs BASE.md D22 revisited.

2026-11-16 03:50Z (merged 03:37Z... see HANDOFF): JGA-357 MCP gateway built as bud PR #19, merged by Jason (branch claude/project-thread-yucxx3): `bud mcp-gateway`, internal/mcpgateway, server/mcp-gateway/ (user unit under op run, example config), spec docs/internals/mcp-gateway.md. Session tier via X-Bud-Privacy header, missing = P0 (fail closed); JGA-358 must render that header into device MCP configs. Audit log is JSONL in the state dir until JGA-348. Open questions put to Jason in the PR: Exa P1+P2 vs tool-map "P2 only". Registry comment notes qwen-35b = Qwen3.6-35B-A3B (proposal doc already had it).
2026-11-16 03:44Z: Jason approved the gateway defaults ("that work"): missing X-Bud-Privacy = P0, Exa open to P1 and P2, IP-only access (Host must be a loopback/tailnet IP; no MagicDNS names). JGA-358 should render IP URLs plus the header.
2026-11-16 04:50Z: JGA-358 is bud PR #20 (branch claude/project-thread-y28iv1), merged by Jason 04:47Z with Copilot fixes in aec692a. Env fragments gain a `bud` block (internal/adapter/env: gateway IP URL, device tier for X-Bud-Privacy, mcp servers once for every runtime, hermes raw settings); adapter/opencode adds mcp.servers + a `private` P0 agent (D81); new adapter/hermes renders ~/.hermes/config.yaml on server devices. content/env/server.jsonc is new, content format is now 2. Open choices in the PR: static per-device tier P1, Playwright navigate always asks. The overlay needs env/personal.jsonc with bud.gateway and bud.mcp.memory.url.

2026-11-16 06:20Z: JGA-361 Bud ops MCP built as bud PR #21 (merged by Jason 06:25Z; Copilot fixes b16463b: doctor never --net since P0 may call it, malformed id-less msgs get -32600) (branch claude/project-thread-cld4r6): `bud mcp-ops` (internal/mcpops), loopback 127.0.0.1:8792 (memory is 8791 in docs), server/mcp-ops/ unit, spec docs/internals/mcp-ops.md. Tools doctor + sync_status real; spend/approvals/reviews listed but return "not built yet". Listed P0-P2 (open question to Jason in PR). Parallel CalDAV thread (JGA-359) may touch the same content/env, gateway example, docs lines.
