---
name: pr-review-fix-comment
description: After pushing fixes for Copilot (or other) review findings, leave one PR comment summarizing the round
metadata:
  type: feedback
  modified: 2026-11-15T19:07:05.389Z
---

After pushing fixes for a round of Copilot review findings, leave one top-level comment on the PR saying the round is done: each finding, what changed (or why not), and the commit(s) that carry it. Thread replies alone aren't enough.

**Why:** Jason asked (2026-11-15, PR #18 on jgarner9/bud): with several commits per round it's hard to tell when the fixes are finished.

**How to apply:** Push all fixes for the round first, then post the summary comment last, with the GitHub footer. Still reply on and resolve each thread as usual.
