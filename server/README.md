# server/

Home-server setup and service units for Hermes, the Bud MCP servers and Signal. The service units and setup guides live in this repo. The Bud binary includes the MCP server commands and the Hermes plugin.

| Dir | What it holds |
| --- | --- |
| `mcp-gateway/` | The MCP gateway's unit and example config (Suite D72) |
| `calendar-mcp/` | The calendar MCP server's unit, over iCloud CalDAV (Suite D82) |
| `contacts-mcp/` | Read-only iCloud contacts unit for private Hermes (Suite D88) |
| `memory-mcp/` | The memory MCP server's unit, how every runtime reaches working memory through the gateway (Suite D102, D108) |
| `mcp-ops/` | The Bud ops MCP server's unit (Suite D83) |
| `embed/` | The embedding server's unit: llama.cpp with Qwen3-Embedding-0.6B on the CPU, for working memory (Suite D52, JGA-294) |
| `hermes/` | How Hermes, the manager, is set up; bud renders its config and Hermes installs its own unit (Suite D48). `hermes/plugins/bud-signal/` is the Hermes plugin that lets Signal reactions answer approvals (D30) |
| `signal-cli/` | signal-cli's daemon unit, Signal for Hermes on the bot number (Suite D30) |
