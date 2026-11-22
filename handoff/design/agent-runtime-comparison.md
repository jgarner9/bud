# Agent runtime for the home-server manager

Companion to [bud-suite-architecture.html](bud-suite-architecture.html), sections 5, 7 and 13 (Q18 to Q21). Written 2026-11-15. Research only: nothing here is decided, and no repo was changed.

**How to read the marks.** ✓ means I read it directly in the project's own docs or source (repos cloned 2026-11-15, commits from that day). ◇ means it comes from a secondary source such as a blog or advisory aggregator, linked. ~ means I inferred it and did not check it.

---

## 1. The short answer

- **Hermes Agent is a good fit for the 24/7 manager, with four settings changed from its defaults.** It already does most of what Bud would otherwise build: a Signal channel on signal-cli, cron with delivery to Signal, agentskills.io-format skills, MCP, 1Password `op://` resolution, an OpenAI-compatible API with an approval endpoint, and a `pre_tool_call` hook that can require a human approval for any tool. Its defaults lean toward autonomy (an LLM can auto-approve commands, and it writes skills and memory on its own), so Bud must turn those off. Section 5 lists the exact settings.
- **OpenClaw is the closest competitor and has a better built-in memory match, but its security record is much worse.** It has 722 published advisories, 14 of them critical, a one-click RCE (CVE-2026-25253) and a malicious-skills campaign on its skill hub ◇. Its defaults are also more permissive. Bud-brain already moved off it (D14).
- **OpenCode Go Plus works outside OpenCode.** It serves OpenAI-compatible Chat Completions and Responses, plus Anthropic Messages and Gemini endpoints ✓. Hermes is on OpenCode's list of validated clients ✓. The catch is that Go says it is "designed for OpenCode and other coding agents" and watches for abuse ✓, so a manager doing mail and reminders on Go is a grey area. Keep routine manager work on Qwen (C0).
- **Headroom is not an embedding model.** It is a context-compression proxy and library ✓. For working-memory vectors I propose **Qwen3-Embedding-0.6B** (Apache-2.0) served by llama.cpp, with where it runs left for you to choose (Q20).
- **Don't hand-sync provider settings across configs.** Make one Bud-owned model registry the only place providers and models are named, and have `bud sync` write each runtime's config from it (Q19). That is also the base for the self-improvement loop in section 7.

---

## 2. Hermes Agent and OpenClaw, side by side

| | Hermes Agent (Nous Research) | OpenClaw (OpenClaw Foundation) |
|---|---|---|
| **License** | MIT ✓ | MIT ✓ |
| **Language and runtime** | Python 3 plus Node for the TUI and desktop app ✓ | TypeScript on Node 24+ ✓ |
| **Releases** | Date tags, latest `v2026.9.24`; main moves daily (60 commits in about a day) ✓ | `2026.11.3` in `package.json`; about 50 commits in a day ✓ |
| **Size of community** | About 259k GitHub stars on 2026-11-01 ◇ ([heyhermes.app](https://heyhermes.app/guides/openclaw-alternatives), a Hermes fan site) | About 390k stars ◇ ([tomrochette.com](https://tomrochette.com/agents/assistant-runtimes-feature-matrix/)) |
| **Architecture** | One gateway process holds every messaging platform, the cron scheduler, the API server and the curator. Sessions live in SQLite (`state.db`) ✓ | One Gateway is the control plane for sessions, tools, channels and events. The Control UI, CLI, TUI and companion apps connect to it over WebSocket ✓ |
| **Server mode and API** | `hermes gateway` with `API_SERVER_ENABLED=true` serves an OpenAI-compatible API on `127.0.0.1:8642` with a bearer key: `/v1/chat/completions`, `/v1/responses`, a Runs API with SSE events, `POST /v1/runs/{id}/approval`, and a Jobs API for cron ✓. Also an ACP server for editors ✓ | Gateway WebSocket protocol, plus optional OpenAI Chat Completions (`/v1/chat/completions`, `/v1/models`, `/v1/embeddings`) and OpenResponses. Disabled by default, and the docs say the token is "equivalent to an owner/operator credential" ✓ |
| **Signal** | Built-in adapter on the signal-cli HTTP daemon (JSON-RPC plus SSE). `SIGNAL_ALLOWED_USERS` allowlist, groups off by default, native formatting, reply quotes, sends reactions, attachments up to 100 MB, numbers redacted in logs ✓. Docs show linking as a secondary device; a dedicated registered number should work, since `SIGNAL_ACCOUNT` is just "the bot phone number" ~ | Plugin `@openclaw/signal` on signal-cli (native daemon or the bbernhard REST container). Its docs recommend a separate bot number and document SMS registration of one ✓, which matches D30 exactly |
| **Reaction approvals (D30)** | Typed `yes`/`no` answers approvals on messaging ✓. The Signal adapter sends reactions but I found no code that treats an incoming reaction as an approval answer ~, so D30's thumbs-up needs a small Bud approval-transport plugin | Not checked |
| **Other channels** | Telegram, Discord, Slack, WhatsApp, Matrix, email, SMS, ntfy, webhooks and about 30 more ✓ | About 50 channels, including iMessage ✓ |
| **Memory** | Small always-in-prompt `MEMORY.md` (2,200 chars) and `USER.md` (1,375 chars), plus FTS5 search over every past session ✓. Pluggable memory providers (Honcho, Mem0, Hindsight and others), and you can write your own ✓ | Built-in engine in per-agent SQLite: FTS5 keyword plus vector search, hybrid ranking, optional sqlite-vec, and local embeddings through a llama.cpp provider ✓. "Dreaming" consolidates short-term signals into `MEMORY.md` on a schedule, on by default ✓ |
| **Skills format** | SKILL.md, compatible with agentskills.io ✓ | SKILL.md skills plus "learned skills" in Skill Workshop ✓ |
| **MCP** | Client for stdio and HTTP servers, with a reviewed catalog ✓ | Client (stdio, SSE, Streamable HTTP) and `openclaw mcp serve` ✓ |
| **Providers and fallback** | 40+ providers including built-in `opencode-go`, custom OpenAI-compatible endpoints (llama.cpp), credential pools, and a fallback chain that switches provider mid-session ✓ | 80+ provider docs including built-in `opencode-go` ✓, model failover ✓ |
| **Scheduling** | Cron with natural language or cron expressions, skills attached per job, delivery to any platform (`SIGNAL_HOME_CHANNEL`), script-only jobs with no LLM, webhook-triggered jobs. A job's model can be pinned only by you, never by the agent ✓ | "Automations" scheduler with delivery to a channel or webhook ✓ |
| **Approval model** | Pattern list of dangerous shell commands, with modes `smart` (an auxiliary LLM auto-approves low risk), `manual` or `off`. Timeouts fail closed. Separate settings for cron, one-shot and API sessions (`deny` by default) ✓. The `pre_tool_call` hook can return `approve` to send **any** tool call (mail send, git push) to the human gate, or `block` ✓. Plugins can register an approval transport, such as a push to the Tauri app ✓ | Exec modes `deny`, `allowlist`, `ask`, `auto`, `full`. `before_tool_call` hooks can block or `requireApproval` for any tool ✓. One independent review found the default is full access with no approval and no sandbox ◇ ([anchorterminal.com](https://www.anchorterminal.com/blog/best-harnesses-for-personal-agents)) |
| **Self-modifying behavior** | On by default: the agent creates and patches skills, saves memory, and a background review after turns writes more ✓. Gated with `skills.write_approval: true` and `memory.write_approval: true` (writes are staged for `/skills approve`), `auxiliary.background_review.enabled: false`, `curator.consolidate: false` (already the default), and `hermes curator lock <skill>` so the agent can't change a skill at all, even under `--yolo` ✓ | On by default (`autonomous.mode: "auto"`). A `propose` mode saves drafts for `/learn drafts`, or disable the `skill-workshop` plugin; dreaming has its own off switch ✓ |
| **Secrets** | `op://` references in `config.yaml`, resolved by `op read` at start with a service-account token ✓. They land in the process environment; the local terminal strips Hermes's own keys from child processes ✓, but the agent runs as your user, so it could likely read `~/.hermes/.env` or the token file ~. Use the Docker terminal backend or a separate Linux user | SecretRef fields resolved through `op` with a service-account token file ✓, plus a bundled `1password` skill that lets the agent read secrets itself, which Bud would leave out (D7) |
| **Security record** | 45 CVEs in 2026: 1 critical (MCP catalog supply chain, 9.0), 4 high, 38 medium ◇ ([radicalnotion.ai](https://radicalnotion.ai/vendor/nousresearch/hermes-agent)). Defaults: strangers are ignored until allowlisted, and no telemetry ◇ | 722 advisories (14 critical, 249 high), 39 with a CVE ◇ ([clawdocs.org](https://clawdocs.org/security/known-vulnerabilities)). One-click RCE CVE-2026-25253, hundreds of malicious ClawHub skills, and tens of thousands of internet-exposed gateways ◇ ([penligent.ai](https://www.penligent.ai/hackinglabs/over-220000-openclaw-instances-exposed-to-the-internet-why-agent-runtimes-go-naked-at-scale/)) |
| **Footprint** | "Runs on a $5 VPS" ✓; light, since inference is remote ~ | Its docs cite a 1 GB droplet ✓; one comparison measured about 1.5 GB baseline ◇ |
| **Clients** | Desktop app (macOS, Windows, Linux), TUI, dashboard; the API suits a Tauri client ✓ | Control UI plus companion apps for macOS, iOS, Android, Windows and Linux ✓ |
| **Delegating coding to OpenCode** | Optional `opencode` skill ✓ | ACP harness covers OpenCode ✓ |

**What this means for Bud.** Hermes is the safer base, and the settings that matter (manual approvals, staged skill and memory writes, locked skills, per-tool approval hooks) are documented features rather than patches. OpenClaw is stronger on built-in memory and native mobile apps, but Bud owns memory anyway (D9, D13) and the Tauri app is already decided (D6). That leaves OpenClaw's main advantage outside what Bud needs, and its security record inside it.

---

## 3. Other realistic runtimes

| Runtime | What it is | Fits Bud because | Doesn't fit because |
|---|---|---|---|
| **OpenCode serve** (the current D17 plan) | The OpenCode server Bud already uses for coding. HTTP API, permissions (allow, ask, deny per tool and command pattern), MCP, SKILL.md skills, TypeScript plugins ✓ | One runtime for everything, and the IDE work (D44) already speaks it | No messaging, no scheduler and no phone approvals ~. Bud would build Signal, cron and the approval flow itself as plugins and systemd timers |
| **Letta Code** (Letta) | A stateful-agent harness: git-versioned memory filesystem, skills, schedules, sleep-time subagents for reflection, messaging including Signal, permissions with allowlist and approval ◇ ([docs.letta.com](https://docs.letta.com/llms.txt)) | The memory design is the closest to Bud's: git-backed and reflective | Built around Letta Cloud and its own server; the agents can rewrite their own system prompts and "mods" can change the harness, which needs gating. License and self-host maturity of Letta Code not checked ~ |
| **goose** (Agentic AI Foundation, Linux Foundation) | Rust agent with desktop, CLI and `goose serve` (ACP over HTTP and WebSocket), MCP extensions, a Skills extension, recipes and `goose schedule` with cron ◇ ([goose-docs.ai](https://goose-docs.ai/docs/guides/goose-cli-commands/)) | Foundation-governed, MCP-native, per-tool Always/Ask/Never permissions, Apache-2.0 ~ | No messaging channels; autonomous by default ◇ |
| **OpenHands** | Coding agent SDK with a REST and WebSocket agent server, confirmation policies, AgentSkills SKILL.md, MCP, and scheduled automations ◇ ([docs.openhands.dev](https://docs.openhands.dev/sdk/index)) | Strong for unattended coding jobs | Coding-first; no personal channels; the duplicate of OpenCode's job |
| **n8n** | Workflow automation with an AI Agent node, MCP client and server, and human review for chosen tools over Slack, Telegram, WhatsApp and others ◇ ([docs.n8n.io](https://docs.n8n.io/build/integrate-ai/ai-examples/human-in-the-loop-for-tools)) | Good for fixed workflows (mail triage, digests) | Fair-code (Sustainable Use License), not open source ◇. No Signal approval channel listed. An agent built as a graph of nodes is harder to keep in git and to swap |
| **ZeroClaw** | One Rust binary, about 30 channels, "supervised" autonomy by default (medium risk asks, high risk blocked), MIT or Apache-2.0 ◇ | Approval-first defaults and a tiny footprint | Thin memory and context; younger ◇ |
| **NanoClaw** | Small TypeScript core; every agent in its own container; keys stay in a gateway outside it ◇ | Strong isolation | Built on Anthropic's Claude Agent SDK; customised by changing code, not config ◇ (D17 moves Bud off Claude) |
| **IronClaw** (NEAR AI) | Rust, tools in WebAssembly sandboxes, encrypted secrets never shown to tools ◇ | The best security architecture of the group | Few channels (Telegram, Slack, web); auto-approve on by default ◇ |
| **nanobot** (HKU) | Small Python agent with a web UI, memory, schedules and chat apps, MIT ◇ | Readable | No approval step; sandbox off until set ◇ |

**Reading of the field.** Only Hermes and OpenClaw cover Signal, cron, MCP, skills and phone approvals together out of the box. Letta Code is worth watching for memory; IronClaw for security ideas.

---

## 4. Recommendation and trade-offs

**Recommended:** Hermes Agent as the home-server manager, OpenCode stays as the coding harness beside the IDE, and Bud keeps the pieces it already owns outside both: memory, the KB, the approval service, the model registry and the skills repo. That last part is what keeps Hermes swappable.

| Requirement | Hermes as is | What Bud adds |
|---|---|---|
| Signal on a dedicated number | ✓ adapter | Register the bot number with signal-cli (D30); allowlist only Jason |
| Cron scheduling | ✓ | Jobs defined in the repo and synced; `cron_mode: deny` |
| SKILL.md skills | ✓ | Synced from the monorepo and locked |
| MCP | ✓ | Bud's tools exposed as MCP servers |
| 1Password `op://` | ✓ | Terminal backend in Docker or a separate user, so values stay out of reach (D7) |
| Approval tiers with phone approval | Partly: shell-pattern approvals plus `pre_tool_call` | A Bud plugin that maps D10's tiers onto `pre_tool_call`, and an approval transport that pushes to Tauri first, then Signal with reactions (D28, D30). The 1-hour expiry (D42) via `approvals.timeout` |
| Tauri thin client | ✓ API with Runs and approvals | Tauri speaks the OpenAI Responses shape plus Bud's approval API, so a runtime swap doesn't break it |
| SQLite memory plus git KB | Its own small memory | Bud's memory as an MCP server (portable), with a thin Hermes memory-provider adapter for automatic recall |
| Jason drives, Bud supports | Off by default | `approvals.mode: manual`, staged skill and memory writes, background review off, skills locked |
| Swappable providers and runtimes | ✓ many providers | The model registry (section 6) |

**Trade-offs you'd accept.**

1. **Two runtimes instead of one.** Hermes (Python) on the server and OpenCode (TypeScript) for coding means two upgrade streams and two configs. In return Bud doesn't build Signal, cron or messaging approvals itself.
2. **A very fast-moving upstream.** Dozens of commits a day and 45 CVEs this year. Pin a reviewed release and let only the Bud installer bump it, as D45 does for opencode.nvim.
3. **Changes to earlier decisions, if you pick it.** D17 (OpenCode for everything) narrows to coding. D12's bootstrap-only approach applies to Hermes too. Mapping gap G6 (approval service as an OpenCode plugin) becomes a standalone service with one adapter per runtime.
4. **Go Plus terms.** Manager traffic on Go is a grey area. Keep chores on Qwen and use Go for reasoning.

**Runner-up:** staying on OpenCode serve for the manager. That means fewer moving parts, but Bud builds Signal, cron and messaging approvals itself.

---

## 5. Hermes settings for "Jason drives, Bud supports" (proposed)

```yaml
# ~/.hermes/config.yaml, written by bud sync (proposed)
approvals:
  mode: manual            # not smart: no LLM approves on Jason's behalf
  timeout: 3600           # D42: one hour, then denied
  cron_mode: deny
  single_query_mode: deny
  unattended_mode: deny
  approvers: []           # only the requester (Jason) answers
skills:
  write_approval: true    # every agent skill write is staged for review
memory:
  write_approval: true
auxiliary:
  background_review:
    enabled: false        # no post-turn self-review; /refine stays manual
curator:
  consolidate: false
```

Plus: `SIGNAL_ALLOWED_USERS` set to Jason's number only, groups off; every synced skill locked with `hermes curator lock`; the terminal backend in Docker (or a dedicated Linux user) so the agent can't read `.env` or the 1Password token; `hermes update` disabled, with the Bud installer bumping a pinned version. That `timeout: 3600` is accepted is unchecked ~.

---

## 6. Keeping providers in sync, and staying adaptable

You asked whether we just keep the provider synced across configs. Hand-syncing is how drift starts. Instead, make the provider a value that is written in exactly one place.

**The model registry (proposed).** One file in the monorepo, with personal values in the overlay, names every provider and maps **roles** to models:

```yaml
# content/env/models.yaml (proposed)
providers:
  go:    { api: openai, base_url: https://opencode.ai/inference/go/openai/v1, key: op://vault/opencode-go/key }
  qwen:  { api: openai, base_url: http://qwen-box:8080/v1 }
  embed: { api: openai, base_url: http://home:8081/v1 }
roles:
  manager:    { model: go/kimi-k3,  fallback: [qwen/qwen-35b] }
  coder:      { model: go/glm-5.3 }
  classifier: { model: qwen/qwen-35b }   # P0-safe
  curator:    { model: qwen/qwen-35b }
  fim:        { model: qwen/coder-fim, fallback: [go/mimo-v2.6-flash] }
  embed:      { model: embed/qwen3-embedding-0.6b, dims: 1024 }
```

`bud sync` renders it into each consumer's own format: Hermes `config.yaml` (providers, fallback chain, cron model), OpenCode `opencode.json`, minuet's config and the memory service. `bud doctor` diffs the rendered files against what is live and reports drift. Changing a model becomes a one-line change plus a sync. The model IDs above are examples, not picks.

| Option | How | Gains | Costs |
|---|---|---|---|
| **A. Rendered configs** (recommended) | The registry is rendered into each runtime's native config | Nothing extra running; each runtime keeps its own provider features, such as Hermes's per-model routing and the `x-opencode-session` header Go asks for ✓ | Bud writes one renderer per runtime |
| B. Local model gateway | A proxy (LiteLLM or a small Bud router) exposes roles like `bud/manager`; runtimes point at it | Swap models without touching runtime configs; one place for the D23 privacy router | One more hop and service; it must forward Go's session header and user agent or it breaks Go's rules ✓ |

Start with A. B is the natural home for the router later, and D12's thin wrapper is the moment to decide.

**Adaptability principle (proposed).** Bud owns the contracts and treats runtimes as adapters:

- **Tools** are MCP servers, so any runtime can use them.
- **Skills** are SKILL.md in the repo (agentskills.io), so any runtime reads them.
- **Memory and the KB** sit behind Bud's own MCP server and git repo, never in a runtime's private store.
- **Approvals** run in Bud's approval service; each runtime has a thin hook adapter.
- **Clients** speak the OpenAI Responses shape plus Bud's approval API.
- **Models** are roles in the registry.

Swapping Hermes for OpenClaw, Letta Code or goose then means a new adapter and a new renderer. Memory, skills, tools, approvals and the app stay as they are.

---

## 7. A Bud-owned self-improvement loop (proposed)

Hermes and OpenClaw both improve themselves by **writing changes directly**. Bud's version should **propose changes for Jason to approve**, and use the registry so the system can follow the AI landscape. It has three parts:

1. **Signals** (collected automatically, read-only). Approval history (Hermes's `hermes approvals suggest` already mines it ✓), failed or retried runs, Jason's corrections, Go allowance use per model, curator stats, and the weekly changes in Go's model list (`models.opencode.ai`) and in pinned runtime releases.
2. **Proposals** (written by jobs, never applied). Three kinds:
   - Skill and memory edits, staged by Hermes's write-approval gate.
   - KB changes, made by the D9 curator.
   - A weekly **landscape scout** on Qwen that runs a small Bud eval set per role against candidate models and runtime versions. The eval set includes privacy-classification cases for the router, which matter most. The scout then drafts, for example, "move `coder` from X to Y: same eval score, 30% less allowance".
3. **Apply** (only Jason). Each proposal becomes a pull request to the monorepo or the overlay with the diff and the eval numbers, announced in the digest. Approving a merge is an approve-first action under D10. After the merge, `bud sync` renders the configs. Rollback is a revert.

The agent never changes its own config, models or locked skills. It only gathers evidence and drafts. That matches "Jason drives, Bud supports", and every change lives in git history.

---

## 8. Headroom and the embedding model

**What Headroom is ✓.** [Headroom](https://github.com/chopratejas/headroom) (`headroom-ai`, Apache-2.0, v0.44.0, now under headroomlabs-ai) compresses what an agent reads (tool output, logs, files, RAG chunks, history) before it reaches the model. It runs locally as a Python or TypeScript library, an HTTP proxy, a `headroom wrap opencode|openclaw|…` wrapper, or an MCP server. The project measured 21–57% fewer input tokens on agent tasks and over 90% on repeated JSON and logs. Originals are cached locally for retrieval. It *uses* small embedding models internally (bge-small, MiniLM) for relevance, but it is not one.

**What role it could play.** A compression proxy in front of Go Plus traffic, to stretch the per-model allowances, or its MCP tools on the manager for large tool output. The caveats: it is another moving part, and whether its proxy keeps Go's `x-opencode-session` header and user agent is unchecked ~. Measure Go allowance use first, and add Headroom only if limits bite. Your `bud:tools` skill already handles Headroom-compressed output.

**Embedding model (proposed): Qwen3-Embedding-0.6B** ◇ ([model card](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF), [comparison](https://www.maximem.ai/blog/local-embedding-models)). Apache-2.0, 32K context, 1,024 dimensions that can be cut down to 32 (Matryoshka), official GGUF at q8_0 (about 640 MB), served by a third `llama-server --embeddings`. Queries take an instruction prefix and documents don't, so the memory service adds it. The lighter alternative is **EmbeddingGemma 2** (Google, 2026-10-06, Apache-2.0, a 270M text backbone, 8K context) ◇.

**Where it runs is the real choice (Q20).** Working memory lives on the home server, which is always on. The Qwen box sleeps (D26).

| Option | Gains | Costs |
|---|---|---|
| **A. Home server CPU** (recommended, if it has about 1–2 GB spare RAM) | Always available; memory writes never wake the Qwen box; text stays on the home server, so it is P0-safe | Slower; fine at personal volume ~ |
| B. Qwen box GPU | Fast | Under 1 GB of VRAM beside the 35B and the FIM model on 12 GB ~; writes queue until the box wakes. FTS5 search works meanwhile, and vectors backfill |

Either way, store the model ID and dimensions with each vector, so a later model swap is a re-embed job rather than a migration. That is the registry principle again.

---

## 9. Sources

Read directly (✓):
- Hermes Agent repo and docs: [github.com/NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent), `website/docs/` (Signal, API server, cron, skills, curator, memory, security, hooks, 1Password, providers), Signal adapter source, at commit `f578714` (2026-11-15).
- OpenClaw repo and docs: [github.com/openclaw/openclaw](https://github.com/openclaw/openclaw), `docs/` (channels/signal, providers/opencode-go, tools/permission-modes, exec-approvals, self-learning, skill-workshop, concepts/memory-builtin, memory-search, dreaming, gateway/1password, openai-http-api, tools/mcp, automation/cron-jobs).
- OpenCode Go: [github.com/sst/opencode](https://github.com/sst/opencode) `services/site/src/docs/content/console/go.mdx` ("Where can I use it?", validated clients, endpoints, usage limits).
- Headroom: [github.com/chopratejas/headroom](https://github.com/chopratejas/headroom) README and source.

Secondary (◇): [radicalnotion.ai Hermes CVEs](https://radicalnotion.ai/vendor/nousresearch/hermes-agent) · [clawdocs.org known vulnerabilities](https://clawdocs.org/security/known-vulnerabilities) · [penligent.ai exposed instances](https://www.penligent.ai/hackinglabs/over-220000-openclaw-instances-exposed-to-the-internet-why-agent-runtimes-go-naked-at-scale/) · [anchorterminal.com harness grades](https://www.anchorterminal.com/blog/best-harnesses-for-personal-agents) · [tomrochette.com feature matrix](https://tomrochette.com/agents/assistant-runtimes-feature-matrix/) · [heyhermes.app alternatives](https://heyhermes.app/guides/openclaw-alternatives) · [joeywang.github.io architecture comparison](https://joeywang.github.io/posts/hermes-openclaw-nanoclaw-zeroclaw-ironclaw/) · [Letta docs index](https://docs.letta.com/llms.txt) · [Letta HITL](https://docs.letta.com/v1-sdk/tools/human-in-the-loop/index.md) · [goose CLI](https://goose-docs.ai/docs/guides/goose-cli-commands/) · [goose tool permissions](https://goose-docs.ai/docs/guides/managing-tools/tool-permissions/) · [OpenHands SDK](https://docs.openhands.dev/sdk/index) · [n8n human review](https://docs.n8n.io/build/integrate-ai/ai-examples/human-in-the-loop-for-tools) · [Qwen3-Embedding GGUF](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF) · [maximem.ai local embedding models](https://www.maximem.ai/blog/local-embedding-models).

Basic Memory failed to connect this session, so nothing was cached there.
