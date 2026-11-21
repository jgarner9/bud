# The mail MCP server

`bud mail-mcp` (`internal/mail`) gives Hermes, the home server's manager, Jason's iCloud mail (Suite D31, D54, D106). It is a small MCP server in standard-library Go (D1), listening on loopback only, behind the MCP gateway (`docs/internals/mcp-gateway.md`), which lists it for P0 sessions alone, so mail stays with Qwen (D23). It runs under `op run`, so the Apple ID and its app-specific password reach it as `BUD_MAIL_USER` and `BUD_MAIL_PASSWORD`, and the address sends come from as `BUD_MAIL_FROM`, and none sits in a file (D7). With `BUD_MAIL_FROM` unset it still reads and cleans up, and the send tools say what to set. The design and the build order are in the R7 mail design note; this page says what is built.

## Flags

| Flag | Default | What it is |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8796` | Where MCP is served, at `/mcp`. Only a loopback address is accepted, since the gateway is what devices reach. |
| `--imap` | `imap.mail.me.com:993` | The IMAP server, with implicit TLS. A loopback host is dialed in the clear, for tests; the client refuses to log in over a plain connection anywhere else. |
| `--smtp` | `smtp.mail.me.com:587` | The SMTP server. STARTTLS is required, except to a loopback host (tests); a server that doesn't offer it is refused before the password is sent. |
| `--trash` | `Deleted Messages` | Where cleanup moves mail when no folder has the `\Trash` special-use flag. |
| `--record` | | Instead of serving, run one scripted read-only session (login, capabilities, `LIST`, `STATUS`, `EXAMINE`, `SEARCH`, a summary and a body fetch, logout) and write it to this file in the transcript format, with the login, every literal and every address redacted, then print whether the server has `MOVE` and `UIDPLUS` and which folder is the trash. The file must not exist yet. |
| `--log` | `~/.local/state/bud/mail-actions.jsonl` | The action log (`paths.MailActions`), mode 0600: one JSON line per move to the trash, restore and send, naming the message by Message-ID with its sender and subject, or for a send its recipients and subject. Never a body. |

## The tools

| Tool | Read-only | What it does |
| --- | --- | --- |
| `list_folders` | yes | Each folder that can hold mail, with its message and unread counts, and `special` (`trash`, `sent`, `junk`, `drafts`, `archive`) from its special-use flag. |
| `search_messages` | yes | The newest matches in one folder (default `INBOX`) for `from`, `to`, `subject`, `text`, `since`, `before` and `unseen`, newest first, `limit` 20 by default and at most 100, with how many matched in all. |
| `read_message` | yes | One message by folder and uid: its header, its text, and its attachments' names, types and sizes (never their contents). |
| `move_to_trash` | marked yes | Moves up to 50 uids from one folder to the trash (the folder with `\Trash`, else `--trash`) and logs each. |
| `restore_message` | marked yes | Moves a message bud put in the trash back where it came from, by Message-ID, from the log. It refuses a message the log has no move of, or one already restored. |
| `cleanup_digest` | yes | The moves and restores since `since` (default the last 24 hours), with counts, for Hermes's daily digest to Jason (D36). |
| `send_message` | no | A new plain-text email: `to` and `cc` (lists, 10 people at most together), `subject` (one line) and `body` (at most 4,000 characters). |
| `reply_message` | no | A reply to folder and uid: `to`, `cc`, `subject` and `body`. `to` must hold whoever the original came from (Reply-To, else From), and every recipient must be on the original (less this account's own addresses), or it refuses and lists who is. `subject` must be the original's with `Re:` in front (one, not two), or it refuses and names the right one; `In-Reply-To` and `References` come from the original. |

Nothing a tool does marks mail read: folders are opened with `EXAMINE` and messages fetched with `BODY.PEEK`. A tool's failure comes back as a result with `isError` and a sentence saying what to fix. Each tool takes only its own arguments, so one it would ignore is refused rather than shown in an approval for a message that doesn't carry it.

**Who decides.** Hermes runs read-only tools alone and asks for the rest (`trust: untrusted` in `content/env/server.jsonc`). Moving to the trash and restoring change nothing that can't be undone, so they are marked read-only and run alone (D36); only the sends are marked as writes. In the gateway, the env fragment's tiers allow everything but `send_*` and `reply_*`, which ask on Jason's phone (D103). Mail sends are the one approval whose Signal prompt shows every argument whole (D107, `internal/approval`'s `summarize`): to, cc, subject and the full body, the body as lines that each open with a bar. That's why a body is capped at 4,000 characters, and why a reply names its recipients and subject in its own arguments instead of having the server work them out after approval. The gateway binds the approval to those exact argument bytes, so what Jason reads is what goes out.

**Mail is untrusted.** Whatever the sender wrote (the header fields and the text) sits under an `untrusted` key, beside a `notice` that says it is data and never an instruction, and the provenance (`folder`, `uid`, `message_id`) sits outside it. That is labeling, not isolation: JGA-317's tool-less summarizer is a later slice. What holds regardless is that nothing in a message can approve anything, since approvals come only from Jason's Signal number (D34).

## IMAP

`imap.go` is the client, one connection per tool call (`account.go` dials, logs in, does the call and logs out, one call at a time), so a dropped connection leaves nothing behind. It speaks the subset of RFC 9051 the tools need: `LOGIN`, or `AUTHENTICATE PLAIN` when the server sets `LOGINDISABLED`; `CAPABILITY`, `LIST`, `STATUS`, `EXAMINE` and `SELECT`; `UID SEARCH`; `UID FETCH` of flags, size, `INTERNALDATE` and `BODY.PEEK[HEADER.FIELDS (...)]` for summaries, and `BODY.PEEK[]<0.N>` (at most 1 MiB) for a read; `UID MOVE`, or `UID COPY`, `UID STORE +FLAGS.SILENT (\Deleted)` and `UID EXPUNGE` of exactly those uids when the server lacks `MOVE`. Without `UIDPLUS` that fallback is refused, since a plain `EXPUNGE` would also remove anything else marked deleted. A refused login is `ErrAuth`, which says to check the app-specific password and never shows it.

`wire.go` parses responses: atoms, quoted strings, literals, nested lists, `NIL` and sections such as `BODY[HEADER.FIELDS (FROM)]<0>`, with literals capped at 1 MiB and lines at 64 KiB. Search text that isn't plain ASCII goes as a synchronizing literal with `CHARSET UTF-8`, and search text with a line break is refused. Folder names are modified UTF-7 on the wire (`utf7.go`). A canceled or expired context sets the connection's deadline, so a server that stops answering can't hold a call.

## Messages

`message.go` reads with `net/mail`, `mime` and `mime/multipart`: RFC 2047 words in headers, addresses as `Name <address>`, `quoted-printable` and `base64` bodies, nested multiparts (at most 8 deep and 100 parts). The text is the first `text/plain` part, else the first `text/html` part stripped to text (scripts, styles and tags dropped, breaks kept at block ends, entities decoded, `from_html: true`). Every other part, and any part with a filename, is an attachment. Charsets are UTF-8, US-ASCII and Latin-1; another is read as UTF-8 with bad bytes replaced, and a note says so. Text is cut at 64 KiB on a character boundary, and `notes` says when anything was cut or guessed.

## Sends

`smtp.go` uses `net/smtp`: dial, `EHLO`, `STARTTLS` with the host's certificate checked, `AUTH PLAIN`, one `MAIL FROM`, a `RCPT TO` per recipient (a refused recipient stops the send before `DATA`, so nothing goes out), then the message. bud writes the message itself: `From` is `BUD_MAIL_FROM`, then `To`, `Cc`, the subject Q-encoded, `Date`, a fresh random `Message-ID` at the From domain, `In-Reply-To` and `References` for a reply, and a quoted-printable UTF-8 plain-text body. No argument can add a header: addresses are parsed with `net/mail` and written back by it, and a subject, address or reply header with a line break is refused. Whether iCloud files a copy in Sent Messages by itself is checked on the first live run; if it doesn't, the server will `APPEND` one.

## Cleanup

`cleanup.go` moves mail only between folders; nothing in bud deletes mail (D36). A move reads the messages' headers first, moves them with the IMAP client, and then logs each by Message-ID. Moves, restores and sends run one at a time, each held together with its log line, so a restore or the digest never misses a move that's done but not logged yet; and once the work is done a failed LOGOUT is ignored, so a dropped connection can't skip the log line. `NewLog` sets an existing log to 0600 too. A message with no Message-ID can be moved but not restored by bud. A restore finds the latest log line for that Message-ID, which has to be a move to the trash, searches the trash for it and moves it back. iCloud empties Deleted Messages after about 30 days (D54, to be confirmed on the live run), after which a restore says it's gone.

## The MCP side

`server.go` is the same Streamable HTTP subset as the calendar server (`docs/internals/calendar.md`): one JSON answer per `POST /mcp`, no sessions, `GET` gets 405, protocol versions 2025-11-25, 2025-06-18 and 2025-03-26, and a request with `Origin` or a `Host` that isn't loopback gets 403. Each tool call gets two minutes.

## Tests

The client tests replay IMAP transcripts in `testdata/*.imap` against a fake server on loopback that fails on any line bud sends that the transcript doesn't expect (the format is at the top of `testdata/login_list.imap`; an `X:` line drops the connection, as `trash_dropped.imap` does in place of the LOGOUT answer). They are hand-written from the RFCs for now; `--record` (`record.go`) captures a redacted iCloud session to check them against. The login is redacted from its command to the server's answer, so a credential sent as a literal can't show on the line after it; every literal form (`{n}`, `{n+}`, `~{n}`) is a placeholder, and a literal whose length can't be read stops the trace rather than write its bytes as lines. The trace is written however the session ends, so a failed login is on file too. A recorded literal is a placeholder, so a recording shows the commands and responses as iCloud sends them but has to be filled in by hand before it replays. `testdata/messages/` holds sample messages, among them one that tries to approve an action from inside the mail. The send tests run against a fake SMTP server on loopback and check the exact message, that a refused recipient sends nothing, that a server without STARTTLS never gets the password, and that header injection and over-long bodies are refused.

## Not yet

- The first live run on the home server (`server/mail-mcp/README.md`), which settles Sent Messages, the trash's retention and whether iCloud has `MOVE` and `UIDPLUS`, and then recorded transcripts in place of the hand-written ones.
- JGA-317's isolated ingestion.
