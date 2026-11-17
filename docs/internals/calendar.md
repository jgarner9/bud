# The calendar MCP server

`bud calendar-mcp` (`internal/calendar`) gives Hermes, the home server's manager, Jason's iCloud calendars over CalDAV (Suite D82). It is a small MCP server in standard-library Go (Suite D92), listening on loopback only, behind the MCP gateway (`docs/internals/mcp-gateway.md`), which lists it for P0 sessions alone, so calendar data stays with Qwen. The server device's sessions send `X-Bud-Privacy: P1`, but Hermes's config sends `P0` to the calendar (Suite D93): Hermes runs only models allowed P0, so what it reads there stays with them. The planning OpenCode keeps P1, so the gateway refuses it, and its rules deny the calendar's tools as well. It runs as the user unit in `server/calendar-mcp/`, under `op run`, so the Apple ID and its app-specific password reach it as `BUD_CALDAV_USER` and `BUD_CALDAV_PASSWORD` and never sit in a file (Suite D7, D31).

Reminders are events with alarms, which ring on Jason's phone, and reminder texts by Signal come from Hermes's own scheduled jobs (Suite D91). Upgraded iCloud Reminders aren't served over CalDAV, so there are no task (VTODO) tools; a reminders-only list still shows in `list_calendars`, with `events: false`.

## Flags

| Flag | Default | What it is |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8791` | Where MCP is served, at `/mcp`. Only a loopback address is accepted, since the gateway is what devices reach. |
| `--url` | `https://caldav.icloud.com/` | The CalDAV server. `https` only (plain `http` only to loopback, for tests). |
| `--tz` | the system's zone | The zone for times a tool is given without one, for "today", and for the times it answers with. |

## The tools

| Tool | Read-only | What it does |
| --- | --- | --- |
| `list_calendars` | yes | Each calendar's id (its collection path), name and whether it holds events. |
| `list_events` | yes | The events overlapping `start` to `end` (default: the start of today to 7 days on, so no arguments is this week; at most 366 days), from every calendar or one, sorted by start. |
| `create_event` | no | A new event: `title`, `start`, and `end` or `duration_minutes` (default 60), plus `calendar`, `location`, `notes` and `alarm_minutes`. A start that's a date alone makes an all-day event, whose `end` is its last day. |
| `move_event` | no | A new `start` (and optional `end`, else the length is kept) for an event that doesn't repeat. |
| `delete_event` | no | Deletes an event that doesn't repeat. |

Times are RFC 3339, a local `2026-11-18T14:30`, or a date. An event's id is its resource path from `list_events`. The read-only tools carry `readOnlyHint: true` and the rest `false`, and `content/env/server.jsonc` marks the server `trust: untrusted` for Hermes, which then asks before every tool that isn't read-only; Hermes's approvals by phone (D48, JGA-290) are what make a write wait for Jason. A tool's failure comes back as a result with `isError` and a sentence saying what to fix, not as a JSON-RPC error.

## CalDAV

The client (`caldav.go`) finds the calendars the way RFC 6764 and RFC 4791 lay out: `PROPFIND` the base URL for `current-user-principal`, the principal for `calendar-home-set`, then the home at depth 1 for collections that are calendars, with their names and `supported-calendar-component-set`. The home is found once per process. Listing events is one `calendar-query` `REPORT` per calendar, with a `time-range` filter and `expand`, so the server hands back each occurrence of a repeating event in the range; one it didn't expand comes back once, with its rule in `repeats`. Events outside the range are dropped, since a server may send them anyway.

Writes guard against lost updates: creating `PUT`s `<uid>.ics` with `If-None-Match: *`, and moving and deleting first `GET` the event and send its `ETag` in `If-Match`, so an event changed on the phone in between is refused ("list it again and retry") rather than overwritten. Moving rewrites only the event's `DTSTART`, `DTEND` (dropping `DURATION`), `DTSTAMP` and `LAST-MODIFIED`, and bumps `SEQUENCE`; every other line, Apple's own properties and the alarms among them, is kept as it was. Moving or deleting a repeating event, or one occurrence of it, is refused for now, naming Calendar as the place to do it.

The credentials go only to `https` (or `http` on loopback) and only to the base host, or, when it's an `icloud.com` host, to another `icloud.com` host, since iCloud answers from `p<n>-caldav.icloud.com` beside `caldav.icloud.com`. Redirects are followed under the same rule, and one that would change the method (a 301, 302 or 303 turns a `PUT` or `DELETE` into a bodiless `GET`, which would look like success) is refused. The event ids a tool is given must lie under the calendar home. A 401 says to check the app-specific password.

## iCalendar

`ical.go` reads and writes just what the tools need from RFC 5545: lines are unfolded, parameters may be quoted, text is unescaped, and times are dates (all day), UTC, `TZID` (falling back to `--tz` when Go doesn't know the zone, as with a custom `VTIMEZONE`) or floating (in `--tz`). An event's end is `DTEND`, else `DURATION`, else a day for an all-day event. Alarms are the `VALARM`s whose `TRIGGER` is a duration, counted from the end when it says `RELATED=END`; one that rings after the start is left out, since `alarm_minutes` counts back from the start. An event with `RRULE`, `RDATE` or `RECURRENCE-ID` is a repeating one. New events are written in UTC, which every client shows in its own zone, with lines folded at 75 octets.

## The MCP side

`server.go` answers MCP's Streamable HTTP transport in its simplest form: every `POST /mcp` gets one JSON answer (a batch gets an array, notifications alone get 202), with no sessions and no event stream, so `GET` gets 405. It speaks protocol versions 2025-11-25, 2025-06-18 and 2025-03-26, answering `initialize` with the client's version when it's one of them. A request carrying `Origin`, or whose `Host` isn't a loopback address or `localhost`, gets 403, against DNS rebinding from a browser on the server. The gateway sets `Host` to the upstream's address, so its forwarded calls pass.

## Not yet

- Inside one Hermes chat, calendar details could still be handed to a cloud tool (Linear, web search) or to a cloud model Jason picks by hand with `/model`; the gateway's header can't see that. Tagging each session's tier is the router's job (phase 3).
- Moving or deleting a repeating event, or one occurrence of it.
- The gateway's audit log records each call; the shared approval and audit log is JGA-348.
