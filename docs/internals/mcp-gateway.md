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
| `.key` | Optional. The header the upstream reads its key from, a prefix such as `Bearer `, and the environment variable `op run` fills. A missing or empty variable stops the gateway at start, naming it. |

The gateway refuses a config with an unknown tier, a bad name, a wildcard listen address or a P0 cloud server, and says which field to fix.

## A request

1. **Who's asking.** The peer's address must be loopback or tailnet, or it gets 403. A request carrying an `Origin` header, or whose `Host` isn't a loopback or tailnet IP address (or `localhost`), gets 403 too. MCP clients send neither, while a page that rebinds its own domain to the gateway's address (DNS rebinding) sends that domain in `Host`, and a browser's same-origin GET can leave `Origin` out. So devices reach the gateway by IP, not by a MagicDNS name.
2. **Which tier.** The session's tier is the `X-Bud-Privacy` header (`P0`, `P1` or `P2`). No header means P0, so a client that wasn't configured fails closed. Another value gets 400. The header is a guard against private text leaking by accident, not authentication: the tailnet is the authentication.
3. **The P0 tool rule.** When the server's `privacy` doesn't list the session's tier, every request to it is refused: 403 with a JSON-RPC error (`-32001`) for each request in the body, and an audit entry for each. No byte goes upstream, so a P0 session can't even list the tools.
4. **Forwarding.** The body (up to 4 MiB) is read and parsed as JSON-RPC, one message or a batch, then sent to the upstream URL with the method, `Mcp-Session-Id` and `Accept` kept. The client's `Authorization`, `Cookie`, `X-Bud-Privacy` and the key header are dropped, and the key header is set from the environment. Responses stream back as they arrive, so server-sent events work. `GET` (the event stream) and `DELETE` (ending a session) are forwarded the same way.
5. **The audit log.** Each `tools/call` becomes one JSON line once the upstream's response ends (for an event stream, when the stream closes; a client that leaves mid-stream still gets its record): time, server, tier, client address, tool, the argument names (sorted; the values are left out, since they can hold private text or keys), the decision (`allowed` or `refused`), the HTTP status and the milliseconds taken. A refused request that isn't a tool call is logged with its method and no tool. Other allowed methods (`initialize`, `tools/list`) aren't logged. The file is appended to, and is set to mode 0600 at start even if it already existed. The status is the final one, never an interim 1xx.

A JSON-RPC error inside a 200 response (the tool itself failing) isn't read back, so the log says the call went upstream, not that it worked.

## Not yet

- The audit log is a JSONL file until the one approval and audit log (JGA-348) exists; its writer then takes these records.
- The gateway doesn't share a listener with the model gateway (D70) or the memory server (D51), since neither is built yet.
- Rendering each device's MCP config to point at the gateway, with its `X-Bud-Privacy` header, is JGA-358.
