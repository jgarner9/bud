---
name: d113-memory-review
description: Suite D113 (2026-11-22, PR #104 merged): bud memory-review asks on Signal about each staged memory record; ops reviews reads its counts
metadata:
  type: project
  modified: 2026-11-22T08:14:48.647Z
---

Suite D113, decided on Claude's recommendation (Jason can reverse it): `bud memory-review` (internal/memoryreview, unit server/memory-review/) holds the memory reviewer token. It lists staged records every minute (memory service action `staged`) and asks Jason about each through the bud-signal reviewer socket, showing the whole record, with at most 4 prompts at once. A yes approves. Only his own no (reviewer reply `"answered": true` → `approval.ErrDeclined`) deletes, via the `discard` action. With no answer the record stays staged and is asked again after 24h. A record over 6,000 characters waits for a hand approval. The ops `reviews` tool reads `memory-review.json` in the state dir (counts only). This replaced revisit item 17's `pending_count` token plan.

**Why:** nothing approved staged records, so memory_search stayed empty on the home server.
**How to apply:** the home server setup steps are in /mnt/project-files/notes/backlog-run-2026-11-22.md (add `staged` and `discard` to the reviewer's actions in the "Bud Memory" clients policy, write memory-review.env, install the unit). Open question (2026-11-22): Jason said Signal approvals ask too much, and the thread "Loosen Signal approvals and privacy routing" is auditing them. Memory review adds one prompt per staged record, so it belongs in that audit (for example auto-approving low-risk tiers or kinds, or a daily batch). PR #105: the gateway, router and memory-review log their start line without --verbose.

Related: [[backlog-sweep-2026-11-21]], [[pr-review-fix-comment]]
