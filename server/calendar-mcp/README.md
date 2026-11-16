# server/calendar-mcp/

The home server's calendar MCP server (Suite D82): `bud calendar-mcp` behind a systemd user unit. It reads and changes Jason's iCloud calendars over CalDAV for Hermes, and sits behind the MCP gateway, which lets only P0 sessions use it, so calendar data stays with Qwen. Reminders are events with alarms, which ring on the phone; upgraded iCloud Reminders aren't on CalDAV. How it works is in [docs/internals/calendar.md](https://github.com/jgarner9/bud/blob/main/docs/internals/calendar.md).

| File | What it is |
| --- | --- |
| `bud-calendar-mcp.service` | The user unit. It runs the server under `op run`, so the credentials reach it as environment variables, on `127.0.0.1:8791`. |
| `calendar-mcp.env.example` | The `op://` references `op run` resolves, with placeholder vault paths. |

## Setting it up

1. Make an app-specific password for the Apple ID (appleid.apple.com, Sign-In and Security, App-Specific Passwords) and save it, with the Apple ID's email as the username, in the Bud vault.
2. Put the env file at `~/.config/bud-server/calendar-mcp.env`, mode 0600, with the item's `op://` paths. The service account's token file is the one the gateway already uses.
3. Install and start the unit with the commands at the top of `bud-calendar-mcp.service`. Add `--tz <zone>` to its `ExecStart` if the server's own zone isn't Jason's.
4. Add the `calendar` entry from `server/mcp-gateway/mcp-gateway.example.json` to the gateway's config, and restart the gateway.
5. Check it through the gateway: `curl -s -H 'X-Bud-Privacy: P0' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_events","arguments":{}}}' http://<listen>/calendar/mcp` lists this week's events, and the same request with `X-Bud-Privacy: P1` is refused.
