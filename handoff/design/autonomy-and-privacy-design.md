# Fewer approvals, lighter privacy routing: plan

> Status: approved by Jason 2026-11-22 (decision card: "Approvals + evidence-based privacy"). Nothing is built yet; build issues are filed (section 4).
> Date: 2026-11-22

## 0. The one-paragraph version

Bud asks too often and runs too much on the slow local model. Both come from the same habit: when Bud can't prove something is safe, it treats it as risky. Today every Signal chat runs on Qwen on the Qwen box (about 24 tok/s), because Hermes's private profile lists only P0 models (Suite D95). And almost every terminal command Hermes runs asks on the phone, even though it runs inside the Docker sandbox. The proposal flips the default to "safe unless there's evidence", and makes the evidence deterministic. Small, stdlib Go checks decide when a request is actually sensitive or risky: secret and PII patterns, where the data came from (a calendar or mail tool result marks the whole conversation P0), and rules keyed by tool and argument. Approvals then trigger less often, and only for effects that leave the house or can't be undone. Privacy can still only be raised automatically, never lowered (Suite D23). The default it starts from becomes P1, Go's zero-retention models, instead of P0.

### Guiding principles

1. **Ask for effects, not for tools.** One tool can do something harmless or something irreversible, so the rule reads the arguments.
2. **The sandbox is the boundary for local effects; approvals guard what leaves it.** A command in Hermes's Docker sandbox can't reach the host, the vault or bud's files (docs/internals/sync.md), so it shouldn't need Jason.
3. **Deterministic first.** A regex, a checksum or the tool a result came from either matches or it doesn't. A model classifier only ever raises a tier, and only after the rules have run.
4. **Leaves stay the enforcement point (Suite D97).** The gateway, router, ops and hermes-gate keep the checks; nothing moves into a patched runtime.
5. **Stdlib only (D1).** Borrow rule data and designs from other tools, never their modules.

## Decisions made

Jason picked the first option on 2026-11-22, which took P1 to P8 as proposed. They're Suite D114 to D121, in order: P1 is D114, P2 D115, P3 D116, P4 D117, P5 D118, P6 D119, P7 D120 and P8 D121. The P labels stay in the table below so earlier references still resolve.

| #  | Decision (2026-11-22) | Notes |
| -- | --------------------- | ----- |
| P1 (Suite D114) | **Hermes's sandboxed terminal runs alone by default; a short ask list covers effects that leave the sandbox.** | Today the coding-device shell rules (`content/env/base.jsonc`, "anything not listed asks") also gate Hermes, so `ls`, `cat`, `curl` and `python3` all ask. Meanwhile `execute_code` is already `allow`, and it can run the same code (task 1 checks that it runs in the same container). The ask list: `git push`, `ssh`/`scp`, publishing (`npm publish`, `gh release`), and anything naming a host-mounted path. `op` stays denied. |
| P2 (Suite D115) | **One prompt per gateway write: mark Linear, calendar and mail as trusted in Hermes, since the gateway now asks at the leaf.** | r6b open question 4. Today a Linear write can ask twice: once from Hermes's own untrusted-server check and once from the gateway. The gateway's check is the strong one (exact argument bytes, one-shot, D103). |
| P3 (Suite D116) | **Argument-keyed approval rules at the gateway and in hermes-gate.** | Extend the `approval` block with an optional `when` that matches argument fields (equals, glob, absent, list empty). Examples: `calendar_create_*` with no attendees runs alone; with attendees, it asks, since that sends invites. `mail_send_*` to Jason's own address runs alone. `cronjob_manage` with action `list` runs alone. Linear deletes keep asking. Written as a small stdlib matcher, not OPA or Cedar (section 3). |
| P4 (Suite D117) | **Private-profile chats start at P1; deterministic evidence raises them to P0.** | Supersedes the D95 rule that Hermes's private profile lists only P0 models. Hermes asks the router for `bud/manager` at P1, and the router raises the tier to P0 on any of: the `#private` marker, the secret and PII rule packs (P5), or a P0 tool result in the conversation (P6). The gateway keeps the calendar and mail P0-only, so nothing changes on the tool side. |
| P5 (Suite D118) | **Rule packs: a secret pack ported from gitleaks' MIT rules, and PII validators with checksums.** | The secret pack is a curated subset (cloud keys, tokens, JWTs, private keys, `ops_` service tokens) rendered into `privacy_rules`, plus a Shannon-entropy check like gitleaks'. The PII part is card numbers checked with Luhn, IBANs checked with mod-97, US SSNs with range rules, and phone numbers. Personal terms (names, address, health, money) go in the overlay as a deny-list. All of it only raises the tier. |
| P6 (Suite D119) | **Taint by provenance: a conversation holding a result from a P0-only tool stays P0 for the rest of the conversation.** | The router can see every earlier tool call's name in the request history (`tool_calls[].function.name`). A call to `calendar_*`, `mail_*` or any server the registry marks P0-only raises the request to P0. This is the taint gate D95 rejected inside Hermes (its `pre_llm_call` hook fails open), but built at the router, which is a Bud-owned leaf. |
| P7 (Suite D120) | **The Gemma E4B classifier (D67) only breaks ties, and it's off at first.** | It would run only on requests that no rule raised, and it could only raise. Leave it off until the audit (task 1) shows that rules miss things. Running it on every chat adds latency to every message, and failing closed to P0 when it's down brings back the slow path. |
| P8 (Suite D121) | **"Simple" requests don't go local by default.** | The local model's latency hurts short answers too: Hermes's long system prompt has to be processed first. Qwen stays for P0, for when the cloud is down, and for unattended C0 chores (curator, classifier, cron) where speed doesn't matter. A `bud/quick` role on a Go flash model covers quick questions. |

What stays gated whatever is picked: sends to anyone but Jason (mail, invites, messages), pushes to main and merges, spending past the $10 top-up cap, changes to the home server's host and services, anything touching secrets or the vault, loosening approvals or privacy rules, the P0 fallback to a cloud model (D104/D105), and deleting staged memory (D113, which relies on "deny without an answer" never deleting anything).

## 1. What asks today

This is from the config at origin/main fa7ce64. Real counts are task 1's job: the gateway's `mcp-audit.jsonl` records every decision, and Hermes logs every approval it raises.

| Source | What asks | Likely volume |
| ------ | --------- | ------------- |
| hermes-gate, `terminal` | Every command not on the coding allowlist, plus anything with `$( )`, pipes into a subshell, or redirects. Coding tools (`go test`, `git status`) run alone; `ls`, `cat`, `grep`, `curl`, `python3`, `pip` and `jq` all ask. | High: most of what the agent does in the sandbox |
| Hermes manual mode | Any command Hermes itself deems dangerous, on top of the gate | Low |
| hermes-gate, tiers | `browser_exec`, `browser_cdp` and `browser_dialog` (page reads included), `cronjob_manage` (listing jobs included), any tool not named | Medium |
| Hermes untrusted MCP servers | Any non-read-only tool on Linear, GitHub, calendar or mail | Medium, and doubled by the gateway |
| Gateway approval tiers | Linear writes; calendar create, move and delete; mail send and reply | Medium |
| Router | P0 fallback when the Qwen box is down (D104), and spend past the cap (not built) | Low, but often, since the Qwen box sleeps |
| memory-review (D113) | Every staged memory record, up to four open at a time, asked again after a day | New; depends on how much Hermes stages. Options for the audit: auto-approve low-risk records (P2 observations, or a given source or kind), or one daily batch |

## 2. The privacy path after P4 to P6

1. Hermes's private profile sends `X-Bud-Privacy: P1` to the router for chat. For the calendar and mail it keeps sending P0 to the gateway (sync.md), so those tools stay P0-only.
2. The router runs `privacy_rules` (the marker, the secret pack, PII, the overlay's personal terms) over every message, as it does today (D112).
3. The router runs the provenance rule over the history's tool calls. A P0-only tool anywhere in the conversation makes the request P0.
4. If nothing raised it, `bud/manager` at P1 skips Qwen and picks the first Go model with allowance left (the pool needs reordering: Qwen moves out of first place for P1).
5. If something raised it, the request is P0 and goes to Qwen, with the D104 fallback as today.

A remaining risk: private text can reach a cloud model when no rule matches it and it didn't come from a P0 tool. One example is Jason typing something personal in plain words. The answers are the overlay's personal deny-list, the `#private` marker, and later the classifier. The Go models keep requests for zero days (registry `retention: 0`, D65), so P1 means "a provider sees it, doesn't keep it". Whether that's acceptable is the core of decision P4.

Open: Bud memory search results. The memory server allows P0 to P2, and a recalled record can hold private text. Either memory search results taint like calendar results do (simple, but most chats would end up P0 again), or records carry their tier and memory-mcp leaves P0 records out of answers to P1 sessions. I recommend the second, as a follow-up.

## 3. Guardrail tools looked at

| Tool | What it is | Fit | Use |
| ---- | ---------- | --- | --- |
| gitleaks | Secret scanner. Its default config is a TOML list of about 200 rules: an RE2 Go regex each, keywords, and an entropy threshold. MIT. | High: the regexes already compile under Go's `regexp`. | Port a curated subset as rule data (P5). Don't import it. |
| TruffleHog | Over 700 detectors that check a credential live against its provider's API. AGPL-3.0, with a long dependency list. | Low: the licence, the dependencies, and live checks send the secret out. | No. |
| Microsoft Presidio | Python PII analyzer: regex recognizers with checksums and context words, plus spaCy named-entity recognition, served over REST in Docker. MIT. | Medium: a good catalogue, but a Python service, and the NER part isn't deterministic. | Borrow the recognizer design (pattern, checksum, context words) for P5. A sidecar later, only if the rules miss names. |
| Open Policy Agent | Rego policy engine, embeddable as a Go library. | Low: a large third-party module (D1). Bud's tiers are already data. | No. |
| Cedar (cedar-go) | AWS's authorization language in Go, v1.9, Apache-2.0. It depends on go-json-experiment, and its schema validator is still experimental. | Low to medium: neat permit and forbid semantics, but third-party, and more than tool-and-argument rules need. | No. Copy "forbid beats permit" into the P3 matcher. |
| Codex `is_safe_command` | OpenAI Codex's read-only command classifier: a fixed list of commands with per-command unsafe flags (`find -exec`, `rg --pre`, `git --output`). Apache-2.0. | High as a design. | Use it for the P1 ask list, and for the coding devices' allowlist if P1 is declined. Lesson from its issue #28732: match a path-qualified `./sed` as a different command from `sed`. |
| NeMo Guardrails | NVIDIA's Python rails framework, Apache-2.0. Its tool rails check calls against an allowlist and a JSON schema; they're experimental and Chat Completions only. | Low: Python, and the gateway already does this. | No. |
| Llama Prompt Guard 2 | A 22M- or 86M-parameter classifier for prompt injection, under the Llama licence. | Later: it can't decide, it can only add evidence. | Possibly to mark web pages and mail as untrusted content. Not for gating. |
| LLM Guard | Scanner pipeline. Archived in July 2026. | None. | No. |

The pattern every one of these comparisons lands on is: cheap deterministic checks first, model checks only for what's left, and neither one solves prompt injection. Bud's real defence against injection stays where it is: the sandbox, and approvals on effects that leave the house.

## 4. Build order

Each step ships and tests on its own. Steps 2 to 4 (approvals) and 5 to 7 (privacy) are independent of each other.

1. **Audit approvals (JGA-376).** Count a week of real asks by source, from the gateway audit log and Hermes's approval log. Check this doc's section 1 against them, and say which of P1 to P3 removes the most asks. Ends with Jason confirming the allow, ask and never lists.
2. **Sandbox terminal tier (D114, JGA-377).** hermes-gate gets a Hermes shell rule set with a default of allow and a Codex-style ask list. Tests: table tests on the ask list, the subshell and redirect cases, and path-qualified commands.
3. **One prompt per write (D115, JGA-378).** Hermes trust for the gateway-enforced servers. Tests: a sync render test, and a doctor check that each trusted server has an `approval` block at the gateway.
4. **Argument rules (D116, JGA-379).** A `when` matcher in `approval` blocks and the tiers file, with forbid beating permit, and an unreadable or missing field meaning ask. Tests: the matcher, plus gateway tests for calendar attendees and the mail recipient.
5. **Rule packs (D118, JGA-380).** A generator for the secret pack from gitleaks' TOML (checked in as JSONC, with the licence noted), and PII validators. Tests: positive and negative samples for each rule, and a benchmark (the 50 ms first-screen budget doesn't apply here, but the router shouldn't add more than about 5 ms).
6. **Provenance taint (D119, JGA-381).** The router reads tool-call names in the history. The registry marks P0-only servers. Tests: a calendar call in the history raises the request; a fake cloud recorder sees zero P0 bytes.
7. **P1 default for private chats (D117, D121, JGA-382; blocked by JGA-380 and JGA-381).** Hermes's private profile sends P1 for chat, the manager pool is reordered, and a `bud/quick` role is added. Tests: a sync render test, and end-to-end tests on the home server with both cloud and local up and down.
8. **Classifier tie-break (D120).** Not filed; only if the audit shows a need.

## 5. Open questions for you

1. ~~How far should privacy loosen?~~ Answered 2026-11-22: approvals plus evidence-based privacy (D114 to D121).
2. ~~Simple requests local?~~ Answered: no (D121). Original question: I recommend no (P8), because Qwen's prefill makes even short answers slow. If you'd rather keep cost at zero for quick chats, the alternative is to keep Qwen first for messages under about 200 characters with no tools.
3. ~~Sandbox terminal by default?~~ Answered: yes (D114). Original question: I recommend yes. The risk that's left is exfiltration of what's already in context. That risk exists today through `execute_code` and `web_extract`, which already run alone.
4. **Personal deny-list:** which names, places and topics (health, money, family) should always stay local? These go in the overlay, never in this repo.
5. **Memory recall and privacy:** should records carry a tier (recommended) or taint the conversation?

## 6. After the build: memory (Suite D130, JGA-389)

**D130 (2026-11-22, Jason's choice on the card "Choose how Hermes's memory should stay private now that chats run at P1").** Hermes's private profile reaches Bud memory at P0, so records it saves default to P0 and it can recall every tier. memory-mcp marks each P0 record in a search result with `bud-p0-record-7c41` (`registry.P0Marker`), and the router raises a request holding the marker to P0 (`X-Bud-Privacy-Raised: memory`) and keeps the conversation there. A P1 chat that recalls only P1 and P2 records stays on Go. Env servers opt in with `marks_p0`, content format 9. Rejected: saving at P0 but searching at P1 (Hermes could never recall a private record), and making memory P0-only (most chats would go local again). Why it was needed: Hermes had reached memory at the home server's device tier, P1, so a record saved in a chat that read the calendar was P1 and a later P1 chat could recall it on Go. Records approved before this are worth a re-check. Delegated subagents stay a separate follow-up (JGA-390).
