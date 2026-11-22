# Wiring the approval store into the MCP gateway and the ops server

Status: approved by Jason 2026-11-21 (Suite D103). Building in the slice order below.

Design note for Jason to approve. Status: proposed. Nothing here is built. Already decided and not reopened: approvals are enforced only at Bud-owned leaves (Suite D97); gateway and ops reviews go through the bud-signal Hermes plugin with the same flow, and Go never reads Signal (D101); an unanswered approval expires after one hour and is denied; a pre-approval counts only from Jason directly and covers only the named action. Citations are file:line at origin/main.

## What exists

`internal/approval` is a complete but unconnected helper. `Store.Dispatch` creates a pending request, calls a `Review` function, then approves and consumes it in one store transaction before running the executor (`dispatch.go:412-461`). The request binds a scope, action, target and SHA-256 of the arguments, and expires after one hour (`lifecycle.go:276`). The store file is private and locked, but the code says file mode alone does not isolate same-user processes (`store.go:15-19`). Dispatch states that agent tools must not reach the store or impersonate the reviewer (`dispatch.go:372-377`). The gateway's `serve` reads the whole body and the tool name before proxying (`gateway.go:105-160`). The ops server runs tools under one mutex (`server.go:233-236`) and its `approvals` tool is a placeholder (`tools.go:57`). The plugin answers prompts only for Hermes approval cards, in memory, keyed by Hermes request id (`adapter.py:81,143`), with at most 256 waiting prompts (`approvals.py:206`).

## 1. Which calls are approval-tier, and where the list lives

Today no ops call is approval-tier: `doctor` and `sync_status` are read-only and the other three tools are not built (`docs/internals/mcp-ops.md`). On the gateway, the approval-tier calls are the non-read tools of the keyed and calendar servers: Linear writes (`server.jsonc:21` asks for `linear_*` except get/list/search) and calendar create, move and delete (`server.jsonc:116-118`). GitHub does not pass through the gateway.

Recommendation: the list is data in the env fragment, next to the tiers it already holds, and `bud sync` renders it into the gateway config as a per-server `approval` block of tool-name globs marked `allow` or `ask`, with a default of `ask`. A tool the list does not name asks, so a new upstream tool fails closed. The gateway matches the upstream's own tool names (`save_issue`), not the runtime-prefixed ones (`linear_save_issue`); the renderer strips the prefix. Ops declares the tier in code on each `Tool`, because ops tools are Bud's own and their effect is known at compile time.

Alternative: a hard-coded read-only allowlist in Go. It cannot drift from the config, but a new Linear read tool then asks until a release ships, and the list lives away from every other tier.

## 2. The review channel between Go and the plugin

Recommendation: the plugin listens on a unix socket in `$XDG_RUNTIME_DIR/bud/` (directory 0700, socket 0600) and speaks newline-delimited JSON. The Go side dials, sends one line `{id, summary, expires_at, secret}`, and holds the connection open; the plugin answers with one line `{id, choice}` when Jason replies. Closing the connection withdraws the prompt. This is exactly the shape of `Review` (blocks, honors ctx, returns one `Reply`), needs only stdlib asyncio in the plugin, and runs Python and Go in one reply path with no polling. `Reply.Scope` is filled by the Go adapter from the scope it assigned, and only after the plugin's authenticated line for that id arrives, so reply contents never choose the scope (`dispatch.go:367-369`). The plugin answers only for senders in `SIGNAL_ALLOWED_USERS`, as today.

Authentication: the socket permissions are the boundary, plus a 32-byte secret file (0600) checked on every request. Both processes run as one user, so the secret is defense in depth and not a boundary: any same-user process that can read the file can ask. The real boundary is that agent tools cannot reach the socket or the files. Hermes's terminal runs in Docker without the host's filesystem or loopback (`docs/internals/signal.md`), so it is outside. Open question 1 covers the planning OpenCode on the same host.

Alternative: the gateway writes the pending request to the shared store and the plugin watches it. It needs no new channel, but it makes the plugin a store writer: it must reimplement the flock and JSON format in Python, and `Dispatch` itself decides and consumes the request (`dispatch.go:438,446`), so Dispatch would have to change. It also puts the reviewer's credential in the store file that the doctor and ops will read. Not recommended.

## 3. What the calling runtime sees while waiting

Recommendation: hold the MCP call open. `Dispatch` blocks in `Review`, and if it returns early its deferred cancel marks the request canceled (`dispatch.go:416-420`), so an immediate "pending, retry" result does not fit the current store. The gateway sets no write timeout (`gateway.go:327`), so a long hold is possible. If the client gives up, its context ends, Dispatch cancels the request, the plugin withdraws the prompt, and a late thumbs-up gets "no approval is waiting". Nothing executes. The one-hour expiry stays the upper bound; the real wait is whatever the client tolerates.

Alternative, only if measurement shows clients give up in a minute or two: a two-phase flow. The first call returns an immediate "approval pending, ask again" result; Jason approves; the retry finds an approved request by its binding and consumes it. That needs a new store method that looks up an approved request by scope, action, target and digest, and it lets a retry with changed arguments through only as a new request, so the binding is unchanged. It is a larger change to `internal/approval`, so defer it.

Ops must run the review outside its mutex, or one waiting approval would block `doctor` for an hour.

## 4. Binding the final call

Action is `mcp:<server>/<tool>`. Target is the server name plus the upstream URL with no key, since the same tool on another upstream is a different effect. Arguments are the exact `params.arguments` bytes from the request, and `execute` forwards the body already read at `gateway.go:105`, unchanged, so the digest covers what is sent. The gateway's own rewrite (headers and key, `gateway.go:91-102`) does not change the effect. A JSON-RPC batch that contains an approval-tier call is refused whole.

Scope has five fields (`dispatch.go:332-338`): Runtime `mcp-gateway` or `mcp-ops`, Profile the privacy tier, Surface the server name, Actor the client address, and Session the `Mcp-Session-Id` header when present, else the client address and tier. A retry on a new MCP session therefore asks again. Because Hermes calls ops directly on loopback and devices come through the gateway, the gateway does not review `ops` calls; ops enforces at its own leaf, so there is no double prompt and no bypass.

## 5. Failure behavior

Every failure denies, and no byte goes upstream. Plugin down or socket missing: the dial fails at once, the call returns a refusal naming that reviews are unavailable, no one-hour wait. Plugin dies mid-wait or restarts: the connection closes, Dispatch cancels the request. Signal down: the plugin cannot send, replies with an error, and the call is refused; if the send is accepted but never delivered, the request expires after one hour and is denied. Store unwritable, full or corrupt: `Create` fails (`dispatch.go:412`, `store.go:151`) and the call is refused. Gateway crash: a pending record remains until it expires; nothing can consume it because approval and consumption are one transaction (`store.go:199-221`). The refusal is a normal tool result with `isError: true` and the reason (denied, expired, unavailable), so the model can tell Jason; the audit log gains these decisions.

## 6. Slice plan

1. Reviewer client and paths. `internal/approval` gets a socket `Review`, the protocol, and `paths` entries for the store, socket and secret. Tests: fake reviewer server for allow, deny, close, id mismatch, bad secret, oversized line, context end; one Dispatch run end to end.
2. Plugin reviewer server in bud-signal. Socket and secret handling, a separate prompt registry that shares the four-digit code space, reaction and code answers, withdrawal on close, one-hour timeout. Tests: stdlib unittest in the existing plugin suite; docs in `signal.md`. Independent: nothing calls it yet.
3. Gateway enforcement, opt-in per server. `approval` config block, tier matching, hold and forward, refusal results, audit decisions. Tests: reads pass unchanged; write holds, approves and forwards identical body bytes; deny, expiry, plugin down and store failure send nothing upstream; client disconnect cancels; batch refused.
4. Ops leaf enforcement. A tier field on `Tool`, review outside the mutex, the gateway skipping `ops`. Tests with a fake write tool; no shipped tool changes.
5. Rendering and cutover. The env fragment's tier list rendered by `bud sync`; a missing `approval` block becomes a config error for any non-read-only server; a doctor check for the socket; the `approvals` ops tool lists pending counts without ids; update `suite.md` ("none of it is wired yet"). Tests: renderer, config validation, doctor.

## Open questions

1. (Recommended, not yet confirmed by Jason: run the planning OpenCode as its own system user, so it can't open the socket or read the secret.) Does the planning OpenCode on the home server run as the same user on the host, where it could reach the socket and secret? If so, the secret is not enough and the socket needs another boundary.
2. What do the actual MCP clients (Hermes, OpenCode) allow for a tool call's wait, and do they reset on progress notifications? This decides whether hold-open is enough (slice 3 measures it).
3. How does ops learn the device session when the gateway proxies to it? The gateway strips headers today; a new loopback-only header is possible but unproven.
4. ~~Which prompt does Hermes's own untrusted-server ask (`server.jsonc:114-118`) add on top, and should those servers become trusted once the gateway asks?~~ Answered 2026-11-22 (Suite D115, JGA-378): it asked a second time for every gateway write, so gateway servers with an approval block are now rendered `trust: full` in Hermes, and `bud doctor` fails when the gateway doesn't review one. GitHub stays untrusted.
5. Is `XDG_RUNTIME_DIR` set for the Hermes gateway and the gateway unit?
6. Pre-approval of a gateway call: the plugin's current parser reads shell commands only. Out of scope until Jason names the message form (for example `run: linear save_issue`).
7. Does Hermes 0.23.2 let the plugin start a listener in the adapter's loop without a private API? The pin stays at 0.23.2.

## Decisions (approved 2026-11-21)

1. Approval-tier calls are listed in the env fragment per server, default `ask`, rendered into the gateway config; ops declares its tier in code.
2. The review channel is a unix socket in the runtime dir carrying newline-delimited JSON, with a secret file as defense in depth.
3. The gateway holds approval-tier MCP calls open; a two-phase retry waits for evidence that clients time out.
4. Binding is `mcp:<server>/<tool>`, server plus upstream URL, exact argument bytes, and the five-field scope above; batches with an approval-tier call are refused.
5. The gateway does not review `ops`; ops enforces at its own leaf, outside its mutex.
6. Every failure denies and returns an `isError` result; the audit log records the decision.
7. Build in the five slices above, in order.
