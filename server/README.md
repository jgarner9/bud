# server/

Home-server setup and service units for Hermes, the planning OpenCode, the Bud MCP servers and Signal. The service units and setup guides live in this repo. The Bud binary includes the MCP server commands and the Hermes plugin.

| Dir | What it holds |
| --- | --- |
| `mcp-gateway/` | The MCP gateway's unit and example config (Suite D72) |
| `calendar-mcp/` | The calendar MCP server's unit, over iCloud CalDAV (Suite D82) |
| `contacts-mcp/` | Read-only iCloud contacts unit for private Hermes (Suite D88) |
| `memory-mcp/` | The memory MCP server's unit, how every runtime reaches working memory through the gateway (Suite D102, D108) |
| `memory-review/` | The memory reviewer's unit: asks Jason on Signal about each staged memory record, a yes approves and a no deletes (Suite D113) |
| `planning-opencode/` | The planning OpenCode's system unit, running `opencode serve` as its own user `bud-planning` for the IDE's planning chats (Suite D49, D109) |
| `mcp-ops/` | The Bud ops MCP server's unit (Suite D83) |
| `embed/` | The embedding server's unit: llama.cpp with Qwen3-Embedding-0.6B on the CPU, for working memory (Suite D52, JGA-294) |
| `hermes/` | How Hermes, the manager, is set up; bud renders its config and Hermes installs its own unit (Suite D48). `hermes/plugins/bud-signal/` is the Hermes plugin that lets Signal reactions answer approvals (D30) |
| `signal-cli/` | signal-cli's daemon unit, Signal for Hermes on the bot number (Suite D30) |

## Ports

Each service on the home server has its own port, all on loopback except the gateway and the planning OpenCode's tailnet side. `TestServerDefaultPortsDontCollide` (`internal/cli/ports_test.go`) fails when a `bud` command's `--listen` default lands on a port another service uses; add a row here and an entry there when you add a service.

| Port | Service |
| --- | --- |
| 8082 | the embedding server (`embed/`) |
| 8790 | the MCP gateway (`mcp-gateway/`) |
| 8791 | `bud calendar-mcp` |
| 8792 | `bud mcp-ops` |
| 8793 | signal-cli's daemon (`signal-cli/`) |
| 8794 | `bud contacts-mcp` |
| 8795 | `bud router` |
| 8796 | `bud mail-mcp` |
| 8797 | the planning OpenCode (`planning-opencode/`) |
| 8798 | `bud memory-mcp` (8793 until November 2026, which collided with signal-cli) |
