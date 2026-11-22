---
name: kb-curator-build
description: JGA-311/312 KB curator and Hugo site build state (2026-11-22): PR #115 (supersedes #107), decisions D122-D124, the home server rollout in revisit item 27
metadata:
  type: project
  modified: 2026-11-22T17:57:27.054Z
---

2026-11-22, thread "Finish KB curator and Hugo view" took over from "Build KB curator and Hugo view". PR #115 (branch claude/project-thread-d7xm7q), squash-merged 2026-11-22 18:23Z as f338f7a after four Copilot rounds, superseded the closed draft #107 and holds both JGA-312 (internal/kb, bud kb-site, server/hugo) and JGA-311 (internal/curator, bud curator run|promote|resolve|restore, server/curator timer at 03:30, memory.Client.Export).

Found by the first real Hugo render (0.152.2, downloadable from GitHub releases into the sandbox): Hugo reserves the front-matter key `kind`, so pages use `page_kind`; `hugo --quiet` hides errors and Hugo prints `Error:` on stdout. CI has no hugo, so TestRealHugo skips there.

Decisions on recommendation: D122 (KB layout and site), D123 (local models only, never the router; P1/P2 only while bud-kb's remote is GitHub; state outside the KB), D124 (6/12-month staleness; review queue data/review.json, no Signal prompts).

Linear JGA-311 and JGA-312 are Done; only the home server rollout remains, on Jason.

**How to apply:** the home server rollout steps are item 27 in /mnt/project-files/revisit-20261122T081850-a7a8.md (Hugo, bud-kb clone with deploy key, curator token with export-only P1/P2 policy in the Bud Memory clients field). Jason does those by hand.

Related: [[embeddings-and-memory-mcp]], [[d113-memory-review]]
