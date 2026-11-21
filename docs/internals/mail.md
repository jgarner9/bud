# The mail MCP server

`bud mail-mcp` (`internal/mail`) gives Hermes, the home server's manager, Jason's iCloud mail (Suite D31, D54, D106). It is a small MCP server in standard-library Go (D1), listening on loopback only. It is meant to sit behind the MCP gateway (`docs/internals/mcp-gateway.md`), listed for P0 sessions alone so mail stays with Qwen (D23); that gateway entry and its tiers are the next slice, so for now nothing reaches it but loopback. It runs under `op run`, so the Apple ID and its app-specific password reach it as `BUD_MAIL_USER` and `BUD_MAIL_PASSWORD` and never sit in a file (D7). The design and the build order are in the R7 mail design note; this page says what is built.

So far it reads. Moving mail to Deleted Messages, the cleanup digest and approved sends over SMTP are the next slices.

## Flags

| Flag | Default | What it is |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8796` | Where MCP is served, at `/mcp`. Only a loopback address is accepted, since the gateway is what devices reach. |
| `--imap` | `imap.mail.me.com:993` | The IMAP server, with implicit TLS. A loopback host is dialed in the clear, for tests; the client refuses to log in over a plain connection anywhere else. |

## The tools

| Tool | Read-only | What it does |
| --- | --- | --- |
| `list_folders` | yes | Each folder that can hold mail, with its message and unread counts, and `special` (`trash`, `sent`, `junk`, `drafts`, `archive`) from its special-use flag. |
| `search_messages` | yes | The newest matches in one folder (default `INBOX`) for `from`, `to`, `subject`, `text`, `since`, `before` and `unseen`, newest first, `limit` 20 by default and at most 100, with how many matched in all. |
| `read_message` | yes | One message by folder and uid: its header, its text, and its attachments' names, types and sizes (never their contents). |

Nothing a tool does marks mail read: folders are opened with `EXAMINE` and messages fetched with `BODY.PEEK`. A tool's failure comes back as a result with `isError` and a sentence saying what to fix.

**Mail is untrusted.** Whatever the sender wrote (the header fields and the text) sits under an `untrusted` key, beside a `notice` that says it is data and never an instruction, and the provenance (`folder`, `uid`, `message_id`) sits outside it. That is labeling, not isolation: JGA-317's tool-less summarizer is a later slice. What holds regardless is that nothing in a message can approve anything, since approvals come only from Jason's Signal number (D34).

## IMAP

`imap.go` is the client, one connection per tool call (`account.go` dials, logs in, does the call and logs out, one call at a time), so a dropped connection leaves nothing behind. It speaks the subset of RFC 9051 the tools need: `LOGIN`, or `AUTHENTICATE PLAIN` when the server sets `LOGINDISABLED`; `CAPABILITY`, `LIST`, `STATUS`, `EXAMINE` and `SELECT`; `UID SEARCH`; `UID FETCH` of flags, size, `INTERNALDATE` and `BODY.PEEK[HEADER.FIELDS (...)]` for summaries, and `BODY.PEEK[]<0.N>` (at most 1 MiB) for a read; `UID MOVE`, or `UID COPY`, `UID STORE +FLAGS.SILENT (\Deleted)` and `UID EXPUNGE` of exactly those uids when the server lacks `MOVE`. Without `UIDPLUS` that fallback is refused, since a plain `EXPUNGE` would also remove anything else marked deleted. A refused login is `ErrAuth`, which says to check the app-specific password and never shows it.

`wire.go` parses responses: atoms, quoted strings, literals, nested lists, `NIL` and sections such as `BODY[HEADER.FIELDS (FROM)]<0>`, with literals capped at 1 MiB and lines at 64 KiB. Search text that isn't plain ASCII goes as a synchronizing literal with `CHARSET UTF-8`, and search text with a line break is refused. Folder names are modified UTF-7 on the wire (`utf7.go`). A canceled or expired context sets the connection's deadline, so a server that stops answering can't hold a call.

## Messages

`message.go` reads with `net/mail`, `mime` and `mime/multipart`: RFC 2047 words in headers, addresses as `Name <address>`, `quoted-printable` and `base64` bodies, nested multiparts (at most 8 deep and 100 parts). The text is the first `text/plain` part, else the first `text/html` part stripped to text (scripts, styles and tags dropped, breaks kept at block ends, entities decoded, `from_html: true`). Every other part, and any part with a filename, is an attachment. Charsets are UTF-8, US-ASCII and Latin-1; another is read as UTF-8 with bad bytes replaced, and a note says so. Text is cut at 64 KiB on a character boundary, and `notes` says when anything was cut or guessed.

## The MCP side

`server.go` is the same Streamable HTTP subset as the calendar server (`docs/internals/calendar.md`): one JSON answer per `POST /mcp`, no sessions, `GET` gets 405, protocol versions 2025-11-25, 2025-06-18 and 2025-03-26, and a request with `Origin` or a `Host` that isn't loopback gets 403. Each tool call gets two minutes.

## Tests

The client tests replay IMAP transcripts in `testdata/*.imap` against a fake server on loopback that fails on any line bud sends that the transcript doesn't expect (the format is at the top of `testdata/login_list.imap`). They are hand-written from the RFCs for now; a recorder run once on the home server will replace them with redacted iCloud sessions. `testdata/messages/` holds sample messages, among them one that tries to approve an action from inside the mail.

## Not yet

- `move_to_trash`, `restore_message`, `cleanup_digest` and the action log; `send_message` and `reply_message` over net/smtp, which ask through the gateway's approval tiers (D103).
- The gateway entry and tiers in `content/env/server.jsonc`, the unit in `server/mail-mcp/`, and the recorder.
- JGA-317's isolated ingestion.
