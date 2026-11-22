---
name: hermes-config
description: How bud renders Hermes's config (JGA-290, PR #23): P0-only model chain, D48 settings, bud hermes-gate tier hook, skill locks
metadata:
  type: project
  modified: 2026-11-17T02:35:31.753Z
---

JGA-290 build (bud PR #23, merged 2026-11-17 03:23Z; Copilot fixes in a0306e6). Next: Jason runs the at-the-machine checklist in the PR and server/hermes/README.md:
- Hermes is v0.23.2 on the home server. Its config schema was checked against the source at tag v0.23.2 (hermes_cli/config_defaults.py).
- Model: the manager role's first P0 model is the default; fallback_providers holds only P0 models until the router (phase 3) knows each session's tier. Cloud models can be picked by hand with /model custom:<provider>:<model>.
- D48 settings are owned by bud (fragments are refused): approvals manual/3600, command_allowlist [], write_approval on for skills and memory, terminal docker, updates.check false, skills.external_dirs = [] and synced skills copied into ~/.hermes/skills under the ledger, then `hermes curator lock` (lock only works on skills in Hermes's own dir; Jason chose copy-and-lock 2026-11-17, PR #24, which also adds _config_version 53).
- Tier gate: `bud hermes-gate <state>/hermes-tiers.json` as a fail-closed pre_tool_call shell hook. Shell rules are shared with OpenCode; Hermes's tools are tiered by `bud.tiers` in content/env/server.jsonc. Content format 3.
- Deferred: the Signal reaction plugin (phase 7), the Bud memory provider (phase 5), the planning OpenCode unit, doctor readiness checks, and pinning the sandbox image digest.
- Hermes's gateway uses its own `hermes gateway install` unit, not a bud unit. The at-the-machine steps are in server/hermes/README.md.

Related: [[bud-suite-implementation]] [[hermes-profiles-d95]] (calendar: superseded by D95 two profiles, PR #27)

Status 2026-11-17 04:15Z: PR #24 merged; the home server runs bud built from main 8c20695. bud sync copied and locked add-provider, bud-dev, file-issue in ~/.hermes/skills; hermes doctor reports config v53 OK, only optional API keys missing. Backups on the home server: ~/.hermes/config.yaml.pre-bud and .pre-bud-2, ~/.local/bin/bud.bak (8d53e63) and bud.1.1.1-main.07bcea9.bak. Gateway installed, linger on, running since 2026-11-17 04:08Z (Jason approved); the bud hermes-gate hook is registered fail-closed. Open follow-ups: no messaging platform yet (Signal plugin is phase 7); github MCP: Jason chose GitHub's hosted server api.githubcopilot.com/mcp/ with the PAT as bearer header from GITHUB_PERSONAL_ACCESS_TOKEN (D94, bud PR #26 merged 2026-11-17 04:45Z); at the home server he needs the PAT in the Bud vault, an op:// ref under bud.hermes.secrets.onepassword.env in the overlay, then bud sync + hermes gateway restart. Known gap: the planning OpenCode's opencode.env carries only registry provider keys, so it won't get GitHub/Exa keys until its install; calendar MCP stays refused (Hermes sends P1): PR #25 merged 2026-11-17 with Copilot's 587a907 keeping the device tier, because a cloud model picked by /model could read P0 calendar data and Hermes's pre_tool_call hook payload has no model field (v0.23.2); D93 marked superseded. Calendar and reminders for Hermes wait for the phase 3 router (or dropping cloud providers from Hermes, which Jason didn't pick); qwen provider has no api key (fine unless the Qwen box's llama server requires one).


2026-11-17 05:50Z: stable promoted to content 2026.11.17.2 (run 44026047612); the home server synced to it (old config kept as ~/.hermes/config.yaml.hand-edited, only diff was gateway.service_install_choice), gateway restarted. github still unconnected: Hermes's op read fails with Authentication failed to auth.LookupAuth, so OP_SERVICE_ACCOUNT_TOKEN in ~/.hermes/.env is likely bad; Jason to test op read by hand. the home server's Claude Code auto-mode refuses op commands for Claude, and once refused bud sync until Jason asked directly.
2026-11-17 18:00Z: root cause: the op service-account token in the home server's ~/.hermes/.env (and likely ~/.config/bud-server/op-service-account.env) belongs to a deleted service account (403 Service Account Deleted). Jason needs a live SA with the Bud vault read access, token in both files.
2026-11-17 18:15Z: DONE. Jason replaced the token; Hermes restarted, 1Password applied 1 secret, github (HTTP) registered 53 tools; 124 tools from context7, ops, linear, github. Calendar still parked (D81/D93 fix needs bud update on the home server).
