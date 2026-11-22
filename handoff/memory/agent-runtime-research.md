---
name: agent-runtime-research
description: 2026-11-15 research on Hermes vs OpenClaw for the home-server manager, Go Plus API outside OpenCode, Headroom, embedding pick; open Q18-Q21
metadata:
  type: project
  modified: 2026-11-15T02:34:35.161Z
---

Written 2026-11-15 to /mnt/project-files/architecture/agent-runtime-comparison.md, linked from section 5 and Q18–Q21 of the architecture doc. Nothing decided.

- Jason leans to Hermes Agent (MIT, Python) as the 24/7 manager, OpenCode stays the coding harness. Proposed: Hermes pinned by the Bud installer with approvals.mode manual, timeout 3600, skills/memory write_approval true, background_review off, synced skills locked, terminal in Docker. Bud keeps memory, KB, approval service, model registry and skills outside the runtime so it stays swappable. If picked, narrows D17 to coding.
- Hermes facts checked in its repo: Signal adapter on signal-cli daemon; typed yes/no answers approvals, but no incoming-reaction approval found (D30 needs a small approval-transport plugin); pre_tool_call hook can require approval for any tool; built-in opencode-go provider; OpenAI-compatible API with /v1/runs/{id}/approval.
- OpenClaw rejected-ish: 722 advisories (14 critical) per clawdocs.org; permissive defaults; but strong built-in SQLite FTS5+sqlite-vec memory.
- OpenCode Go works outside OpenCode: https://opencode.ai/inference/go/openai/v1 (also /anthropic, /google). Go docs say it's designed for coding agents and traffic is monitored; Hermes is a validated client. Keep manager chores on Qwen.
- Headroom = context-compression proxy/library (headroom-ai, Apache-2.0), not an embedding model. Proposed embedder: Qwen3-Embedding-0.6B on llama.cpp, home-server CPU preferred (Qwen box sleeps, D26); EmbeddingGemma 2 is the lighter alternative.
- Jason wants the architecture adaptable to a changing AI landscape and a Bud-owned self-improvement loop; proposal is a model registry rendered by bud sync (Q19) and a propose-only loop where changes arrive as PRs (Q21).

Related: [[bud-ide-mapping]], [[bud-role-assistant-not-driver]], [[bud-suite-decisions]]
