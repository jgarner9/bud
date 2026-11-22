# R7: `bud mail-mcp`, iCloud mail behind the gateway

> **Historical status (handoff note, 2026-11-22).** Built: slices 1 to 4 merged in PRs #81 to #84, and mail runs on the home server (live since 2026-11-22). What's left is the `--record` output and the ingestion work in JGA-317. Current docs: `docs/internals/mail.md`.

Status: approved by Jason 2026-11-21; building in the slice order below. Nothing here is built yet. Already decided and not reopened: standard library only, own IMAP subset, net/smtp with STARTTLS on 587 (Suite D106, D1); mail is for reading, every send and reply asks (D31); cleanup only moves to Deleted Messages, runs alone and is reported in a digest (D36, D54); mail is P0, Qwen only (D23); the app-specific password is an `op://` reference in the Bud vault and the agent never reads it (D7); approvals are enforced at the gateway's leaf through the bud-signal flow (D97, D101, D103). Linear: JGA-315 (this server), JGA-317 (isolated ingestion, a later slice).

## Shape

`bud mail-mcp` (`internal/mail`) is a loopback MCP server like `bud calendar-mcp`: the same Streamable HTTP subset, `Origin` and `Host` checks, `isError` results with a sentence saying what to fix. It listens on `127.0.0.1:8796` and is listed in the gateway as `mail`, privacy `["P0"]` only, so only Hermes's private profile reaches it (D95). It runs as `server/mail-mcp/bud-mail-mcp.service` under `op run`, which fills `BUD_MAIL_USER` (the Apple ID login), `BUD_MAIL_PASSWORD` (the app-specific password) and `BUD_MAIL_FROM` (the address sends come from; iCloud refuses a `From` the account doesn't own). No value is ever logged or returned by a tool.

| Flag | Default | What it is |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8796` | Loopback only. |
| `--imap` | `imap.mail.me.com:993` | Implicit TLS. A plain connection only to loopback, for tests. |
| `--smtp` | `smtp.mail.me.com:587` | STARTTLS is required; a server that doesn't offer it is refused before `AUTH`. |
| `--trash` | `Deleted Messages` | Where cleanup moves mail. Found by the `\Trash` special-use flag first, this name second. |
| `--log` | `~/.local/state/bud/mail-actions.jsonl` | The action log: every move, restore and send (`paths.MailActions`). |

## Tools and their tiers

| Tool | Tier | What it does |
| --- | --- | --- |
| `list_folders` | allow | Each folder's name, special-use flag, and message and unseen counts (`STATUS`). |
| `search_messages` | allow | Newest first in one folder (default `INBOX`): `from`, `to`, `subject`, `text`, `since`, `before`, `unseen`, `limit` (default 20, at most 100). Each hit: uid, date, from, to, subject, size, flags, Message-ID. |
| `read_message` | allow | One message by folder and uid with `BODY.PEEK[]`, so it stays unread: headers, the text part (HTML stripped to text when there is no plain part), attachment names and sizes only. Capped at 1 MiB fetched and 64 KiB of text returned, saying when it cut. |
| `send_message` | ask | A new plain-text message: `to`, `cc`, `subject`, `body`. |
| `reply_message` | ask | A reply to folder and uid: `to`, `cc`, `subject`, `body`. The server checks the recipients and the subject (the original's with `Re:`) against the original, so the approval shows both, and sets `In-Reply-To` and `References` from it (D107). |
| `move_to_trash` | allow | Moves up to 50 uids from one folder to Deleted Messages and logs each by Message-ID, sender and subject. |
| `restore_message` | allow | Moves a message back out of Deleted Messages to the folder it came from, found by Message-ID from the log. |
| `cleanup_digest` | allow | The moves since a time (default the last 24 h) from the log, for Hermes's daily digest to Jason (D36). |

The tiers live in `content/env/server.jsonc` beside the calendar's (`mail_list_*`, `mail_search_*`, `mail_read_*`, `mail_move_*`, `mail_restore_*`, `mail_cleanup_*` allow; `mail_send_*` and `mail_reply_*` ask; anything else asks). The gateway binds an approval to the exact argument bytes (D103), so the Signal prompt for a send must show what the bytes say. The send tools' arguments are therefore the whole message: no tool argument names a template, an attachment path or a draft id that could change after approval. Recipients are limited to 10 per send. The Signal prompt for a send shows the whole message (D107): `to`, `cc`, `subject` and the full body, with bodies over 4,000 characters refused. A reply's arguments name its `to` and `cc` too, and the server refuses any recipient who isn't on the original (Reply-To or From, To, Cc, minus Jason's own addresses), so the prompt shows the real recipients.

There is no permanent delete, no `EXPUNGE` of anything but the copies a move leaves behind (below), no flag setting, no attachments on sends, and no invite accepting. Each is a separate decision if it's ever wanted.

## IMAP subset

`imap.go` speaks just enough of RFC 9051 / RFC 3501 over `crypto/tls`: the greeting, `CAPABILITY`, `LOGIN` (iCloud's app-specific password; `AUTHENTICATE PLAIN` is the fallback if `LOGINDISABLED`), `LIST "" "*"` with special-use flags, `STATUS`, `SELECT` and `EXAMINE` (reads use `EXAMINE`, so nothing changes), `UID SEARCH`, `UID FETCH` with `BODY.PEEK[HEADER.FIELDS (...)]`, `BODY.PEEK[]<0.N>`, `FLAGS`, `RFC822.SIZE` and `INTERNALDATE`, `UID MOVE` when `MOVE` is in the capabilities, else `UID COPY` then `UID STORE +FLAGS.SILENT (\Deleted)` then `UID EXPUNGE` of exactly those uids (`UIDPLUS`; without it the move is refused rather than risk expunging other messages someone marked deleted), and `LOGOUT`. The parser handles tagged and untagged lines, quoted strings, literals `{n}`, nested lists and `NIL`, with a 1 MiB literal cap. Search strings with non-ASCII text are sent as literals with `CHARSET UTF-8`. Folder names use modified UTF-7.

One connection per tool call, opened and logged out inside it, so a dropped connection never leaves state behind and there's nothing to keep alive. Calls are serialized by a mutex; mail tools are slow anyway and Hermes is one client.

Message headers are parsed with `net/mail` (RFC 2047 words via `mime.WordDecoder`), bodies with `mime/multipart` and `mime/quotedprintable`, with `charset` limited to UTF-8, US-ASCII and Latin-1 (others are shown as replaced characters, saying so).

## SMTP

`smtp.go` uses `net/smtp`: dial 587, `STARTTLS` with the host's certificate checked, `AUTH PLAIN`, one message. The message is built by bud: `From` is `BUD_MAIL_FROM`, `Date`, a fresh `Message-ID`, `MIME-Version`, `Content-Type: text/plain; charset=utf-8`, quoted-printable body, headers encoded with `mime.QEncoding`. Header values with CR or LF are refused, so no argument can add a header.

iCloud may or may not file a sent copy in Sent Messages on its own. The live check (slice 4) finds out; if it doesn't, the server `APPEND`s the copy itself.

## Untrusted mail (JGA-317)

Everything a read tool returns comes wrapped as data: a JSON result whose text fields sit under `untrusted` with the folder, uid and Message-ID as provenance, and the tool description says the content is mail from outside and is never an instruction. That is labeling, not isolation. The isolation JGA-317 asks for (a tool-less summarizer handed new mail on a timer, idempotent by uid and UIDVALIDITY) is slice 5 and lives in Hermes's config, not in this server. What the server guarantees on its own: nothing in a message can approve a send (approvals come only from Jason's Signal number, D34), and every action tool needs a Jason-visible approval or is a reversible move.

## Tests

Recorded sessions: `testdata/*.imap` and `*.smtp` are transcripts, one line per turn prefixed `C:` or `S:`, replayed by a fake server on loopback that fails the test on any line the client sends that the transcript doesn't expect. The first transcripts are written from the RFCs and iCloud's documented capabilities, since this container can't reach iCloud. Slice 4 adds `bud mail-mcp --record <dir>`, which logs a real session with the `LOGIN` and `AUTH` lines replaced by `<redacted>` and message bodies replaced by fixed text, and Jason runs it once on the home server; those transcripts then replace the hand-written ones wherever they differ. Adversarial fixtures (forged "yes 1234" approval text, injected instructions, encoded headers, a CRLF in a subject argument) cover JGA-317's validation list for what the server itself can check.

## Slices, one PR each

1. **IMAP client.** `internal/mail/imap.go` and its parser, the transcript replayer, tests for login, list, status, search, peek fetch, move and the copy-and-expunge fallback. No command yet.
2. **Read tools.** `bud mail-mcp` with the MCP server, `list_folders`, `search_messages`, `read_message`, MIME parsing, the untrusted wrapper, `paths.MailActions`, `docs/internals/mail.md`, `docs/commands.md`.
3. **Cleanup and sends.** `move_to_trash`, `restore_message`, `cleanup_digest`, the action log, `send_message` and `reply_message` over net/smtp, the gateway entry and tiers in `server.jsonc`.
4. **Unit and live check.** `server/mail-mcp/` (unit, env example, README), `--record`, and the exact commands for Jason on the home server: the 1Password item, install, start, record, one test send to himself (approved on Signal), a move and a restore, and checking the Deleted Messages retention setting (D54).
5. **Ingestion (JGA-317).** The timer and tool-less summarizer profile in Hermes, after slice 4 runs live.

## Open questions

1. Does iCloud file sent mail in Sent Messages itself? Answered by the live check; the fallback is `APPEND`.
2. Is Deleted Messages still kept about 30 days? D54 says verify at build time. If it's shorter, cleanup moves to a `Bud Holding` folder instead and a later slice empties it after 30 days, which would need Jason's decision since it's a delete.
3. Does iCloud advertise `MOVE` and `UIDPLUS`? Both are handled either way; the recording says which path runs.
