# server/memory-mcp/

The home server's memory MCP server (Suite D102, D108): `bud memory-mcp` behind a systemd user unit. It is how every runtime (Hermes, the planning OpenCode, a laptop's OpenCode) reaches Bud's working memory, through the MCP gateway, so none keeps a memory store of its own (JGA-313). It searches and stages records in the memory service (`server/memory/`) and embeds them on the embedding server (`server/embed/`). How it works is in [docs/internals/memory-mcp.md](https://github.com/jgarner9/bud/blob/main/docs/internals/memory-mcp.md).

| File | What it is |
| --- | --- |
| `bud-memory-mcp.service` | The user unit. It runs the server under `op run` on `127.0.0.1:8798`, so the tokens reach it as environment variables. |
| `memory-mcp.env.example` | The five `op://` references `op run` resolves, with placeholder vault paths. |

## Setting it up

The memory service and the embedding server come first: `server/memory/README.md` and `server/embed/README.md`.

1. Make five tokens of at least 32 random characters each (`openssl rand -hex 32`) and keep them in a Bud vault item, one field each. Add each to the memory service's `BUD_MEMORY_CLIENTS` policy with these tiers and actions, next to the reviewer's own token, which stays with `bud memory-review` (`server/memory-review/`):

   ```json
   {
     "<p0 token>":    { "tiers": ["P0", "P1", "P2"], "actions": ["search", "retrieve"] },
     "<p1 token>":    { "tiers": ["P1", "P2"],       "actions": ["search", "retrieve"] },
     "<p2 token>":    { "tiers": ["P2"],             "actions": ["search", "retrieve"] },
     "<stage token>": { "tiers": ["P0", "P1", "P2"], "actions": ["stage"] },
     "<embed token>": { "tiers": ["P0", "P1", "P2"], "actions": ["vector_pending", "vector_put"] }
   }
   ```

   Restart the memory service so it reads the new policy.
2. Put the env file at `~/.config/bud-server/memory-mcp.env`, mode 0600, with the item's `op://` paths. The service account's token file is the one the gateway already uses.
3. Run `bud sync`, so memory-mcp can read the registry's embedding model and endpoint.
4. Install and start the unit with the commands at the top of `bud-memory-mcp.service`. Run it as the user the memory service runs as, since the service's socket is mode 0600.
5. Add the `memory` entry from `server/mcp-gateway/mcp-gateway.example.json` to the gateway's config, and restart the gateway. In the overlay's env fragment, set `mcp.memory` to `"gateway": true`, in place of any `url` it had, and run `bud sync` on each device, so the runtimes reach memory through the gateway with their own tier.
6. Check it through the gateway: stage a record with `memory_stage` from a P1 session (`X-Bud-Privacy: P1`), approve it on Signal when `bud memory-review` asks (`server/memory-review/`), and wait a minute for it to be embedded (the unit's log says `embedded 1 records`). Then `memory_search` finds it from a P0 or P1 session, matched by vector (and by text when the words match), and a P2 session doesn't see it.
