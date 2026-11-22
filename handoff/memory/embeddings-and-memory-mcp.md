---
name: embeddings-and-memory-mcp
description: JGA-294 embeddings build (PRs #86/#87, 2026-11-21) and the R4 bud memory-mcp design with its open tier question
metadata:
  type: project
  modified: 2026-11-21T10:41:03.345Z
---

2026-11-21 thread "Build embeddings and continue roadmap":
- PR #86 merged: server/embed/ (llama-server b12200 commit c19043f, built from source CPU static by scripts/build-embed-server.sh; Qwen3-Embedding-0.6B-Q8_0 GGUF pinned by HF revision + sha256; port 8082, alias qwen3-embedding-0.6b, BUD_EMBED_HOST in ~/.config/bud-server/embed.env). Measured on cloud 4 vCPU: 2.1 GB RSS, 29 ms query, ~225 ms/record batch. bud doctor --net "embeddings" check warns on dims mismatch.
- PR #87 (merged): memory service POST /vector_pending (new action) lists approved records lacking a current vector for a model; the re-embed path.
- The home server install is Jason's step (side notes file has steps). GitHub release asset digests aren't reachable from cloud sessions; git reads of public repos are, hence the source-build pin.
- Branch rule: claude/project-thread-xwl0z5 can't be force-pushed; after a squash merge, merge the old head with `-s ours` and push.
- R4 design: /mnt/project-files/architecture/r4-memory-mcp-design.md. Tools memory_search, memory_stage; embed loop in memory-mcp; one memory token per session tier. Q1 decided 2026-11-21 (Suite D108): gateway forwards its checked X-Bud-Privacy to loopback/tailnet upstreams; one memory-mcp instance.

- PR #88: internal/memory Go client (R4 slice 1). Staging needs a token covering the record's tier, so the design uses a separate all-tier stager token. JGA-294 left In Progress for the home server install.

- PR #89 merged (D108 gateway tier forwarding; key.header may not be X-Bud-Privacy). PR #90 (merged 2026-11-21): bud memory-mcp; retrieve works text-only without model/values, vector_pending takes an after cursor (internal/memorymcp, port 8793 loopback at the time, moved to 8798 in PR #101 because signal-cli holds 8793, five BUD_MEMORY_TOKEN_* tokens P0/P1/P2/STAGE/EMBED, backfill loop). Open for Jason: the home server install + 1Password tokens, overlay mcp.memory gateway:true, turning off Hermes built-in memory.

Related: [[bud-suite-roadmap]]
