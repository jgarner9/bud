---
name: architecture-diagrams
description: Public architecture diagrams live in jgarner9/bud docs/architecture.md as Mermaid (JGA-388, PR #111, merged 2026-11-22)
metadata:
  type: project
  modified: 2026-11-22T16:47:43.070Z
---

Jason chose Mermaid (2026-11-22) for Bud's architecture diagrams: six levels (suite, home server, routing and privacy, approvals, memory/KB, releases and sync) in docs/architecture.md, drawn natively by GitHub and in the browser on the docs site (site/_layouts/doc.html loads Mermaid 11.17.2 only on pages with a diagram, themed from site.css variables). AGENTS.md says to update the diagrams in the same change as the code they draw. Planned parts are dashed, decision points have thick borders; machines are named by role only.

**How to apply:** when a change alters routing, approvals, memory, sync or which services run where, update docs/architecture.md too. The private, richer diagrams are still in bud-suite-architecture.html ([[bud-suite-architecture-doc]]).
