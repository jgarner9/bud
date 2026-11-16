# The Bud ops MCP server

`bud mcp-ops` (`internal/mcpops`) is a small MCP server that reports on Bud itself (Suite D83, JGA-361). Hermes uses it to answer "is everything healthy" and "what's waiting on me", and the self-improvement loop (D47) uses it to see what its changes did. It runs on the home server as the user unit in `server/mcp-ops/`, listening on loopback only. Hermes on the server reaches it directly, and devices reach it through the MCP gateway (D72) as the `ops` server.

Every tool is read-only and wraps code bud already has. The only thing it touches on disk is the sync lock file, which `bud doctor` takes too, so a reading never lands halfway through a sync.

## Tools

| Tool | Arguments | What it returns |
| --- | --- | --- |
| `doctor` | none | `bud doctor --quick` as JSON: `failed` and `warnings` counts, then each result's `name`, `status` (`ok`, `warn`, `fail`), `detail` and `fix`. It never runs the network checks (`--net`): the server can't see a session's tier, since the gateway strips `X-Bud-Privacy`, and P0 sessions may call it, so nothing it does may leave the house (D81). The slow startup timing is skipped too. |
| `sync_status` | none | What the sync ledger says this device has: `device`, `channel`, `content` (the version in use), `current` (where the `content/current` link points), `overlay` (repo and commit), `rolled_back`, and `drift`, each file bud wrote whose bytes changed (`changed`) or that's gone (`missing`). `synced` is false on a device `bud sync` never ran on. A sync running right now is an error saying to ask again. |
| `spend` | none | Not built yet: the error says router spend comes with the model gateway (build plan phase 3, D70). |
| `approvals` | none | Not built yet: the error says the approval queue comes with the approval service (phase 7) and the shared approval log (JGA-348). |
| `reviews` | none | Not built yet: the error says staged skill and memory writes come with Hermes (D48) and the memory curator (phase 5). |

The three that aren't built are listed anyway, with descriptions that say so, so a model asked "what's waiting on me" answers that the queues don't exist yet rather than that nothing is waiting. Each one becomes a wrapper when the code it reads lands.

Tool calls run one at a time, since `doctor` and `sync_status` both take the sync lock and would otherwise see each other as a running sync.

## The protocol

MCP over Streamable HTTP, the stateless subset: `POST /mcp` with one JSON-RPC message or a batch, answered with `application/json` (never an event stream), and no `Mcp-Session-Id`. It answers `initialize` (protocol version `2025-06-18`, or the client's own when it's `2025-03-26` or `2025-11-25`), `ping`, `tools/list` and `tools/call`. Notifications (a 2.0 message with a method and no `id`) and responses get 202 with no body; anything else without an `id`, such as `{}`, is `-32600` with a null `id`. An unknown method is JSON-RPC error `-32601`, an unknown tool or bad arguments `-32602`, and a body that isn't JSON `-32700` with HTTP 400. `GET` and `DELETE` get 405, since there's no event stream or session to end. A failing check is a result, not an error; a tool that couldn't run returns `isError: true` with the reason.

## Who it serves

- `--listen` (default `127.0.0.1:8792`) must be a loopback IP address and port; anything else stops it at start. Devices come in through the gateway, which holds the tailnet side.
- A request whose peer isn't loopback, whose `Host` isn't a loopback IP or `localhost`, or that carries an `Origin` header gets 403, the gateway's guard against DNS rebinding.
- In the gateway's config it's `"ops": {"url": "http://127.0.0.1:8792/mcp", "privacy": ["P0", "P1", "P2"]}`. It's allowed P0 because its answers never leave the house: the P0 tool rule (D81) is about cloud servers, and this one is on loopback. The content env lists it the same way: through the gateway on `dev-machine`, at the loopback URL on `server`, and its tools run alone.
