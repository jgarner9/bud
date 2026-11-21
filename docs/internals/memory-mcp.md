# The memory MCP server

`bud memory-mcp` (`internal/memorymcp`, with the client in `internal/memory`) is how every runtime reaches Bud's working memory (Suite D102). It runs on the home server behind the MCP gateway, as the unit in `server/memory-mcp/`, and is a thin layer over two services that own the real work: the memory service (`server/memory/`, Python, SQLite with FTS5 and sqlite-vec, staging and review) and the embedding server (`server/embed/`, Qwen3-Embedding-0.6B on llama.cpp). Hermes, the planning OpenCode and a laptop's OpenCode all call the same two tools, so none keeps a durable memory store of its own (JGA-313). The design is in the project's `architecture/r4-memory-mcp-design.md`.

## Tools

| Tool | What it does |
| --- | --- |
| `memory_search` | `query` (up to 4,096 characters), optional `source`, `since` and `until` (inclusive `YYYY-MM-DD`) and `limit` (1 to 20, default 8). It embeds the query and calls the service's `/retrieve`, which ranks by text and vector together. Each result carries its id, source, kind, privacy, when it was saved, how it matched and its body. |
| `memory_stage` | `source` (the runtime saving it, lowercase letters, digits and `-`), `kind` (`observation`, `fact` or `summary`), `body` (up to 16,000 characters) and optional `privacy`. It stages the record for Jason's review; nothing is searchable until the reviewer approves it. The dedupe key is the SHA-256 of kind, privacy and body, so a retry finds the record it already made instead of staging it twice. |

There is no approve, edit, delete or export tool: approving stays with the reviewer identity, and the KB stays with the curator and Jason. A tool's failure is an MCP result with `isError: true` and a sentence the model can pass on, such as "Bud memory is unavailable (the memory service isn't running on the home server); nothing was saved, so save it again later". Runtimes never fall back to a file of their own.

## Privacy

The session's tier comes from `X-Bud-Privacy`, which the gateway sets to the tier it checked before forwarding to an upstream inside the house (Suite D108, `docs/internals/mcp-gateway.md`). No header is P0. The server listens on loopback only and refuses a request with an `Origin` header or a `Host` that isn't loopback, as the other Bud MCP servers do, so only the gateway (or something already on the machine) reaches it.

memory-mcp holds five memory-service tokens (`BUD_MEMORY_TOKEN_P0`, `_P1`, `_P2`, `_STAGE`, `_EMBED`), and the service holds each one's tiers and actions:

| Token | Tiers | Actions | Used for |
| --- | --- | --- | --- |
| P0 | P0, P1, P2 | search, retrieve | searches from P0 sessions |
| P1 | P1, P2 | search, retrieve | searches from P1 sessions |
| P2 | P2 | search, retrieve | searches from P2 sessions |
| STAGE | P0, P1, P2 | stage | every stage |
| EMBED | P0, P1, P2 | vector_pending, vector_put | the backfill |

A search uses the token of the session's tier, so the service, not memory-mcp, decides what comes back, and filters before anything leaves it. Staging uses one all-tier token, because the service refuses a record outside a token's tiers and a session may stage a record more private than itself: memory-mcp checks first that the requested privacy is the session's or more private (P0 is the most private), and refuses one less private, since a P0 session's text may hold private data.

## Embedding

The query embedding and the backfill use the registry's embedding role: its first model, which must be on a local provider and give `dims` (memory holds P0 text). `HTTPEmbedder` posts to the provider's `/embeddings` and refuses a reply with other than `dims` numbers per vector, or vectors that don't line up with the inputs.

When the embedding server is down, `memory_search` calls `/retrieve` with no model or vector, so the service ranks by words alone and still applies `source`, `since` and `until` before its limit, and adds a `note` saying it matched by words only.

`Backfill` runs once at start and then every minute (`--no-backfill` turns it off). It asks `/vector_pending` for the model, embeds 16 records at a time and stores each vector with `/vector_put`, passing back the record's `body_hash`. Each round walks forward by id (`after` is the last id it was given), so it ends once it passes the newest record. A record edited in between comes back as `ErrStale`; it stays pending and the next round picks up its new body, and however many records keep changing, they can't hold back the ones after them. A failure waits longer each time, up to ten minutes, so a down embedding server isn't hammered. A new model in the registry starts with every approved record pending, so this is also the re-embed path.

## Measured

On the cloud build machine (4 vCPUs), with the memory service, the embedding server and memory-mcp on one host: a `memory_search` through MCP took 36 to 41 ms end to end, including the query embedding. JGA-310's gate is 300 ms at p95. Re-measure on the home server.

## Not yet

- Hermes and OpenCode still have their own memory: Hermes's built-in `memory` toolset is on in `content/env/server.jsonc`, with writes staged. Turning it off in favour of these tools, and the cross-runtime check on the home server, are the rest of JGA-313.
- Devices reach memory through the gateway once the overlay sets `mcp.memory` to `"gateway": true`; the public fragments leave it unset, since the overlay knows the address.
