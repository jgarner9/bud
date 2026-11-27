# server/mail-mcp/

The home server's mail MCP server (Suite D106): `bud mail-mcp` behind a systemd user unit. It reads Jason's iCloud mail for Hermes over IMAP, moves mail to Deleted Messages and back, and sends over SMTP. It sits behind the MCP gateway, which lets only P0 sessions use it and asks Jason on Signal before every send or reply, showing the whole message (D107). Hermes's private profile, which lists only P0 models, is the one client, so mail stays with Qwen (Suite D95). How it works is in [docs/internals/mail.md](https://github.com/jgarner9/bud/blob/main/docs/internals/mail.md).

| File | What it is |
| --- | --- |
| `bud-mail-mcp.service` | The user unit. It runs the server under `op run`, so the credentials reach it as environment variables, on `127.0.0.1:8796`. |
| `mail-mcp.env.example` | The `op://` references `op run` resolves, with placeholder vault paths. |

## Setting it up

1. Use the Bud vault's `iCloud` item, which the calendar server already reads: its username is the Apple ID, its password the app-specific password made for Bud (appleid.apple.com, Sign-In and Security, App-Specific Passwords), and its `email` field the address mail goes out from. Without that item yet, make it as the calendar README says and add the `email` field.
2. Put the env file at `~/.config/bud-server/mail-mcp.env`, mode 0600, with the item's `op://` paths. The service account's token file is the one the gateway already uses.
3. Record one read-only session, which checks the login and what iCloud supports: `op run --env-file ~/.config/bud-server/mail-mcp.env -- bud mail-mcp --record /tmp/icloud.imap`. It prints whether the server has `MOVE` and `UIDPLUS` and which folder is the trash. The file has the login, every literal and every address redacted; read it before sharing it.
4. Install and start the unit with the commands at the top of `bud-mail-mcp.service`.
5. Run `bud sync`, which adds the `mail` tiers to the gateway's approvals file, add the `mail` entry from `server/mcp-gateway/mcp-gateway.example.json` to the gateway's config, and restart the gateway.
6. Check it through the gateway: `curl -s -H 'X-Bud-Privacy: P0' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_folders","arguments":{}}}' http://<listen>/mail/mcp` lists the folders, and the same request with `X-Bud-Privacy: P1` is refused.
7. From Hermes, send one email to yourself and approve it on Signal; check that the prompt shows the whole message, that it arrives, and whether a copy shows up in Sent Messages. Then move a message to the trash and restore it.
8. Check what a send and a trash move leave behind, through the gateway (the same `curl` as step 6, with `list_folders` replaced by each call below). Send yourself one email from Hermes and approve it on Signal. Then run `search_messages` with `folder` "Sent Messages" and `subject` set to the email's subject: a hit means a send leaves a copy there, and no hit means it doesn't, so don't rely on one. Take the uid of that copy, or of the delivered message in `INBOX`, and call `move_to_trash` on it, then `search_messages` in "Deleted Messages" to see it there and `restore_message` by its Message-ID to bring it back. The retention window (how long Deleted Messages keeps mail before iCloud empties it) can't be seen in one sitting: leave one trashed test message in place, note its date, and look again after a week.
