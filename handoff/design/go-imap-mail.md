# The mail IMAP client on go-imap (Suite D138)

Status: decided 2026-11-25; building the same night. Jason asked for the change and granted the D1 exception himself ("I'm giving a D1 exception here btw, I am working to change this if you are looking for it"); the BASE.md D1 rewording is his own change in flight. This supersedes the R7 line "standard library only, own IMAP subset" (Suite D106 context) for the IMAP half only. Unchanged: `smtp.go` and `net/smtp`, `message.go`, `cleanup.go`'s semantics, the action log, every tool contract in `docs/internals/mail.md`, the tiers, one connection per call.

## What changes and why

`internal/mail`'s homegrown IMAP client — `imap.go`'s command engine, `wire.go`'s response parser, `utf7.go`'s modified UTF-7, and the hand-written transcript tests that pin them — is replaced by `github.com/emersion/go-imap/v2` (v2.0.0-beta.9): the maintained Go IMAP library (MIT, active, default branch `v2`, used by several mail clients; dependencies `emersion/go-sasl` for AUTHENTICATE and `emersion/go-message` in its module graph). It is bud's first third-party Go module.

What bud keeps: every safety refusal and error sentence below, `--record`'s tracer (it sits below the client on `net.Conn`), the exported client surface so `account.go`, `cleanup.go`, `actions.go`, `server.go` and `message.go` do not change, and the rule that nothing a read does marks mail read. What bud gains: a protocol client with years of edge-case fixes (literals, malformed responses, UIDPLUS/MOVE), and roughly 1.6K lines of bud code plus transcripts deleted.

`go.mod` gains `require github.com/emersion/go-imap/v2 v2.0.0-beta.9`; `go mod tidy` fills `go.sum` and the indirect requires. No vendoring.

## The adapter (`internal/mail/imap.go`, rewritten)

`imap.go` becomes a thin adapter over `imapclient.Client`. The exported surface stays exactly: `Client`, `Dial`, `Login`, `Logout`, `Close`, `Has`, `List`, `Status`, `Examine`, `Select`, `Search`, `FetchSummaries`, `FetchBody`, `Move`, the types `Folder`, `Status`, `Mailbox`, `Criteria`, `Summary`, `Body`, and the errors `ErrAuth`, `ErrNoMessage`. `wire.go` and `utf7.go` are deleted: the library parses responses, and it encodes and decodes mailbox names as modified UTF-7 itself (`ListData.Mailbox` is already the decoded name).

1. **Dial(ctx, addr, conf)** keeps its contract: implicit TLS with `conf`, clear only with nil conf (loopback, tests). Dial with `tls.Dialer`/`net.Dialer` under ctx, wrap with `imapclient.New(conn, nil)`, then `WaitGreeting`. A PREAUTH greeting is refused: "the IMAP server sent PREAUTH, an already logged-in connection: bud only uses connections it logs in itself" (`*imap.Error` with PREAUTH type). Another non-OK greeting: "the IMAP server refused the connection: ..." with the type and text.
2. **secure** is decided as now: a TLS conn, or a plain conn whose peer is loopback.
3. **Login(user, pass)** first refuses a plain connection off loopback: "refusing to log in over a plain connection: use TLS (port 993)". Then `c.Caps()`: with `LOGINDISABLED` the server must also offer `AUTH=PLAIN` (else "the IMAP server disables LOGIN and doesn't offer AUTHENTICATE PLAIN, so bud can't log in") and login goes through `c.Authenticate(sasl.NewPlainClient("", user, pass))`; otherwise `c.Login(user, pass).Wait()`. A tagged NO maps to `ErrAuth`, whose sentence stays word for word; the password never appears in an error.
4. **Logout** is `c.Logout().Wait()`; `account.session` still ignores a LOGOUT failure after finished work, and `Close()` still drops the connection.
5. **Has** maps "MOVE" to `imap.CapMove` and "UIDPLUS" to `imap.CapUIDPlus` (the only callers) onto `c.Caps()`; a nil set counts as absent.
6. **List** is `c.List("", "*", nil).Collect()`: `Folder{Name, Delim, Flags}` from `ListData{Mailbox, Delim, Attrs}`, sorted INBOX first then by name; `Selectable` and `HasFlag` keep working off the attr strings.
7. **Status(folder)** asks `NumMessages`, `NumUnseen`, `UIDNext`, `UIDValidity` and maps the pointers into `Status`.
8. **Examine/Select** is `c.Select(folder, &imap.SelectOptions{ReadOnly: ...}).Wait()`: EXAMINE on the wire when read-only, `Mailbox{Exists: NumMessages, UIDValidity, UIDNext}`. A NO maps to "there's no folder %q: list_folders shows the names (%w)".
9. **Search(cr)** validates first, unchanged (a line break in any text: "the %s search text has a line break; search for one line"; a bad Message-ID: "%q isn't a Message-ID"). Criteria map: From/To/Subject to `SearchCriteriaHeaderField{Key: "FROM"/"TO"/"SUBJECT"}`, Message-ID to `{Key: "Message-ID"}`, Text to `Text`, Since/Before to the date fields, Unseen to `NotFlag{imap.FlagSeen}`. `c.UIDSearch(criteria, nil).Wait()`; uids come back ascending and deduped. The library sends `CHARSET UTF-8` for non-ASCII itself.
10. **FetchSummaries(uids)**: `c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, RFC822Size: true, InternalDate: true, BodySection: []*imap.FetchItemBodySection{{Specifier: imap.PartSpecifierHeader, HeaderFields: strings.Fields(headerFields), Peek: true}}})`, iterated to `Summary{UID, Flags, Size, Received, Header}`, only the requested uids, in the order the server sends. The `headerFields` const is unchanged.
11. **FetchBody(uid, max)**: the same section with `Peek: true` and `Partial: &imap.SectionPartial{Offset: 0, Size: min(max, 1 MiB)}`; `Body{UID, Size, Raw, Truncated: Size > len(Raw)}`; no item for the uid is `ErrNoMessage`.
12. **Move(uids, dest)** keeps bud's own gating, not the library's internal fallback: with `imap.CapMove` it is `c.Move(set, dest).Wait()`; else with `imap.CapUIDPlus` it is `c.Copy(set, dest).Wait()`, then `c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close()`, then `c.UIDExpunge(set)`; else refused: "the IMAP server has neither MOVE nor UIDPLUS, so bud won't move mail: expunging without UIDPLUS could remove other messages". Only on a folder opened with Select ("move needs a folder opened with Select").
13. **Sets**: `imap.UIDSet` with `AddNum` per uid; the old `uidSet` helper goes.
14. **Contexts**: the library's Wait methods take none, so the adapter enforces the call's ctx itself, per method, the way the old `deadline(ctx)` did: the conn's deadline is ctx's, and a cancel or expiry kills the connection (`context.AfterFunc`); an error that arrives after ctx fired surfaces as `ctx.Err()`. A dropped connection surfaces the library's error, wrapped with what bud was doing.
15. **record.go**: the tracer wraps the conn below the client and is unchanged; `Record()`'s scripted sequence is re-expressed on the new surface (Caps iteration → `Findings.Capabilities` sorted; List → folders and trash; STATUS INBOX; EXAMINE INBOX; Search; the two newest summaries; a 512-byte body; LOGOUT). The transcript format, the redaction and `Findings` are unchanged.

## Not in this change

IDLE, QRESYNC, CONDSTORE, ESEARCH, APPEND (the sent-copy question stays a live-run item), metadata, ACL, anything SMTP, the tools, the tiers, the action log, the gateway env.

## Tests

`imap_test.go` and `testdata/*.imap` are deleted with the parser they pinned; `testdata/messages/*.eml` stays. The new `client_test.go` runs against the library's own in-process server, `imapserver` + `imapserver/imapmemserver` (same module, so no new dependency, and both ends of the wire are the library's):

- A `memServer` helper: `imapmemserver.New`, `NewUser("someone@example.com", "app-pass")`, `user.Create` for the fixtures (one with `SpecialUse: []imap.MailboxAttr{imap.MailboxAttrTrash}`, one `\Noselect`, one with a non-ASCII name), `imapserver.New(&imapserver.Options{NewSession: ..., Caps: ..., InsecureAuth: true})`, listening on `127.0.0.1:0`. Messages are seeded by APPEND over a client connection.
- A capability matrix per test: `{IMAP4rev1, MOVE, UIDPLUS}` for the MOVE path; `{IMAP4rev1, UIDPLUS}` for the COPY/STORE/UID EXPUNGE fallback; `{IMAP4rev1}` alone for the refusal; `CapLoginDisabled` added for the AUTHENTICATE PLAIN path. Never `IMAP4rev2`, which implies MOVE and UIDPLUS and would blur the matrix.
- The old tests' contracts, same names' intents: list decodes names and puts INBOX first; search with UTF-8 text, summaries, a partial body fetch, a missing uid → `ErrNoMessage`, and the flags re-fetched to prove nothing became `\Seen`; move over MOVE verified by the messages actually being in the trash folder; the fallback expunging only the moved uids, the others still present; the refusal; the refused login → `ErrAuth` with no password in the error; the AUTHENTICATE PLAIN fallback; the plain-connection refusal off loopback (the `fakeConn` trick); PREAUTH refused; a canceled ctx stopping a waiting command (a `net.Pipe` that answers the greeting and nothing else); line-break refusals; and `--record` against the mem server producing a transcript with no password and literals redacted.
- `server_test.go`: the two tests that used `replay()` (`TestAccountFoldersSkipsUnselectableOnesAndNamesSpecialUse`, `TestAccountSearchKeepsTheNewestMatchesNewestFirst`) move onto the `memServer` harness with the same wants; the rest of the file never touches IMAP.

## Commands

```
go get github.com/emersion/go-imap/v2@v2.0.0-beta.9
go mod tidy
go test ./internal/mail
go test ./...
go vet ./...
gofmt -w internal/mail
just leak-check
```

The first two need the network and are Jason's, until the module is on disk; the rest are the overnight loop.

## Open

1. The first live run on the home server still settles iCloud's capabilities, Sent Messages filing and trash retention, exactly as `docs/internals/mail.md`'s "Not yet" says; `--record` still records it.
2. Whether other homegrown protocol clients (CalDAV, the IMAP-adjacent bits elsewhere) ever follow the same exception is a separate decision each; none is proposed here.
