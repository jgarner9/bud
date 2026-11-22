---
name: jga-321-improve
description: bud improve (JGA-321, PR #109, Suite D125-D129): self-improvement loop, weekly Signal digest, bud-proposal PRs only Jason merges
metadata:
  type: project
---

Built 2026-11-22: `bud improve run` (internal/improve, timer in server/improve/) measures router spend and doctor counts daily, scans provider model lists against the registry weekly, queues proposals in ~/.local/state/bud/improve.json, and asks at most one Signal digest a week (action `improve:digest`). A yes opens draft PRs labelled `bud-proposal`; his no drops them for good. 8 days after a merge it compares the week before and after, and proposes a revert on a regression. Suite D125-D129 were taken on recommendation (design: /mnt/project-files/architecture/jga-321-self-improvement-design.md).

**Why:** D47 requires a propose-only loop; Jason wants few Signal prompts.
**How to apply:** Claude threads never merge a `bud-proposal` PR; merging one is Jason's approval of that change. Open item: the `improve:digest` entry in internal/approval's titles map (the auto-mode classifier blocked adding it from this thread; revisit item 19). the home server rollout steps are revisit items 21.

Related: [[d113-memory-review]], [[autonomy-privacy-design]]
