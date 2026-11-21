# The MCP gateway

`bud mcp-gateway` (`internal/mcpgateway`) is the home server's MCP gateway (Suite D72). Devices reach the keyed MCP servers (Linear, Exa) and the Bud ops MCP (`docs/internals/mcp-ops.md`) only through it, so a laptop holds no MCP key. It does four things: it listens on the tailnet only, forwards each MCP request to its upstream server with that server's key added, writes every tool call to an audit log, and refuses cloud tools to P0 sessions (the P0 tool rule, Suite D81). It runs as the user unit in `server/mcp-gateway/`, under `op run`, so the keys reach it as environment variables and never sit in a file (Suite D7, D73).

## Config

One JSON file (D2), by default `~/.local/state/bud/mcp-gateway.json` (`paths.MCPGateway`), or `--config <file>`. The real one lives in the overlay, since the listen address is a tailnet address; `server/mcp-gateway/mcp-gateway.example.json` is the public example.

```json
{
  "listen": "127.0.0.1:8790",
  "audit_log": "",
  "servers": {
    "linear": {
      "url": "https://mcp.linear.app/mcp",
      "privacy": ["P1", "P2"],
      "key": { "header": "Authorization", "prefix": "Bearer ", "env": "LINEAR_API_KEY" }
    }
  }
}
```

| Key | What it is |
| --- | --- |
| `listen` | `host:port`. The host must be a loopback address or a tailnet address (100.64.0.0/10, fd7a:115c:a1e0::/48), never a wildcard or a LAN address. |
| `audit_log` | Where the audit log goes. Empty means `~/.local/state/bud/mcp-audit.jsonl` (`paths.MCPAudit`). |
| `servers.<name>` | One upstream server. Devices reach it at `http://<listen>/<name>/mcp`. Names are lowercase letters, digits and `-`. |
| `.url` | The upstream's Streamable HTTP endpoint. `https`, or `http` to a loopback or tailnet host. |
| `.privacy` | The session tiers that may use it. A server outside the house (any host that isn't loopback or tailnet) may not list `P0` (D81). |
| `.approval` | Turns on review for this server's approval-tier tools; see "Approvals". Required unless the server is `read_only` or is `ops`. |
| `.read_only` | Optional. Says every tool on the server only reads, so it needs no `approval` block. |
| `.key` | Optional. The header the upstream reads its key from, a prefix such as `Bearer `, and the environment variable `op run` fills. A missing or empty variable stops the gateway at start, naming it. |

The gateway refuses a config with an unknown tier, a bad name, a wildcard listen address or a P0 cloud server, and says which field to fix.

## A request

1. **Who's asking.** The peer's address must be loopback or tailnet, or it gets 403. A request carrying an `Origin` header, or whose `Host` isn't a loopback or tailnet IP address (or `localhost`), gets 403 too. MCP clients send neither, while a page that rebinds its own domain to the gateway's address (DNS rebinding) sends that domain in `Host`, and a browser's same-origin GET can leave `Origin` out. So devices reach the gateway by IP, not by a MagicDNS name.
2. **Which tier.** The session's tier is the `X-Bud-Privacy` header (`P0`, `P1` or `P2`). No header means P0, so a client that wasn't configured fails closed. Another value gets 400. The header is a guard against private text leaking by accident, not authentication: the tailnet is the authentication.
3. **The P0 tool rule.** When the server's `privacy` doesn't list the session's tier, every request to it is refused: 403 with a JSON-RPC error (`-32001`) for each request in the body, and an audit entry for each. No byte goes upstream, so a P0 session can't even list the tools.
4. **Forwarding.** The body (up to 4 MiB) is read and parsed as JSON-RPC, one message or a batch, then sent to the upstream URL with the method, `Mcp-Session-Id` and `Accept` kept. The client's `Authorization`, `Cookie`, `X-Bud-Privacy` and the key header are dropped, and the key header is set from the environment. Responses stream back as they arrive, so server-sent events work. `GET` (the event stream) and `DELETE` (ending a session) are forwarded the same way.
5. **The audit log.** Each `tools/call` becomes one JSON line once the upstream's response ends (for an event stream, when the stream closes; a client that leaves mid-stream still gets its record): time, server, tier, client address, tool, the argument names (sorted; the values are left out, since they can hold private text or keys), the decision (`allowed` or `refused`), the HTTP status and the milliseconds taken. A refused request that isn't a tool call is logged with its method and no tool. Other allowed methods (`initialize`, `tools/list`) aren't logged. The file is appended to, and is set to mode 0600 at start even if it already existed. The status is the final one, never an interim 1xx.

A JSON-RPC error inside a 200 response (the tool itself failing) isn't read back, so the log says the call went upstream, not that it worked.

## Approvals

A server with an `approval` block holds its approval-tier `tools/call` requests for review (Suite D97, D103; design in the r6b approvals note). The block is required (Suite D103): the gateway refuses to load a config with a server that has neither `approval` nor `read_only: true`, except `ops`, naming the server and saying to add its block to the env fragment and run `bud sync`. A `read_only` server passes through as above.

The tiers come from the env fragment's `bud.mcp.<name>.approval` (and `read_only`). `bud sync` on the server profile renders them to `mcp-gateway-approvals.json` (`paths.MCPGatewayApprovals`) beside the config, and `Read` lays that file over every server in the config that sets neither itself; entries for servers the config lacks are ignored, and a missing file changes nothing. The public `content/env/server.jsonc` tiers Linear (reads allow) the calendar (`list_*` allow; create, move and delete ask) and mail (reads, moves to the trash, restores and the digest allow; sends and replies ask, D107). After updating bud, an existing home server must run `bud sync` (or add the blocks to its `mcp-gateway.json` by hand) before restarting the gateway, or it refuses to start; any other gateway server the overlay adds needs its own `approval` or `read_only` in the overlay's env.

```json
"approval": { "tools": { "get_*": "allow", "list_*": "allow", "search_*": "allow" } }
```

`tools` maps upstream tool-name globs (`path.Match`, against the upstream's own names such as `save_issue`) to `allow` or `ask`. A tool no glob names asks, so a new upstream tool fails closed; when several globs match, `ask` wins. A bad glob or a value other than `allow` or `ask` fails validation.

An asking call goes through `approval.Store.Dispatch` with `approval.SocketReview` (`paths.ReviewSocket`, `paths.ReviewSecret`, the store at `paths.ApprovalStore`), and the HTTP request stays open until Jason answers. The binding is:

- Action `mcp:<server>/<tool>`; target the server name and its upstream URL as scheme://host/path only (no userinfo, query or fragment). Config validation refuses a query string on a server with an `approval` block; put credentials in the `key` header.
- Arguments: the exact bytes of `params.arguments`. On approval the gateway forwards the body it already read, byte for byte, so the digest covers what is sent.
- Scope: runtime `mcp-gateway`, profile the privacy tier, surface the server name, actor the client address, session the `Mcp-Session-Id` header, else the client address and tier. A retry on a new MCP session asks again.

Every failure denies and sends nothing upstream: a denial, expiry (one hour at most), a reviewer that is down or refuses, a reply that doesn't match, or a store that can't be written. The client gets HTTP 200 with a JSON-RPC result whose `isError` is true and a short reason, so the model can tell the user. A client that disconnects cancels the review and gets nothing. A batch holding an approval-tier call is refused whole, with an `isError` result per request. The gate reads raw JSON, and fails closed on an approval-enabled server: a message it can't parse, or a `tools/call` whose params or name can't be read or whose `arguments` is present and not an object, is refused with an `isError` result and audited as `refused`. Reads and `allow` tools pass through byte for byte.

The audit log records the decision for each asking call: `approved` (forwarded; the status is the upstream's), `denied`, `expired`, `unavailable`, `failed`, `canceled` (status 499, client left) or `refused` (batch). Argument values are never logged. Whether MCP clients tolerate a long hold is still unmeasured (design open question 2).

The `ops` server is the exception: `approval` on a server named `ops` is a config error. Ops enforces approvals at its own leaf, outside its mutex (see `mcp-ops.md`), so a gateway review as well would ask twice. Calls to `ops` pass through unreviewed here.

## Not yet

- The audit log is a JSONL file until the one approval and audit log (JGA-348) exists; its writer then takes these records.
- The gateway doesn't share a listener with the model gateway (D70) or the memory server (D51), since neither is built yet.
- Rendering each device's MCP config to point at the gateway, with its `X-Bud-Privacy` header, is JGA-358.
