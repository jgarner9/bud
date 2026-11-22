---
name: hermes-profiles-d95
description: D95 (2026-11-17): Hermes split into a private profile (Signal, calendar, P0 models only) and a cloud profile; PR #27 status and the home server steps
metadata:
  type: project
  modified: 2026-11-17T18:36:54.643Z
---

Jason chose (2026-11-17 18:30Z, decision card) two Hermes profiles over private-only Hermes and a taint gate. Logged as D95 in the arch doc. Supersedes D93's "calendar stays off" outcome.

- Private = default profile ~/.hermes: owns Signal, calendar, reminders; config lists only P0 providers/models (cloud provider dropped whole, key too); sends X-Bud-Privacy P0 to P0-only gateway servers. bud sync refuses a ~/.hermes/.env setting a left-out provider's key var (Hermes built-ins read OPENCODE_GO_API_KEY etc. from env).
- Cloud = ~/.hermes/profiles/cloud (`hermes -p cloud`): all chat models, no calendar, gateway.standalone true (Hermes multiplexes every profile into the default gateway otherwise; multiplex_profiles:false is retired in v0.23.2). Same D48 settings, gate, skills locked with `hermes -p cloud curator lock`.
- Gate: tiers file `private` patterns (mcp__calendar__*) refused unless payload profile == "default".
- Why not taint gate: in v0.23.2 pre_llm_call (the only hook that sees the model) fails open; fail_closed applies only to pre_tool_call; session ids rotate on compaction/branch/subagent.
- Open: calendar text can reach cloud tools (Linear/Exa) in a private chat; Bud memory (phase 5) must keep private writes from cloud profile; cron runs only in private profile; reminder texts need Signal.
- PR #27 merged 2026-11-17 18:57Z (Copilot fixes 3879dc6: built-in provider key list BuiltinKeys, privateOnly by server tiers, BOM; merge with Signal #28 in e3e4dfa, Signal private-only). The home server steps after merge: update bud; move OPENCODE_GO_API_KEY to ~/.hermes/profiles/cloud/.env, OP_SERVICE_ACCOUNT_TOKEN in both .env files; bud sync; hermes doctor + hermes -p cloud doctor; hermes gateway restart; server/hermes/README.md step 8 calendar check (renumbered after the Signal merge).

Related: [[hermes-config]] [[bud-suite-implementation]]
