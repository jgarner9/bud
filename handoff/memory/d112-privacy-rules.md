---
name: d112-privacy-rules
description: Suite D112 (2026-11-21, PR #95 merged): registry privacy_rules that only raise a request's tier, applied by the router (JGA-346 slice 1)
metadata:
  type: project
  modified: 2026-11-21T20:00:05.506Z
---

Suite D112: `privacy_rules` in content/env/registry.jsonc (name → RE2 `match`, tier P0/P1), merged with the overlay by name. `bud router` applies them to every message text (chat content, prompt, input) before picking a model; raise only. Shipped: `marker` (`#private` at the start of a message) and `private-key`, both P0. Response header `X-Bud-Privacy-Raised: <rule>`; the log never holds text. An `op://` rule was left out on purpose (planning chats mention op refs).

**Why:** D23 says rules come first and can only raise. Decided on Claude's recommendation; Jason can reverse it.
**How to apply:** gateway half built 2026-11-21 in PR #96: `bud mcp-gateway` applies the same rules to every string in each JSON-RPC message's params, uses the raised tier for the P0 tool rule/audit/approval scope/upstream header, and refuses to start without a registry (`--registry` or synced). Still open on JGA-346: the D67 classifier (needs Gemma on the home server). Pending items for Jason are in /mnt/project-files/notes/revisit.md. Related: [[bud-suite-roadmap]]
