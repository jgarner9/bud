---
name: backlog-sweep-2026-11-21
description: Thread "Work Bud backlog items that don't need Jason" (2026-11-21 evening): what it built (PRs #98-#100) and why the rest of the Bud 2.0 backlog waits on Jason or the home server
metadata:
  type: project
  modified: 2026-11-21T21:39:59.662Z
---

2026-11-21 ~21:30Z sweep of Linear (team Personal, project Bud Suite, milestone Bud 2.0) for repo-only work.

Built: PR #98 ops `spend` tool over the router ledger (router now writes its cap into router-spend.json; `router.ReadSpend`). PR #99 doctor checks `signal-cli` (loopback /api/v1/check, quick too, no redirects) and `Hermes version` (first line of `hermes --version` vs bud-signal plugin.yaml pin, process group killed so the update check never runs; not in --quick). PR #100 content/skills/mail (JGA-319 slice 1).

Not built, logged in /mnt/project-files/notes/revisit.md items 16-18: router over-cap approval (how much one yes covers), ops `reviews` (needs a memory-service count action), the home server follow-ups.

Everything else open in Bud 2.0 needs the home server/the Qwen box hardware (JGA-345/347/352/322/291/295), Jason's design (JGA-311/312/318/321/353/375, JGA-317 summarizer), or Herdr (JGA-292/354). JGA-319 left: plan-to-Linear workflow, Signal texts, digest skill.

Branch claude/project-thread-a0yq5z can't be force-pushed: after a squash merge, reset to origin/main then merge the old remote head before pushing.

Related: [[bud-suite-roadmap]], [[pr-review-fix-comment]]
