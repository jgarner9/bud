---
name: model-pool-proposal
description: Role pools + model capability fields (D64-D69, decided 2026-11-15, PR #17), verified Go Plus facts, Jev verdict, Q25 other providers deferred
metadata:
  type: project
  modified: 2026-11-15T18:06:08.341Z
---

Design in the Claude Doc "Model pool and capability routing (proposal)", https://claude.ai/artifact/McKJzwkeXTx1GF5wSyEjZb, written 2026-11-15. Jason approved 1-5 ("I like this") 2026-11-15: logged D64 (pools + caps), D65 (only 0-day retention sees P1; trains = P2 only), D66 (DeepSeek out), D67 (Gemma 4 E4B classifier on home server, Jev-style scoring, escalate to qwen-35b), D68 (kimi-k3.1-pro replaces kimi-k3), D69 (pool recheck: bud registry refresh, doctor --net drift, router availability cache later). Built in PR #17. Decision 6 confirmed by Jason 2026-11-15: qwen-35b is Qwen3.6-35B-A3B. PR #18 (content format gate, content/FORMAT) merged 2026-11-15; thread done. Q25: other cloud providers (OpenAI/Anthropic API keys) deferred to a later milestone; subscriptions != API access.

Verified facts (2026-11-15):
- Go endpoint https://opencode.ai/inference/go/openai/v1 is right; `GET .../models` needs no key and returns per-model modalities, tools, reasoning, structured output, context/output limits, prices. Names kimi-k3, glm-5.3, mimo-v2.6-flash exist. This closed the "verify Go endpoint and names" to-do.
- Every Go text model works through chat completions (Go translates), so one `api: openai` provider reaches all.
- Go Plus allowances are per-model dollar amounts (5 h = 20%, week = 50%); responses report `opencode.allowance`. Allowance and retention are only on the docs page (opencode.ai/docs/go), not the API.
- Retention: most models 0-day; Grok, GPT Lunas, Claude Haiku, Beam Free 30-day; Muse Spark and Iceman train on data; DeepSeek and LongCat 2.5 Free need the Global region.
- kimi-k3.1-pro beats kimi-k3 on price and allowance.

Proposal: role `pool` + `needs` (+ optional `pin`), model fields inputs/caps/output/grade/allowance/price/retention/trains; router filters then ranks by grade vs difficulty, allowance left, pool order; `bud registry refresh go` pulls facts.

"Jev" = TypeSafe AI's hosted classifier (closed weights, API-only), so it can't be the classifier under D23. Recommended instead: Gemma 4 E4B on the home server with Jev-style option scoring, escalating to qwen-35b. qwen-35b = Qwen3.6-35B-A3B (confirmed 2026-11-15).

Related: [[agent-runtime-research]], [[bud-suite-architecture-doc]]

Q26 answered as D70 (2026-11-15): Jason chose the Go gateway. Was: make phase 3's router an OpenAI-compatible gateway on the home server that every runtime uses as its only provider (virtual role models like bud/coding), so providers (Go, OpenRouter, Nous Portal) swap underneath; skills/instructions stay in runtimes. Recommended: build it in Go; alternatives LiteLLM (Python, PyPI compromised Mar and Oct 2026) or OpenRouter as sole upstream. Copilot subscription: official in OpenCode since 2026-01, no clear terms for gateway/API use.
