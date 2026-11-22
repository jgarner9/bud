---
name: mcp-placement-jga355
description: JGA-355 MCP server placement deliberation - Jason's choices so far and what is still pending
metadata:
  type: project
  modified: 2026-11-16T03:00:18.036Z
---

Thread "Decide where each MCP server runs" (2026-11-15).

- 2026-11-15 21:33Z: Jason chose a home-server MCP gateway (decision card): small stdlib Go gateway, tailnet only, injects each server's key from op run, logs every tool call (feeds JGA-348), gates P0 tool egress. Laptops hold no MCP keys. Not yet logged as a D-number (next free is D72 after D71).
- 2026-11-16 02:38Z: Jason approved the per-server table ("I like the current list"). Then asked for a tool map by profile plus suggested additions: written to /mnt/project-files/architecture/tool-map.md (built-in Hermes/OpenCode tool picks, power additions: CalDAV calendar, Bud ops MCP, KB search in memory server, SearXNG). Those picks await his OK.
- 2026-11-16 03:10Z: Jason approved additions #1-#4 plus Playwright, cocoindex-code, Contacts, Headroom MCP, and a deferred (post-2.0) Dollarwise connector. Logged as D72-D90 in the arch doc (D72 gateway ... D90 Dollarwise); build-plan phase 2 updated. Filed 2026-11-16 03:03Z: JGA-357 gateway, 358 config rendering, 359 CalDAV, 360 CardDAV, 361 ops MCP, 362 Dollarwise (after 2.0), 363 review after 2.0; comments on JGA-301/295/310/314; JGA-355 Done. Ids written into build-plan.md phase 2.
- Finding: Hermes `web` and OpenCode `websearch` both support Exa natively; Hermes `web.keyless_fallback` defaults true (must be false for P0 egress control).
- The per-server table (approved): 1Password not an MCP (op run, D7); Bud memory server-hosted over tailnet; Linear and Exa behind the gateway; GitHub MCP server only (laptop uses gh/git); Context7 and Serena local direct, no keys; retire Basic Memory (D13/D51); drop Claude Docs/Artifacts/HIBP. Also proposed: block cloud MCP tools in P0 sessions; op:// env-fragment gap as low-priority follow-up.
- Overlay profile `local` = the Qwen box (Jason confirmed 2026-11-16).

After approval: log decisions in architecture/bud-suite-architecture.html, update build-plan.md phase 2, update JGA-355, file follow-ups. Related: [[bud-suite-implementation]]
- 2026-11-16 03:12Z: Jason approved the whole tool map incl. built-in toolset picks. Thread done.
