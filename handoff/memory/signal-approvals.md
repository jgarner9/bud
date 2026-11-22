---
name: signal-approvals
description: Signal for Hermes and the bud-signal reaction-approval plugin (bud PR #28, 2026-11-17): design, defaults, what Jason must do at the home server
metadata:
  type: project
  modified: 2026-11-17T18:37:58.755Z
---

Built 2026-11-17 in thread "Build Signal chat platform and reactions": bud PR #28 merged 2026-11-17 18:47Z (Copilot fixes 3ec4e53: edited/linked plugin file disables plugin, held dirs kept out of stale deletes incl. skills, reactions must target bot number). Declined: rejecting untracked files (__pycache__) and symlinked ~/.hermes.

- bud-signal: Hermes plugin embedded in the bud binary (server/hermes/plugins/bud-signal/), installed by bud sync into ~/.hermes/plugins/bud-signal on server devices. It re-registers Hermes's "signal" platform with a SignalAdapter subclass: approval prompts get a 4-digit code; thumbs-up or "yes <code>" approves once, thumbs-down or "no <code>" denies; only SIGNAL_ALLOWED_USERS senders. Pinned requires_hermes ==0.23.2 (uses private adapter parts); re-check on a Hermes bump.
- Why not Hermes's approval-transport API: in v0.23.2 the plugin pre_tool_call gate (_ACTION_GATE, which bud hermes-gate uses) doesn't go through transports.
- Defaults picked (told Jason in PR): signal-cli daemon on 127.0.0.1:8793; numbers (SIGNAL_ACCOUNT, SIGNAL_ALLOWED_USERS, SIGNAL_HOME_CHANNEL) in ~/.hermes/.env; bud owns all of Hermes `plugins` and `platforms.signal`; unauthorized_dm_behavior ignore.
- Not built: approval service (D28, JGA-348: Tauri push first, Signal after 5 min); doctor check for signal-cli/plugin.
- At the home server (Jason): install signal-cli native 0.14.10 to ~/.local/bin, register+verify bot number (captcha), setPin (PIN to Bud vault), unit server/signal-cli/bud-signal-cli.service, .env numbers, update bud, bud sync, hermes gateway restart, end-to-end test.
- Plugin was never run in real Hermes (sandbox refused installing Hermes deps); tests use stand-ins.

Related: [[hermes-config]], [[bud-suite-implementation]]

2026-11-18: pre-approval never matched on real Hermes, because gate approvals show prompt.command as the label "<terminal> (plugin approval rule)". Fix in bud PR #32 (merged 2026-11-18 06:55Z; the home server needs bud update, bud sync, hermes gateway restart): gate reason starts `bud: run "<whole cmd as JSON>"? why`, plugin's approvals.command_of reads it for that label only. Linear JGA-366: bud sync should tolerate Hermes-owned config.yaml keys (onboarding).
