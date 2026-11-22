# The Bud ops MCP server

`bud mcp-ops` (`internal/mcpops`) is a small MCP server that reports on Bud itself (Suite D83, JGA-361). Hermes uses it to answer "is everything healthy" and "what's waiting on me", and the self-improvement loop (D47) uses it to see what its changes did. It runs on the home server as the user unit in `server/mcp-ops/`, listening on loopback only. Hermes on the server reaches it directly, and devices reach it through the MCP gateway (D72) as the `ops` server.

Every tool is read-only and wraps code bud already has. The only thing it touches on disk is the sync lock file, which `bud doctor` takes too, so a reading never lands halfway through a sync.

## Tools

| Tool | Arguments | What it returns |
| --- | --- | --- |
| `doctor` | none | `bud doctor --quick` as JSON: `failed` and `warnings` counts, then each result's `name`, `status` (`ok`, `warn`, `fail`), `detail` and `fix`. It never runs the network checks (`--net`): the server can't see a session's tier, since the gateway strips `X-Bud-Privacy`, and P0 sessions may call it, so nothing it does may leave the house (D81). The slow startup timing is skipped too. |
| `sync_status` | none | What the sync ledger says this device has: `device`, `channel`, `content` (the version in use), `current` (where the `content/current` link points), `overlay` (repo and commit), `rolled_back`, and `drift`, each file bud wrote whose bytes changed (`changed`) or that's gone (`missing`). `synced` is false on a device `bud sync` never ran on. A sync running right now is an error saying to ask again. |
| `spend` | none | This month's spend as `bud router` counted it (`docs/internals/router.md`): `month` (UTC, as `2026-11`), `topup` (the month's top-up dollars), `topup_cap` and `topup_left` (the cap of the router that last wrote the ledger, and what's left under it), and `models`, each model with spend this month by id, with its `cost` class and, for a C1 model with an allowance, `allowance` and `allowance_left`. Dollars are rounded to the cent. It reads the default ledger, `router-spend.json` in bud's state dir, with `router.ReadSpend`, which writes nothing and reads a file from an earlier month as an empty month, as the router would. Cost classes and allowances come from the synced registry (the one the router loads by default); when it can't be loaded they are left out and `note` says why. `note` also says when the router hasn't charged anything yet, or when the ledger doesn't hold the cap yet (a router that predates it, until its next charge). A router run with `--spend` elsewhere isn't seen. |
| `approvals` | none | The count of approval requests still waiting, `pending` in total and `by_action` (such as `mcp:linear/save_issue`). It reads `paths.ApprovalStore` without the lock, writes nothing and creates nothing, and leaves out ids, targets and arguments, so it can't approve or replay anything. An unwritten store is zero. Expired and decided requests aren't counted. |
| `reviews` | none | The memory records waiting on Jason's review, as `bud memory-review` last counted them (`docs/internals/memory-review.md`): `memory` holds `pending`, `by_privacy`, `asking` (on his phone now), `too_long` (to approve by hand), `oldest` and `counted_at`, read from `memory-review.json` in bud's state dir, so ops holds no memory token and sees no bodies. `memory` is null and `note` says so when memory-review never counted; `note` also says when the count is over five minutes old (memory-review may not be running) or some are too long. `skills` says Hermes's own staged skill writes aren't counted. |

`reviews` counts from a file rather than asking the memory service, so ops keeps running without `op run` and holds no token that could read staged bodies (Suite D113).

Tool calls run one at a time, since `doctor` and `sync_status` both take the sync lock and would otherwise see each other as a running sync.

## Approval tier

Each `Tool` has a `Tier`: `ReadOnly` (the zero value) or `ApprovalTier` (Suite D97, D103); any other value is refused, so an unknown tier fails closed. No shipped tool is approval-tier yet; tests use a fake write tool. A read-only tool runs at once under the mutex. An approval-tier tool goes through `approval.Store.Dispatch` with `approval.SocketReview` (`paths.ApprovalStore`, `ReviewSocket`, `ReviewSecret`, set by `SetApprovals` in `bud mcp-ops`), and the HTTP request stays open until Jason answers. The review runs with no lock held, so a waiting approval doesn't block `doctor`; the mutex is taken only around the tool's own effect. Once approved, the effect waits for the mutex and then checks the request context again, so an approval that expires or a request that ends while it waits never runs the effect.

The binding is action `mcp:ops/<tool>`, target `ops` (its name in the gateway config), and the exact `params.arguments` bytes. The scope is fixed: runtime `mcp-ops`, profile `unknown`, surface `ops`, actor and session `loopback`. Ops can't learn the calling device's session or privacy tier, because the gateway strips them (r6b design, open question 3), so every ops call shares one scope until a loopback-only header exists. The tool's effect runs only on an allow; deny, expiry, an unavailable reviewer, a store failure, or no approvals wiring all return an `isError` result with a short reason and run nothing. `tools/list` marks such tools `readOnlyHint: false`.

The gateway doesn't review `ops`: its config refuses an `approval` block on a server named `ops`, so a call is reviewed once, here.

## The protocol

MCP over Streamable HTTP, the stateless subset: `POST /mcp` with one JSON-RPC message or a batch, answered with `application/json` (never an event stream), and no `Mcp-Session-Id`. It answers `initialize` (protocol version `2025-06-18`, or the client's own when it's `2025-03-26` or `2025-11-25`), `ping`, `tools/list` and `tools/call`. Notifications (a 2.0 message with a method and no `id`) and responses get 202 with no body; anything else without an `id`, such as `{}`, is `-32600` with a null `id`. An unknown method is JSON-RPC error `-32601`, an unknown tool or bad arguments `-32602`, and a body that isn't JSON `-32700` with HTTP 400. `GET` and `DELETE` get 405, since there's no event stream or session to end. A failing check is a result, not an error; a tool that couldn't run returns `isError: true` with the reason.

## Who it serves

- `--listen` (default `127.0.0.1:8792`) must be a loopback IP address and port; anything else stops it at start. Devices come in through the gateway, which holds the tailnet side.
- A request whose peer isn't loopback, whose `Host` isn't a loopback IP or `localhost`, or that carries an `Origin` header gets 403, the gateway's guard against DNS rebinding.
- In the gateway's config it's `"ops": {"url": "http://127.0.0.1:8792/mcp", "privacy": ["P0", "P1", "P2"]}`. It's allowed P0 because its answers never leave the house: the P0 tool rule (D81) is about cloud servers, and this one is on loopback. The content env lists it the same way: through the gateway on `dev-machine`, at the loopback URL on `server`, and its tools run alone.
