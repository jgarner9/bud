# R4: `bud memory-mcp` design (JGA-313, Suite D102)

Status: decided 2026-11-21. Q1 was answered A (Suite D108); everything else follows decisions already made.

## What it is

`bud memory-mcp` is an MCP server, in the one `bud` binary (stdlib only, D1), that runs on the home server behind the MCP gateway. It is how every runtime reaches Bud's working memory: Hermes, the planning OpenCode and the laptop's OpenCode all call the same tools through the gateway, so no runtime keeps its own durable memory store (JGA-313 "no competing MEMORY.md").

It is a thin client of the two services that already exist:

- the memory service (`server/memory/`, Python, Unix socket, bearer tokens with server-owned tier and action policies), which owns SQLite, FTS5, sqlite-vec, staging and review;
- the embedding server (`server/embed/`, PR #86), which turns text into 1,024-number vectors on the home server's CPU.

## Tools

| Tool | What it does | Gateway tier |
| --- | --- | --- |
| `memory_search` | `query`, optional `source`, `since`, `until`, `limit` (1-20, default 8). Embeds the query, then calls `/retrieve` (hybrid text and vector). It returns each record's body with its provenance: source, privacy, created time and how it matched. | allow |
| `memory_stage` | `kind` (observation, fact or summary, the memory service's kinds), `body`, optional `privacy`. Stages a record for review. Nothing is searchable until the reviewer approves it, so this is safe to run unattended. Retries reuse a deterministic dedupe key, so they don't duplicate (JGA-313 "retries do not duplicate"). | allow |

There is no approve, delete or export tool. Approving stays with the reviewer identity (the bud-signal flow), and KB writes stay with the curator and Jason.

## Embedding new records

memory-mcp also runs a small loop. Every minute, and once at start, it asks `/vector_pending` (PR #87) for the registry's embedding model, embeds what comes back in batches of 16, and stores each vector with `/vector_put`. Newly approved records become vector-searchable within about a minute, and a model change re-embeds everything with no separate job. While the embedding server is down, the loop backs off and search falls back to text only. Each result then says it matched by text only, and `bud doctor --net` already warns.

## Privacy

Memory holds P0 text, so the tier of the calling session decides what it may see:

| Session tier | Records it may see | Records it may stage |
| --- | --- | --- |
| P0 (local model) | P0, P1, P2 | P0 (default), P1, P2 |
| P1 (zero-retention cloud) | P1, P2 | P1 (default), P0 |
| P2 (public) | P2 | P2 (default), P1, P0 |

A record may be staged as more private than its session, never less. Search filtering happens in the memory service, by the token's policy, before anything leaves it (JGA-310 "filters enforced before results can enter a cloud-bound prompt").

memory-mcp holds one search token per session tier, one stager token (`stage`, all tiers) and one embedder token (`vector_pending`, `vector_put`, all tiers). It picks the search token by the session's tier. Staging uses the stager token, because the service refuses a record outside the token's tiers; memory-mcp first checks that the requested privacy is at least as private as the session's. All of the tokens come from 1Password through `op run`, like the other units.

### Q1: how memory-mcp learns the session's tier (decided: A, Suite D108)

The gateway checks `X-Bud-Privacy` and then strips it before forwarding, so today an upstream never learns the tier.

- **A. The gateway forwards the tier it checked (recommended).** For upstreams on loopback or the tailnet only, the gateway sets `X-Bud-Privacy` to the tier it already checked. Cloud upstreams still never see it. That's one memory-mcp instance and one gateway entry (`memory`, privacy P0 to P2). The trust model is the same as the router's (D100): the header guards against leaks, and the tailnet is the authentication.
- **B. One instance per tier.** Three gateway entries (`memory-p0`, `memory-p1`, `memory-p2`), each allowed one tier, each a memory-mcp started with that tier's token. This needs no gateway change, but it means three units and three device MCP entries, and a client set up with the wrong one sees the wrong tier.

## Disconnected memory

When the socket is missing or the service refuses, each tool returns an MCP error result (`isError: true`) that names the problem and what to do. For example: "Bud memory is unavailable (the memory service isn't running on the home server); nothing was saved, so stage it again later." A runtime never falls back to writing its own file. Hermes's built-in memory provider stays off (D48), and OpenCode has none.

## Wiring

- `server/memory-mcp/`: a user unit on a loopback port (8793), under `op run` with the five tokens.
- The gateway config gets a `memory` server (loopback upstream, read_only false, approval globs `memory_search`: allow, `memory_stage`: allow).
- Hermes and the planning OpenCode reach it through the gateway like the other servers. JGA-358 renders the device entries.

## Slices (one PR each)

1. The memory-service client in Go (`internal/memory`): Unix-socket HTTP with a bearer token, and `retrieve`, `stage`, `vector_pending` and `vector_put`, tested against a fake socket server.
2. `bud memory-mcp`: the MCP surface, the two tools, the tier-to-token mapping and the error results.
3. The embed loop.
4. The unit, the gateway entry, docs (`docs/internals/memory-mcp.md`) and, for Q1=A, the gateway change.
5. The cross-runtime check on the home server: stage from Hermes, approve, find it from OpenCode, with provenance intact (JGA-313 validation). This needs Jason for the approval and the install.

## Not in scope

The curator promoting approved records to the KB (JGA-311), measuring tokens (JGA-295), and an in-session cache. JGA-310's gate is 300 ms p95; with a measured 29 ms query embedding, nothing points to needing a cache.
