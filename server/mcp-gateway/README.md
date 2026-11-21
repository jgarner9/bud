# server/mcp-gateway/

The home server's MCP gateway (Suite D72): `bud mcp-gateway` behind a systemd user unit. Devices reach Linear, Exa and later keyed MCP servers through it, so laptops hold no MCP key, every tool call lands in the audit log, and P0 sessions are refused cloud tools (Suite D81). How it works is in [docs/internals/mcp-gateway.md](https://github.com/jgarner9/bud/blob/main/docs/internals/mcp-gateway.md).

| File | What it is |
| --- | --- |
| `bud-mcp-gateway.service` | The user unit. It runs the gateway under `op run`, so keys reach it as environment variables. |
| `mcp-gateway.example.json` | The config, with loopback in place of the tailnet address. |
| `mcp-gateway.env.example` | The `op://` references `op run` resolves, with placeholder vault paths. |

## Setting it up

The real config and env file come from the overlay, since they name the tailnet address and the Bud vault.

1. Run `bud sync` first: the gateway reads its privacy rules (Suite D112) from the synced registry and won't start without one.
1. Put the config at `~/.local/state/bud/mcp-gateway.json`, with `listen` set to the home server's tailnet IP (`tailscale ip -4`) and a port.
2. Put the env file at `~/.config/bud-server/mcp-gateway.env`, and the service account's `OP_SERVICE_ACCOUNT_TOKEN=...` at `~/.config/bud-server/op-service-account.env`, both mode 0600.
3. Install and start the unit with the commands at the top of `bud-mcp-gateway.service`, then `loginctl enable-linger` so it runs without a login.
4. Check it: `curl -s -H 'X-Bud-Privacy: P1' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' http://<listen>/linear/mcp` lists Linear's tools (after an `initialize`, for a server that wants a session first), and the same request with `X-Bud-Privacy: P0` is refused.

Devices then use `http://<listen>/<server>/mcp` as a remote MCP server with the `X-Bud-Privacy` header set to the session's tier. Rendering that into each device's OpenCode config is JGA-358.
