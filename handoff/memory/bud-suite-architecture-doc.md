---
name: bud-suite-architecture-doc
description: Where the Bud Suite architecture doc lives and the rules for editing its decisions log
metadata:
  type: project
  modified: 2026-11-15T00:48:32.891Z
---

The Bud Suite architecture doc is one self-contained HTML file at /mnt/project-files/architecture/bud-suite-architecture.html (started 2026-11-15), with inline SVG diagrams and an append-only decisions log (D1, D2, ...) at the very bottom.

**Why:** Jason asked for a living, human-readable architecture doc with fancy diagrams, and a decisions list that never gets overwritten.

**How to apply:** Edit that file rather than starting a new doc. Tag content Decided / Proposed / Open. Never renumber, delete or rewrite a decision; mark it "Superseded by Dn" with old text struck through and append the new one. Answered open questions (Q1–Q7) become new decisions. As of 2026-11-15, D1–D8 are logged (D6: Tauri 2 thin client; D7: 1Password holds all secrets, model never reads values; D8: Linear for dev TODOs, KB for reference docs; D9: two-layer memory with nightly Qwen curator; D10: action tiers do alone / approve by phone / never; D11: OpenCode Go Plus is the cloud plan; D12: bootstrap only, Go wrapper later; D13: working memory in SQLite with FTS + vectors; D14: bud-brain rebuilt as the Suite repo; D15: OpenCode runs locally beside the IDE; D16: running commands under the action tiers; D17: fully off Claude, skills/plugins/environment sync; D18: bud-brain is one monorepo whose installer pulls per device profile). D19: public bud folds into the monorepo, archived after; D20: KB in its own repo, monorepo is the application layer; the monorepo tree and sync are proposals in section 8; the routing tiers are proposals, not decisions. Jason has access to a paid Apple developer account (shared with his dad), so TestFlight is available.

Related: [[bud-suite-decisions]]
