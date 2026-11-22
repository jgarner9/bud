---
name: autonomy-privacy-design
description: 2026-11-22 proposal to cut Signal approvals and loosen P0-only routing; doc, proposals P1-P8, JGA-376 audit, Jason approved 2026-11-22 as Suite D114-D121
metadata:
  type: project
---

Jason (2026-11-22): Signal asks too much; wants Bud more autonomous, and only sensitive (maybe simple) requests on local Qwen (24 tok/s too slow), more on Go "within reason". Security-sensitive: the "go with the recommendation" rule does NOT cover this; he decides.

Doc: /mnt/project-files/architecture/autonomy-and-privacy-design.md (proposals P1-P8, become Suite D114+ when picked; D113 is memory-review, PR #104). Audit issue JGA-376 (Bud 2.0, High, related JGA-348/374). 2026-11-22 08:18Z Jason chose "Approvals + evidence-based privacy": P1-P8 = Suite D114-D121 in order. Build issues JGA-377 (terminal), 378 (one prompt), 379 (argument rules), 380 (rule packs), 381 (provenance), 382 (P1 default, blocked by 380/381). Classifier (D120) not filed. The audit also covers memory-review prompts (D113).

Key findings: hermes-gate gates Hermes's sandboxed terminal with the coding shell allowlist (base.jsonc), so ls/cat/curl ask while execute_code is allow; Linear/calendar/mail writes can prompt twice (Hermes untrusted + gateway). Recommended: sandbox terminal allow + ask list, trust gateway-enforced servers in Hermes, argument-keyed `when` rules (stdlib), private chats default P1 with router raising to P0 via rule packs (gitleaks MIT regex port, Luhn/IBAN PII), provenance taint (P0-only tool calls in history), classifier off at first, simple requests not local.
Research: no OPA/Cedar/trufflehog(AGPL)/Presidio imports (D1); borrow Codex is_safe_command design. Related: [[d112-privacy-rules]] [[signal-approvals]]

Build status 2026-11-22 18:40Z: all merged: #106 (377), #108 (378), #110 (380), #112 (381), #113 (379), #114 (382, 39bdd7a). Jason chose to hold P1 on the home server: overlay puts qwen-35b first in roles.manager.pool until memory records get tiers. 382 added sticky P0 conversations in the router (by x-opencode-session hash, 30 days), because Hermes compression and title calls drop tool_calls. Content format is now 8 (optional `quick` role). Known gaps for Jason to decide: memory search results reach Go in P1 chats (needs per-record tiers), and delegated subagents get new conversation ids. The home server steps: revisit-20261122T081850-a7a8.md items 22-25. Handoff: /mnt/project-files/handoff/approvals-routing.md.
