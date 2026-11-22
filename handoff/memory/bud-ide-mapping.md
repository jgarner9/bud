---
name: bud-ide-mapping
description: Repos behind Bud Suite (bud, bud-brain, bud-plugins), the IDE mapping proposal, and OpenCode V2 facts that shape it
metadata:
  type: project
  modified: 2026-11-15T01:08:18.929Z
---

Mapping written 2026-11-15 at /mnt/project-files/architecture/bud-ide-mapping.md, summarized in section 10 of the architecture doc, with open questions Q8–Q13 added. All proposals, nothing decided.

Repos (public read via git proxy, no attach needed): `jgarner9/bud` (public IDE, Go CLI + Neovim, v1.1.0, BASE.md D1–D149, already ACP with an `opencode` backend); `jgarner9/bud-brain` (private, 2026-11-13 plan on OpenClaw + Basic Memory + $20 subs, BASE.md D1–D28, `brain` Go tool with run/locate/search/fetch built); `jgarner9/bud-plugins` (being archived). bud-brain conflicts with Suite D3/D6/D7/D9/D10/D13; reconciling it is Q8/Q13.

Jason's IDE asks (2026-11-15): slim or redo it; inline autocomplete, chat window, generative tooling; built around planning/brainstorming and running scripts, tests and metrics; Bud assists, Jason does the thinking (so autonomous features like bud.ai.work are proposed for dropping).

OpenCode V2 facts checked from its repo docs: reads AGENTS.md only (no CLAUDE.md fallback); skills are SKILL.md dirs (also reads ~/.claude/skills, `skills` config array, HTTPS catalogs); agents/commands are Markdown with front matter; hooks only in TypeScript plugins; `opencode acp` always starts a private local server (can't attach to a remote one); tools run where the server runs; TUI attaches with `--server <url>`. opencode.ai is blocked by the sandbox proxy; read docs from github.com/sst/opencode `services/site/src/docs/content`.

Related: [[bud-suite-architecture-doc]], [[bud-suite-decisions]]

Repo decisions as of 2026-11-15: D14–D20, then D21 (supersedes D14/D18/D19 on location): the Suite monorepo is PUBLIC with fresh history, bud-brain stays a private archive, personal parts (device profiles, tailnet names, personal env and instructions) live in a private overlay repo (proposed name `bud-overlay`, git over SSH, deploy key in the Bud vault on the home server). KB is its own private repo (D20). Q16 open: grow `jgarner9/bud` into the monorepo (recommended, keeps releases and existing installs) or a new repo and archive bud. No repo has been created, moved or archived.
D22 (2026-11-15): Q16 answered, `jgarner9/bud` itself grows into the public monorepo; nothing is archived. bud-brain's work comes in as new files (its history stays private).
Routing decided 2026-11-15: D23 three privacy tiers with rule-first pre-tagging; D24 cost tiers C0 Qwen / C1 Go Plus included / C2 Go Extra Usage capped ~$10/mo, approval past cap by text message (channel is Q17, open); D25 llama.cpp serves Qwen on an RX 6700 XT 12 GB + 32 GB DDR5 (Vulkan, experts on CPU, separate FIM model, moderate context are proposals); D26 Wake-on-LAN from the home server for queued P0 work, sleep after idle. Q7 closed.
D27 (2026-11-15): approval texts go by iMessage; open: which always-on Mac sends, and fallback vs replace for the Tauri push (fallback proposed).
D28 (2026-11-15): approvals push to the Tauri app first, iMessage fallback after a few minutes unanswered; only open part of Q17 is which always-on Mac sends.
D29 (2026-11-15): texts only as push fallback or when Jason asks; texting lives in the personal management layer. Jason has no always-on Mac, so D27 (iMessage) is under reconsideration: Twilio SMS (recommended), a Mac mini, or ntfy; D27 stands until he picks.
D30 (2026-11-15): texts go by Signal via signal-cli on the home server, on Jason's spare T-Mobile number as a bot number; reaction approves/denies, 'yes <code>' backup; only Jason's number accepted; reg-lock PIN in the Bud vault. Supersedes D27, closes Q17.
D31 (2026-11-15): mail is read-only plus sends with approval (including thread replies and invite accepts); cleanup/deleting junk allowed (proposed: trash only, never permanent, with a digest). Closes Q13.
D32 (2026-11-15): inline autocomplete = Qwen FIM first, cloud fallback when Qwen is off, never on P0 paths. Proposed provider Copilot Free (2,000 completions/mo, checked 2026-11-15) then Pro $10 unlimited; alternative minuet on a Go Plus model.
D33 (2026-11-15): memory staleness 6–12 months for standard data, long-standing info refreshed not archived (proposed: flag at 6 mo, archive at 12, evergreen pages get a refresh check); promote to KB after 2 sightings or when Jason asks to doc-commit.
D34 (2026-11-15): unanswered approvals expire and are denied; Jason can pre-approve in his prompt or for a session (proposed guardrails: 1 h expiry, only from Jason directly, scoped to named action+target, never-tier excluded, always logged).
D35 (2026-11-15): IDE planning/brainstorm chats run on the home server's OpenCode; coding stays local. Q12 (chat window) now = how bud's chat reaches the server (HTTP API, sudo-tee/opencode.nvim, or server TUI in a pane); Jason wants a deep-dive.
D36 (2026-11-15): mail cleanup runs alone, trash only (~30 days recoverable), never permanent delete, digest of what was cleared. Refines D31.
D37 (2026-11-15): autocomplete fallback = Copilot Free, Pro only if cap hit, off on P0. Refines D32.
D38 (2026-11-15): autocomplete fallback = fast Go Plus model via minuet (supersedes D37 Copilot); proposed MiMo-V2.6-Flash (cheapest, EU/US region, ~300k req/mo on Go Plus); stop rather than spill into C2; Go terms for completion traffic unchecked.
D39 (2026-11-15): staleness confirmed: flag at 6 mo without reference, archive at 12 unless refreshed, references reset the clock, evergreen pages get a refresh check. Refines D33.
D40 (2026-11-15): pre-approval guardrails confirmed (only from Jason directly, never from read content; named action+target only; never-tier excluded; always logged). 1-hour expiry and session-end still proposals. Refines D34.
D41 (2026-11-15): git commit asks in the editor; docker, make, npm run alone; Jason can add tools to run-alone by asking. Proposed guardrails: docker prune/rm/--privileged/outside mounts ask; npm install of a new package asks; additions only from Jason directly, stored as permission rules in the overlay env, shown in digest. Refines D16.
Q12 (2026-11-15): sudo-tee/opencode.nvim is the leading option, not decided. Checked its docs at commit 96c610b: one server per setup (server.url or local V2 service), no runtime switch found; no :BudGen equivalent; diff review is after the fact, not before-write.
D42 (2026-11-15): approvals expire after 1 h (setting) and are denied; session pre-approvals end with the session. Refines D34/D40; the approval rules are now fully decided.
D43 (2026-11-15): D41 run-alone guardrails confirmed as proposed (docker/npm exceptions ask, make alone, additions only from Jason, stored in overlay env, shown in digest). Q12 still awaiting Jason.
D44 (2026-11-15): Q12 answered: opencode.nvim is the chat window, :BudGen kept, bud writes the server switch. Proposed: no fork up front; pin a reviewed upstream commit, switch as a bud Lua module, upstream PR; fork only if needed (small rebased patch set, review every upstream diff, CI, no auto-merge).
D45 (2026-11-15): opencode.nvim pin/fork policy confirmed (pinned reviewed commit, bump after reading diff, switch as bud module + upstream PR, fork only if needed as rebased patch set with CI, no auto-merge). Refines D44.
Tool inventory (2026-11-15): new section 12 in the architecture doc, two tables (third-party, Bud-owned) with Tool and Service columns per Jason. Proposed new: Hugo for the KB site, sqlite-vec, an embedding model on the Qwen box (gap). Open questions now section 13, decisions 14.
2026-11-15 proposals (not decided): provider/model registry in content/env/registry.jsonc + overlay override, roles (manager, coding, planning, classifier, FIM, embedding, fallback, curator), bud sync renders each runtime's config, doctor checks drift; runtime adapters in internal/; Bud-owned self-improvement loop (observe, propose diff/PR, review queue, approve under D10, edge-then-stable rollout, revert, check) plus weekly landscape scan. Headroom (headroom-ai, Apache-2.0) is a context-compression proxy, NOT an embedding model; listed as a candidate. Manager runtime (Hermes vs OpenClaw etc.) researched in another thread.
D46 (2026-11-15): provider/model registry + bud sync rendering + doctor drift + runtime adapters DECIDED. D47: Bud-owned self-improvement loop + weekly landscape scan DECIDED.
D48 (2026-11-15): Hermes Agent (NousResearch/hermes-agent) is the home-server manager; OpenCode stays local coding. Required: manual approvals wired to D10 via pre_tool_call, staged skill+memory writes, synced skills locked, terminal in Docker, Bud plugin for Signal reaction approvals. Supersedes server-runtime parts of D3/D12, narrows D17. Q18/Q19/Q21 answered (D48/D46/D47). Open knock-ons: Q22 IDE planning chats (OpenCode serve vs Hermes), Q23 Tauri via Hermes API, Q24 Hermes memory vs Bud memory. Q20 embedding (Qwen3-Embedding-0.6B) still proposed.
D49 (2026-11-15): IDE planning chats -> small opencode serve on home server beside Hermes (Q22 answered). Proposed: shares registry, skills, memory (MCP over SQLite). Open: Q20, Q23, Q24.
D50 (2026-11-15): Tauri app -> Hermes OpenAI-compatible API, generic shape (Responses + Bud approval API). Q23 answered. Open: Q20, Q24.
D51 (2026-11-15): Hermes memory pointed at Bud SQLite (source of truth) if performance holds. Proposed: Bud memory-provider plugin over shared memory MCP server; recall p95 < 300 ms measured in build; fallback Hermes built-in + curator sync. Only Q20 open.
D52 (2026-11-15): Qwen3-Embedding-0.6B on llama.cpp on the home server. Q20 answered; all Q1-Q24 answered.
D53 (2026-11-15): Hugo, sqlite-vec, Headroom (optional) decided. D54: mail is iCloud; proposed IMAP/SMTP with app-specific password in Bud vault, cleanup to Deleted Messages (~30 days, verify). D55: Qwen tuning and FIM model deferred to that build step.
Build-time checks recorded (section 11, 2026-11-15): Go terms for autocomplete traffic, OpenCode reload after sync, D51 recall budget, iCloud Deleted retention, D55 Qwen tuning.
D56 (2026-11-15): repo layout decided (cmd/bud, internal, ide embedded, app via platform channels, server incl. Hermes + planning OpenCode + router + curator + Hugo, content, profiles examples, docs, scripts). Decide-now items 2-6 pending.
D57 (2026-11-15): content/ contents decided; all skills on every device. Items 3-6 pending.
D58 (2026-11-15): registry roles = manager, coding, planning, classifier, FIM, embedding, fallback, curator, vision; routing points at roles. Items 4-6 pending.
D59 (2026-11-15): overlay = jgarner9/bud-overlay, merged by path+ID, overlay wins, doctor shows layer. Items 5-6 pending.
D60 (2026-11-15): code and content signed, unsigned/downgrades refused; overlay unsigned (SSH, private). Item 6 channels pending.
D61 (2026-11-15): stable+edge channels, separate code/content versions, laptop edge, home server stable. Decide-now list complete.
