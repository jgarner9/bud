# server/mcp-ops/

The Bud ops MCP server (Suite D83): `bud mcp-ops` behind a systemd user unit. Its read-only tools report on Bud itself, `bud doctor` and what `bud sync` left on the home server, so Hermes can answer "is everything healthy" and, once the queues exist, "what's waiting on me". How it works is in [docs/internals/mcp-ops.md](https://github.com/jgarner9/bud/blob/main/docs/internals/mcp-ops.md).

| File | What it is |
| --- | --- |
| `bud-mcp-ops.service` | The user unit. It needs no keys, so there's no `op run` and no env file. |

## Setting it up

1. Install and start the unit with the commands at the top of `bud-mcp-ops.service`, then `loginctl enable-linger` if the gateway's setup hasn't already, so it runs without a login. It listens on `127.0.0.1:8792`; pass `--listen` in `ExecStart` for another loopback port.
2. Add it to the MCP gateway's config, so devices reach it: `"ops": {"url": "http://127.0.0.1:8792/mcp", "privacy": ["P0", "P1", "P2"]}` under `servers` (the example config has it), then restart `bud-mcp-gateway`.
3. Check it: `curl -s -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor","arguments":{}}}' http://127.0.0.1:8792/mcp` returns doctor's results.

`bud sync` renders it into Hermes's and the planning OpenCode's configs at the loopback URL, and into each dev machine's through the gateway.
